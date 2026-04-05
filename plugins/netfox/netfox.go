package netfox

import (
	"context"
	"os"
	"path/filepath"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
)

type NetFoxPlugin struct{}

type netFoxVFSWrapper struct {
	*NetFoxVFS
}
// ctxReader wraps vfs.ReadAtCloser to implement standard io.Reader
type ctxReader struct {
	r   vfs.ReadAtCloser
	ctx context.Context
}

func (cr ctxReader) Read(p []byte) (int, error) {
	return cr.r.Read(cr.ctx, p)
}

func (w *netFoxVFSWrapper) ProcessPanelKey(app vfs.App, e *vtinput.InputEvent) bool {
	// Only F7 without ANY modifiers (Shift/Ctrl/Alt)
	mods := vtinput.LeftCtrlPressed | vtinput.RightCtrlPressed | vtinput.LeftAltPressed | vtinput.RightAltPressed | vtinput.ShiftPressed
	if e.VirtualKeyCode == vtinput.VK_F7 && (e.ControlKeyState&uint32(mods)) == 0 {
		actionNewConnection(app, w.NetFoxVFS)
		return true
	}
	return false
}

func (p *NetFoxPlugin) Init(api vfs.HostAPI) error {
	api.RegisterDrive("&3. NetFox", func() vfs.VFS {
		cfgDir, _ := os.UserConfigDir()
		return &netFoxVFSWrapper{NewNetFoxVFS(filepath.Join(cfgDir, "f4", "NetFox.json"))}
	})
	return nil
}

func actionNewConnection(app vfs.App, nf *NetFoxVFS) {
	// To truly isolate UI, we would need a full Dialog API.
	// For now, since this is an "Internal Go Plugin", we allow a limited
	// callback for custom connection strings via InputBox.
	app.InputBox(" New Connection ", "Host (sftp://user@host:port):", "", func(connStr string) {
		if connStr == "" { return }
		// Simplified: parse minimal string and save
		nf.SaveConfig("NewServer", NetFoxConfig{
			Type: "sftp",
			Host: connStr,
			User: "root",
		})
		app.RefreshAll()
	})
}

func (p *NetFoxPlugin) Close() error { return nil }
func (p *NetFoxPlugin) GetName() string { return "NetFox Support" }