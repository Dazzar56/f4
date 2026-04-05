package netfox

import (
	"context"
	"os"
	"path/filepath"

	"encoding/json"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

type NetFoxPlugin struct{}

type netFoxVFSWrapper struct {
	*vfs.NetFoxVFS
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

// netFoxProvider handles SFTP connections from the NetFox list
type netFoxProvider struct{}

func (p *netFoxProvider) Name() string  { return "NetFox-SFTP" }
func (p *netFoxProvider) Priority() int { return 100 }

func (p *netFoxProvider) CanOpen(ctx context.Context, parent vfs.VFS, pth string) bool {
	w, ok := parent.(*netFoxVFSWrapper)
	if !ok { return false }
	item, err := w.Stat(ctx, pth)
	if err != nil || item.IsDir { return false }

	// Open the connection file (JSON) to check type
	f, err := w.Open(ctx, pth)
	if err != nil { return false }
	defer f.Close()

	var cfg vfs.NetFoxConfig
	json.NewDecoder(ctxReader{f, ctx}).Decode(&cfg)
	return cfg.Type == "sftp" || cfg.Type == ""
}

func (p *netFoxProvider) Open(ctx context.Context, parent vfs.VFS, pth string) (vfs.VFS, error) {
	w := parent.(*netFoxVFSWrapper)
	vtui.DebugLog("NETFOX: Opening SFTP connection from config %q", pth)
	f, err := w.Open(ctx, pth)
	if err != nil { return nil, err }
	defer f.Close()

	var cfg vfs.NetFoxConfig
	// Сбрасываем указатель (если VFS поддерживает Seek)
	if s, ok := f.(interface{ Seek(int64, int) (int64, error) }); ok {
		s.Seek(0, 0)
	}
	if err := json.NewDecoder(ctxReader{f, ctx}).Decode(&cfg); err != nil { return nil, err }

	port := cfg.Port
	if port == "" { port = "22" }
	return vfs.NewSFTPVFS(parent, cfg.Host, port, cfg.User, cfg.Pass)
}

// ftpProvider handles FTP connections from the NetFox list
type ftpProvider struct{}

func (p *ftpProvider) Name() string  { return "NetFox-FTP" }
func (p *ftpProvider) Priority() int { return 100 }

func (p *ftpProvider) CanOpen(ctx context.Context, parent vfs.VFS, pth string) bool {
	w, ok := parent.(*netFoxVFSWrapper)
	if !ok { return false }
	item, err := w.Stat(ctx, pth)
	if err != nil || item.IsDir { return false }

	f, err := w.Open(ctx, pth)
	if err != nil { return false }
	defer f.Close()

	var cfg vfs.NetFoxConfig
	json.NewDecoder(ctxReader{f, ctx}).Decode(&cfg)
	return cfg.Type == "ftp"
}

func (p *ftpProvider) Open(ctx context.Context, parent vfs.VFS, pth string) (vfs.VFS, error) {
	w := parent.(*netFoxVFSWrapper)
	vtui.DebugLog("NETFOX: Opening FTP connection from config %q", pth)
	f, err := w.Open(ctx, pth)
	if err != nil { return nil, err }
	defer f.Close()

	var cfg vfs.NetFoxConfig
	if s, ok := f.(interface{ Seek(int64, int) (int64, error) }); ok {
		s.Seek(0, 0)
	}
	if err := json.NewDecoder(ctxReader{f, ctx}).Decode(&cfg); err != nil { return nil, err }

	port := cfg.Port
	if port == "" { port = "21" }
	return vfs.NewFTPVFS(parent, cfg.Host, port, cfg.User, cfg.Pass)
}

func (p *NetFoxPlugin) Init(api vfs.HostAPI) error {
	api.RegisterVFSProvider(&netFoxProvider{})
	api.RegisterVFSProvider(&ftpProvider{})

	api.RegisterDrive("&3. NetFox", func() vfs.VFS {
		cfgDir, _ := os.UserConfigDir()
		return &netFoxVFSWrapper{vfs.NewNetFoxVFS(filepath.Join(cfgDir, "f4", "NetFox.json"))}
	})
	return nil
}

func actionNewConnection(app vfs.App, nf *vfs.NetFoxVFS) {
	dlg := vtui.NewCenteredDialog(48, 17, " New Connection ")
	dlg.ShowClose = true

	rbType := vtui.NewRadioGroup(0, 0, 1, []string{"SFTP", "FTP"})
	rbType.Selected = 0

	lblName := vtui.NewLabel(0, 0, "Connection &Name:", nil)
	editName := vtui.NewEdit(0, 0, 20, "MyServer")
	lblName.FocusLink = editName

	lblHost := vtui.NewLabel(0, 0, "&Host or IP:", nil)
	editHost := vtui.NewEdit(0, 0, 30, "")
	lblHost.FocusLink = editHost

	lblPort := vtui.NewLabel(0, 0, "&Port:", nil)
	editPort := vtui.NewEdit(0, 0, 6, "22")
	lblPort.FocusLink = editPort

	rbType.OnChange = func(val int) {
		if val == 0 { editPort.SetText("22") } else { editPort.SetText("21") }
	}

	lblUser := vtui.NewLabel(0, 0, "&User:", nil)
	editUser := vtui.NewEdit(0, 0, 20, "anonymous")
	lblUser.FocusLink = editUser

	lblPass := vtui.NewLabel(0, 0, "Pass&word:", nil)
	editPass := vtui.NewEdit(0, 0, 20, "")
	lblPass.FocusLink = editPass

	btnOk := vtui.NewButton(0, 0, "&Ok")
	btnCancel := vtui.NewButton(0, 0, "Cancel")
	btnOk.IsDefault = true

	dlg.AddItem(rbType); dlg.AddItem(lblName); dlg.AddItem(editName)
	dlg.AddItem(lblHost); dlg.AddItem(editHost); dlg.AddItem(lblPort); dlg.AddItem(editPort)
	dlg.AddItem(lblUser); dlg.AddItem(editUser); dlg.AddItem(lblPass); dlg.AddItem(editPass)
	dlg.AddItem(btnOk); dlg.AddItem(btnCancel)

	vbox := vtui.NewVBoxLayout(dlg.X1+2, dlg.Y1+1, 48-4, 17-2)
	vbox.Add(rbType, vtui.Margins{Bottom: 1}, vtui.AlignCenter)
	vbox.Add(lblName, vtui.Margins{}, vtui.AlignLeft)
	vbox.Add(editName, vtui.Margins{}, vtui.AlignFill)

	hbox1 := vtui.NewHBoxLayout(0, 0, 48-4, 2)
	vboxHost := vtui.NewVBoxLayout(0, 0, 30, 2)
	vboxHost.Add(lblHost, vtui.Margins{}, vtui.AlignLeft); vboxHost.Add(editHost, vtui.Margins{}, vtui.AlignFill)
	vboxPort := vtui.NewVBoxLayout(0, 0, 10, 2)
	vboxPort.Add(lblPort, vtui.Margins{}, vtui.AlignLeft); vboxPort.Add(editPort, vtui.Margins{}, vtui.AlignFill)
	hbox1.Add(vboxHost, vtui.Margins{Right: 2}, vtui.AlignLeft); hbox1.Add(vboxPort, vtui.Margins{}, vtui.AlignLeft)
	vbox.Add(hbox1, vtui.Margins{Top: 1}, vtui.AlignFill)

	hbox2 := vtui.NewHBoxLayout(0, 0, 48-4, 2)
	vboxUser := vtui.NewVBoxLayout(0, 0, 20, 2)
	vboxUser.Add(lblUser, vtui.Margins{}, vtui.AlignLeft); vboxUser.Add(editUser, vtui.Margins{}, vtui.AlignFill)
	vboxPass := vtui.NewVBoxLayout(0, 0, 20, 2)
	vboxPass.Add(lblPass, vtui.Margins{}, vtui.AlignLeft); vboxPass.Add(editPass, vtui.Margins{}, vtui.AlignFill)
	hbox2.Add(vboxUser, vtui.Margins{Right: 2}, vtui.AlignLeft); hbox2.Add(vboxPass, vtui.Margins{}, vtui.AlignLeft)
	vbox.Add(hbox2, vtui.Margins{Top: 1}, vtui.AlignFill)

	hboxBtns := vtui.NewHBoxLayout(0, 0, 48-4, 1)
	hboxBtns.HorizontalAlign = vtui.AlignCenter; hboxBtns.Spacing = 2
	hboxBtns.Add(btnOk, vtui.Margins{}, vtui.AlignTop); hboxBtns.Add(btnCancel, vtui.Margins{}, vtui.AlignTop)
	vbox.Add(hboxBtns, vtui.Margins{Top: 1}, vtui.AlignFill)

	vbox.Apply()
	dlg.SetFocusedItem(editName)

	btnCancel.OnClick = func() { dlg.Close() }

	btnOk.OnClick = func() {
		name := editName.GetText()
		if name != "" {
			tStr := "sftp"
			if rbType.Selected == 1 { tStr = "ftp" }
			nf.SaveConfig(name, vfs.NetFoxConfig{
				Type: tStr, Host: editHost.GetText(), Port: editPort.GetText(),
				User: editUser.GetText(), Pass: editPass.GetText(),
			})
			app.RefreshAll()
		}
		dlg.Close()
	}
	vtui.FrameManager.Push(dlg)
}

func (p *NetFoxPlugin) Close() error { return nil }
func (p *NetFoxPlugin) GetName() string { return "NetFox Support" }