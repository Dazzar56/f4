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
		actionNewConnectionMenu(app, w.NetFoxVFS)
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

func actionNewConnectionMenu(app vfs.App, nf *NetFoxVFS) {
	protocols := vfs.GetNetFoxProtocols()
	var names []string
	var types []string
	for t := range protocols {
		names = append(names, t)
		types = append(types, t)
	}

	app.Menu(" New Connection ", names, func(idx int) {
		if idx < 0 || idx >= len(types) { return }
		p := protocols[types[idx]]
		if name, cfg, ok := p.CreateConnectionUI(app); ok {
			nf.SaveConfig(name, cfg)
			app.RefreshAll()
		}
	})
}

func (p *NetFoxPlugin) Close() error { return nil }
func (p *NetFoxPlugin) GetName() string { return "NetFox Support" }