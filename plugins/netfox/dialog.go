package netfox

import (
	"context"

	"github.com/mattn/go-runewidth"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

func padLabel(s string) string {
	for runewidth.StringWidth(s) < 11 {
		s += " "
	}
	return s
}

func showConnectionDialog(app vfs.App, nf *NetFoxVFS, oldName string) {
	var cfg NetFoxConfig
	name := ""
	if oldName != "" {
		name = oldName
		configs := nf.getConfigs()
		cfg = configs[oldName]
	} else {
		cfg.Type = "sftp"
		cfg.Port = "22"
	}

	dlg := vtui.NewCenteredDialog(60, 15, " Site Connection ")
	dlg.ShowClose = true

	editName := vtui.NewEdit(0, 0, 40, name)

	comboProto := vtui.NewComboBox(0, 0, 15, []string{"sftp", "ftp"})
	comboProto.DropdownOnly = true
	if cfg.Type == "ftp" {
		comboProto.Menu.SetSelectPos(1)
		comboProto.Edit.SetText("ftp")
	} else {
		comboProto.Menu.SetSelectPos(0)
		comboProto.Edit.SetText("sftp")
	}

	editHost := vtui.NewEdit(0, 0, 40, cfg.Host)
	editPort := vtui.NewEdit(0, 0, 10, cfg.Port)
	editUser := vtui.NewEdit(0, 0, 40, cfg.User)
	editPass := vtui.NewPasswordEdit(0, 0, 40, cfg.Pass)

	// Dynamic port switching based on protocol change
	origOnAction := comboProto.Menu.OnAction
	comboProto.Menu.OnAction = func(idx int) {
		if origOnAction != nil {
			origOnAction(idx)
		}
		proto := comboProto.Menu.Items[idx].Text
		currPort := editPort.GetText()
		if proto == "sftp" && (currPort == "" || currPort == "21") {
			editPort.SetText("22")
		} else if proto == "ftp" && (currPort == "" || currPort == "22") {
			editPort.SetText("21")
		}
	}

	btnOk := vtui.NewButton(0, 0, "&Save")
	btnCancel := vtui.NewButton(0, 0, "Cancel")
	btnOk.IsDefault = true

	vbox := vtui.NewVBoxLayout(dlg.X1+2, dlg.Y1+2, 56, 11)

	makeRow := func(label string, edit vtui.UIElement) *vtui.HBoxLayout {
		hbox := vtui.NewHBoxLayout(0, 0, 56, 1)
		l := vtui.NewLabel(0, 0, padLabel(label), edit)
		dlg.AddItem(l)
		dlg.AddItem(edit)
		hbox.Add(l, vtui.Margins{Right: 1}, vtui.AlignLeft)
		hbox.Add(edit, vtui.Margins{}, vtui.AlignFill)
		return hbox
	}

	vbox.Add(makeRow("&Name:", editName), vtui.Margins{}, vtui.AlignFill)
	vbox.Add(makeRow("P&rotocol:", comboProto), vtui.Margins{Top: 1}, vtui.AlignFill)
	vbox.Add(makeRow("&Host:", editHost), vtui.Margins{Top: 1}, vtui.AlignFill)
	vbox.Add(makeRow("P&ort:", editPort), vtui.Margins{Top: 1}, vtui.AlignFill)
	vbox.Add(makeRow("&User:", editUser), vtui.Margins{Top: 1}, vtui.AlignFill)
	vbox.Add(makeRow("Pass&word:", editPass), vtui.Margins{Top: 1}, vtui.AlignFill)

	sep := vtui.NewSeparator(0, 0, 56, true, true)
	dlg.AddItem(sep)
	vbox.Add(sep, vtui.Margins{Top: 1, Bottom: 1}, vtui.AlignFill)

	btnHbox := vtui.NewHBoxLayout(0, 0, 56, 1)
	btnHbox.HorizontalAlign = vtui.AlignCenter
	btnHbox.Spacing = 2

	btnOk.OnClick = func() {
		newName := editName.GetText()
		if newName == "" {
			vtui.ShowMessageOn(dlg, " Error ", "Name cannot be empty", []string{"&Ok"})
			return
		}
		if newName == "<Add connection>" || newName == ".." {
			vtui.ShowMessageOn(dlg, " Error ", "Reserved connection name", []string{"&Ok"})
			return
		}

		cfg.Type = comboProto.Edit.GetText()
		cfg.Host = editHost.GetText()
		cfg.Port = editPort.GetText()
		cfg.User = editUser.GetText()
		cfg.Pass = editPass.GetText()

		if oldName != "" && oldName != newName {
			nf.Remove(context.Background(), oldName)
		}
		nf.SaveConfig(newName, cfg)

		dlg.Close()
		app.RefreshAll()
	}
	btnCancel.OnClick = func() {
		dlg.Close()
	}

	dlg.AddItem(btnOk)
	dlg.AddItem(btnCancel)

	btnHbox.Add(btnOk, vtui.Margins{}, vtui.AlignTop)
	btnHbox.Add(btnCancel, vtui.Margins{}, vtui.AlignTop)

	vbox.Add(btnHbox, vtui.Margins{}, vtui.AlignFill)
	vbox.Apply()

	vtui.FrameManager.Push(dlg)
}