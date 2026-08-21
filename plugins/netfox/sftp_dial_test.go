package netfox

import (
	"context"
	"path/filepath"
	"testing"
)

// TestSFTPProviderFailedDialReturnsPlainNil covers the stored-site path used
// by the panel. NewSFTPVFS returns a nil *SFTPVFS when DialSSH fails; the
// provider must not wrap that pointer in a non-nil vfs.VFS interface, because
// the asynchronous opener closes every non-nil result while reporting err.
func TestSFTPProviderFailedDialReturnsPlainNil(t *testing.T) {
	manager := NewNetFoxVFS(filepath.Join(t.TempDir(), "NetFox.json"))
	manager.SaveConfig("key-only", NetFoxConfig{
		Type:    "sftp",
		Host:    "127.0.0.1",
		Port:    "1",
		User:    "nobody",
		Pass:    "",
		Timeout: "1",
	})

	parent := &netFoxVFSWrapper{NetFoxVFS: manager}
	opened, err := (&sftpProvider{}).Open(context.Background(), parent, "key-only")
	if err == nil {
		if opened != nil {
			_ = opened.Close()
		}
		t.Fatal("opening an unreachable SFTP site succeeded")
	}
	if opened != nil {
		// This is the cleanup performed by the asynchronous panel opener. A
		// typed nil reaches this call and panics instead of showing err.
		_ = opened.Close()
		t.Fatalf("failed SFTP open returned non-nil file system %T", opened)
	}
}
