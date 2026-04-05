package netfox

import (
	"path/filepath"
	"testing"
	"github.com/unxed/f4/vfs"
)

func TestNetFoxVFS_ConfigPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_net.json")
	nf := NewNetFoxVFS(dbPath)

	// 1. Test Saving
	cfg := vfs.NetFoxConfig{Type: "sftp", Host: "1.2.3.4", User: "root"}
	nf.SaveConfig("My Server", cfg)

	// 2. Test Loading (via internal helper)
	configs := nf.getConfigs()
	if len(configs) != 1 {
		t.Fatalf("Expected 1 config, got %d", len(configs))
	}
	if configs["My Server"].Host != "1.2.3.4" {
		t.Errorf("Host mismatch. Got %s", configs["My Server"].Host)
	}

	// 3. Test ReadDir (visual representation)
	found := false
	nf.ReadDir(nil, "", func(items []vfs.VFSItem) {
		for _, itm := range items {
			if itm.Name == "My Server" { found = true }
		}
	})
	if !found { t.Error("ReadDir failed to list saved connection") }

	// 4. Test Removal
	nf.Remove(nil, "My Server")
	if len(nf.getConfigs()) != 0 {
		t.Error("Config was not removed")
	}
}