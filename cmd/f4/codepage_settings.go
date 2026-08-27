package main

import (
	"fmt"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

func codepageSettingChoices() ([]int, []string) {
	ids := make([]int, 0, len(vfs.AvailableCodepages))
	labels := make([]string, 0, len(vfs.AvailableCodepages))
	for _, cp := range vfs.AvailableCodepages {
		ids = append(ids, cp.ID)
		labels = append(labels, fmt.Sprintf("%5d  %s", cp.ID, vfs.DisplayCodepageName(cp.ID)))
	}
	return ids, labels
}

func codepageChoiceIndex(ids []int, current int) int {
	for i, id := range ids {
		if id == current {
			return i
		}
	}
	return 0
}

func actionViewerSettings(pf *PanelsFrame) {
	width, height := 78, 10
	dlg := vtui.NewCenteredDialog(width, height, Msg("ViewerSettings.Title"))
	dlg.ShowClose = true

	ids, labels := codepageSettingChoices()
	comboDefault := vtui.NewComboBox(0, 0, 40, labels)
	comboDefault.DropdownOnly = true
	selected := codepageChoiceIndex(ids, AppConfig.ViewerDefaultCodePage)
	comboDefault.Menu.SetSelectPos(selected)
	comboDefault.Edit.SetText(labels[selected])
	lblDefault := vtui.NewLabel(0, 0, Msg("ViewerSettings.DefaultCodePage"), comboDefault)

	chkAutodetect := vtui.NewCheckbox(0, 0, Msg("ViewerSettings.AutodetectCodePage"), false)
	if AppConfig.ViewerAutodetectCodePage {
		chkAutodetect.State = 1
	}
	btnOK := vtui.NewButton(0, 0, Msg("vtui.Ok"))
	btnOK.IsDefault = true
	btnCancel := vtui.NewButton(0, 0, Msg("vtui.Cancel"))

	dlg.AddItem(chkAutodetect)
	dlg.AddItem(lblDefault)
	dlg.AddItem(comboDefault)
	dlg.AddItem(btnOK)
	dlg.AddItem(btnCancel)

	vbox := vtui.NewVBoxLayout(dlg.X1+2, dlg.Y1+2, width-4, height-4)
	vbox.Add(chkAutodetect, vtui.Margins{}, vtui.AlignLeft)
	rowDefault := vtui.NewHBoxLayout(0, 0, width-4, 1)
	rowDefault.Add(lblDefault, vtui.Margins{Right: 1}, vtui.AlignLeft)
	rowDefault.Add(comboDefault, vtui.Margins{}, vtui.AlignFill)
	vbox.Add(rowDefault, vtui.Margins{Top: 1}, vtui.AlignFill)
	buttons := vtui.NewHBoxLayout(0, 0, width-4, 1)
	buttons.HorizontalAlign = vtui.AlignCenter
	buttons.Spacing = 2
	buttons.Add(btnOK, vtui.Margins{}, vtui.AlignTop)
	buttons.Add(btnCancel, vtui.Margins{}, vtui.AlignTop)
	vbox.Add(buttons, vtui.Margins{Top: 1}, vtui.AlignFill)
	vbox.Apply()

	btnCancel.OnClick = func() { dlg.Close() }
	btnOK.OnClick = func() {
		AppConfig.ViewerAutodetectCodePage = chkAutodetect.State == 1
		if pos := comboDefault.Menu.SelectPos; pos >= 0 && pos < len(ids) {
			AppConfig.ViewerDefaultCodePage = ids[pos]
		}
		SaveConfig()
		dlg.Close()
	}

	vtui.FrameManager.Push(dlg)
}
