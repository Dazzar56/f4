package netfox

import (
	"github.com/unxed/f4/vfs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNetFoxVFS_ConfigPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_net.json")
	// Ensure the file is created for consistency in tests
	os.WriteFile(dbPath, []byte("{}"), 0644)
	nf := NewNetFoxVFS(dbPath)

	// 1. Test Saving
	cfg := NetFoxConfig{Type: "sftp", Host: "1.2.3.4", User: "root", Pass: "plaintext_secret"}
	nf.SaveConfig("My Server", cfg)

	// Check if password was actually encrypted on disk
	rawJSON, _ := os.ReadFile(dbPath)
	if !strings.Contains(string(rawJSON), cryptoPrefix) {
		t.Error("Password was not encrypted on disk")
	}
	if strings.Contains(string(rawJSON), "plaintext_secret") {
		t.Error("Plaintext password leaked into JSON file")
	}

	// 2. Test Loading (via internal helper)
	configs := nf.getConfigs()
	if len(configs) != 1 {
		t.Fatalf("Expected 1 config, got %d", len(configs))
	}
	if configs["My Server"].Host != "1.2.3.4" {
		t.Errorf("Host mismatch. Got %s", configs["My Server"].Host)
	}
	if configs["My Server"].Pass != "plaintext_secret" {
		t.Errorf("Decryption during load failed. Expected 'plaintext_secret', got %q", configs["My Server"].Pass)
	}

	// 3. Test ReadDir (visual representation)
	found := false
	nf.ReadDir(nil, "", func(items []vfs.VFSItem) {
		for _, itm := range items {
			if itm.Name == "My Server" {
				found = true
			}
		}
	})
	if !found {
		t.Error("ReadDir failed to list saved connection")
	}

	// 4. Test Removal
	nf.Remove(nil, "My Server")
	if len(nf.getConfigs()) != 0 {
		t.Error("Config was not removed")
	}
}
