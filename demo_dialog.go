package main

import (
	"github.com/unxed/vtui"
)

func ShowDemoDialog() {
	// Центрируем диалог
	scrWidth := vtui.FrameManager.GetScreenSize()
	dlgWidth, dlgHeight := 60, 15
	x1 := (scrWidth - dlgWidth) / 2
	y1 := 4

	dlg := vtui.NewDialog(x1, y1, x1+dlgWidth-1, y1+dlgHeight-1, " vtui Components Demo ")

	// Группа радио-кнопок
	dlg.AddItem(vtui.NewText(x1+2, y1+2, "Select mode:", vtui.Palette[vtui.ColDialogText]))

	rb1 := vtui.NewRadioButton(x1+4, y1+4, "Fast and Dangerous")
	rb1.Selected = true
	rb2 := vtui.NewRadioButton(x1+4, y1+5, "Slow and Stable")
	rb3 := vtui.NewRadioButton(x1+4, y1+6, "Mental Health Mode")

	dlg.AddItem(rb1)
	dlg.AddItem(rb2)
	dlg.AddItem(rb3)

	// Чекбоксы
	dlg.AddItem(vtui.NewText(x1+30, y1+2, "Settings:", vtui.Palette[vtui.ColDialogText]))
	dlg.AddItem(vtui.NewCheckbox(x1+32, y1+4, "Enable AI", false))
	dlg.AddItem(vtui.NewCheckbox(x1+32, y1+5, "Auto-update", true))
	dlg.AddItem(vtui.NewCheckbox(x1+32, y1+6, "Force Legacy", false))

	// ComboBox
	dlg.AddItem(vtui.NewText(x1+2, y1+8, "Encoding:", vtui.Palette[vtui.ColDialogText]))
	items := []string{"UTF-8", "CP866 (OEM)", "Windows-1251", "KOI8-R"}
	combo := vtui.NewComboBox(x1+12, y1+8, 20, items)
	combo.Edit.SetText("UTF-8")
	dlg.AddItem(combo)

	btnOk := vtui.NewButton(x1+dlgWidth/2-5, y1+12, "Close")
	btnOk.OnClick = func() {
		dlg.SetExitCode(0)
	}
	dlg.AddItem(btnOk)

	vtui.FrameManager.Push(dlg)
}