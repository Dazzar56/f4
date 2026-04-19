package main

import (
	"fmt"
	"time"
	"runtime"

	"os/user"
	"strconv"

	"github.com/mattn/go-runewidth"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

func padLabel(s string) string {
	for runewidth.StringWidth(s) < 12 {
		s += " "
	}
	return s
}

func ShowAttributesDialog(pf *PanelsFrame, v vfs.VFS, path string, item vfs.VFSItem) {
	if runtime.GOOS == "windows" {
		showAttributesWindows(pf, v, path, item)
	} else {
		showAttributesUnix(pf, v, path, item)
	}
}

func showAttributesUnix(pf *PanelsFrame, v vfs.VFS, path string, item vfs.VFSItem) {
	width, height := 64, 21
	dlg := vtui.NewCenteredDialog(width, height, " Attributes ")
	dlg.ShowClose = true

	x, y := dlg.X1, dlg.Y1

	// 1. Info Header
	info := fmt.Sprintf("Change file attributes for:\n%s", vtui.TruncateMiddle(v.Base(path), 56))
	for i, line := range vtui.WrapText(info, 56) {
		dlg.AddItem(vtui.NewText(x+2, y+1+i, line, vtui.Palette[vtui.ColDialogText]))
	}

	// 2. Ownership
	dlg.AddItem(vtui.NewGroupBox(x+2, y+4, x+61, y+7, " Ownership "))
	ownerName := strconv.Itoa(item.Uid)
	if u, err := user.LookupId(ownerName); err == nil {
		ownerName = u.Username
	}
	groupName := strconv.Itoa(item.Gid)
	if g, err := user.LookupGroupId(groupName); err == nil {
		groupName = g.Name
	}

	editOwner := vtui.NewEdit(x+14, y+5, 12, ownerName)
	editGroup := vtui.NewEdit(x+14, y+6, 12, groupName)
	dlg.AddItem(vtui.NewLabel(x+4, y+5, "Owne&r:", editOwner)); dlg.AddItem(editOwner)
	dlg.AddItem(vtui.NewLabel(x+4, y+6, "&Group:", editGroup)); dlg.AddItem(editGroup)

	// 3. Permissions (3x3 Grid)
	dlg.AddItem(vtui.NewGroupBox(x+2, y+8, x+61, y+14, " Permissions "))

	makePermRow := func(label string, rowY int, bitOffset uint) (*vtui.Checkbox, *vtui.Checkbox, *vtui.Checkbox) {
		lbl := vtui.NewText(x+4, rowY, padLabel(label), vtui.Palette[vtui.ColDialogText])
		dlg.AddItem(lbl)
		r := vtui.NewCheckbox(x+16, rowY, "Read", false); r.State = map[bool]int{true: 1}[(item.UnixMode & (0400 >> bitOffset)) != 0]
		w := vtui.NewCheckbox(x+28, rowY, "Write", false); w.State = map[bool]int{true: 1}[(item.UnixMode & (0200 >> bitOffset)) != 0]
		x_ := vtui.NewCheckbox(x+40, rowY, "Execute", false); x_.State = map[bool]int{true: 1}[(item.UnixMode & (0100 >> bitOffset)) != 0]
		dlg.AddItem(r); dlg.AddItem(w); dlg.AddItem(x_)
		return r, w, x_
	}

	uR, uW, uX := makePermRow("User:", y+9, 0)
	gR, gW, gX := makePermRow("Group:", y+10, 3)
	oR, oW, oX := makePermRow("Other:", y+11, 6)

	editOctal := vtui.NewEdit(x+52, y+13, 6, fmt.Sprintf("%04o", item.UnixMode))
	editOctal.Validator = &vtui.OctalValidator{MaxDigits: 4}
	dlg.AddItem(vtui.NewLabel(x+46, y+13, "O&ct:", editOctal)); dlg.AddItem(editOctal)

	const timeFormat = "02.01.2006 15:04:05"

	// --- Sync Logic ---
	syncing := false
	allChecks := []*vtui.Checkbox{uR, uW, uX, gR, gW, gX, oR, oW, oX}

	updateOctalFromChecks := func() {
		if syncing { return }
		syncing = true
		var mode uint32
		bits := []uint32{0400, 0200, 0100, 0040, 0020, 0010, 0004, 0002, 0001}
		for i, cb := range allChecks {
			if cb.State == 1 { mode |= bits[i] }
		}
		editOctal.SetText(fmt.Sprintf("%04o", mode))
		syncing = false
		vtui.FrameManager.Redraw()
	}

	updateChecksFromOctal := func(s string) {
		if syncing { return }
		var mode uint64
		_, err := fmt.Sscanf(s, "%o", &mode)
		if err != nil { return }
		syncing = true
		bits := []uint32{0400, 0200, 0100, 0040, 0020, 0010, 0004, 0002, 0001}
		for i, cb := range allChecks {
			if (uint32(mode) & bits[i]) != 0 { cb.State = 1 } else { cb.State = 0 }
		}
		syncing = false
		vtui.FrameManager.Redraw()
	}

	for _, cb := range allChecks {
		cb.OnChange = func(int) { updateOctalFromChecks() }
	}
	editOctal.OnTextChange = updateChecksFromOctal

	// 4. Times
	editMTime := vtui.NewEdit(x+24, y+16, 20, item.MTime.Format(timeFormat))
	dlg.AddItem(vtui.NewLabel(x+4, y+16, "Modification time:", editMTime))
	dlg.AddItem(editMTime)

	// 5. Buttons
	btnSet := vtui.NewButton(0, 0, "Set")
	btnSet.IsDefault = true
	btnCancel := vtui.NewButton(0, 0, "Cancel")
	dlg.AddItem(btnSet); dlg.AddItem(btnCancel)

	btnHbox := vtui.NewHBoxLayout(x+2, y+height-3, width-4, 1)
	btnHbox.HorizontalAlign = vtui.AlignCenter
	btnHbox.Spacing = 2
	btnHbox.Add(btnSet, vtui.Margins{}, vtui.AlignTop)
	btnHbox.Add(btnCancel, vtui.Margins{}, vtui.AlignTop)
	btnHbox.Apply()

	btnSet.OnClick = func() {
		// 1. Resolve Ownership Names back to IDs
		if u, err := user.Lookup(editOwner.GetText()); err == nil {
			item.Uid, _ = strconv.Atoi(u.Uid)
		} else {
			id, _ := strconv.Atoi(editOwner.GetText())
			item.Uid = id
		}

		if g, err := user.LookupGroup(editGroup.GetText()); err == nil {
			item.Gid, _ = strconv.Atoi(g.Gid)
		} else {
			id, _ := strconv.Atoi(editGroup.GetText())
			item.Gid = id
		}

		// 2. Reconstruct UnixMode from 9 checkboxes
		var mode uint32
		if uR.State == 1 { mode |= 0400 }; if uW.State == 1 { mode |= 0200 }; if uX.State == 1 { mode |= 0100 }
		if gR.State == 1 { mode |= 0040 }; if gW.State == 1 { mode |= 0020 }; if gX.State == 1 { mode |= 0010 }
		if oR.State == 1 { mode |= 0004 }; if oW.State == 1 { mode |= 0002 }; if oX.State == 1 { mode |= 0001 }
		item.UnixMode = mode

		newTime, err := time.Parse(timeFormat, editMTime.GetText())
		if err == nil { item.MTime = newTime }

		vtui.RunAsync(func(ctx *vtui.TaskContext) {
			err := v.SetAttributes(ctx.Context, path, item)
			ctx.RunOnUI(func() {
				if err != nil {
					vtui.ShowMessage(" Error ", err.Error(), []string{"&Ok"})
				} else {
					dlg.Close()
					pf.RefreshAll()
				}
			})
		})
	}
	btnCancel.OnClick = func() { dlg.Close() }

	vtui.FrameManager.Push(dlg)
}

func showAttributesWindows(pf *PanelsFrame, v vfs.VFS, path string, item vfs.VFSItem) {
	width, height := 50, 15
	dlg := vtui.NewCenteredDialog(width, height, " Attributes ")
	dlg.ShowClose = true
	x, y := dlg.X1, dlg.Y1

	dlg.AddItem(vtui.NewText(x+2, y+1, "File: "+v.Base(path), vtui.Palette[vtui.ColDialogText]))

	const timeFormat = "02.01.2006 15:04:05"

	chkRO := vtui.NewCheckbox(x+2, y+3, "&Read only", false)
	chkHD := vtui.NewCheckbox(x+2, y+4, "&Hidden", false)
	chkSY := vtui.NewCheckbox(x+2, y+5, "&System", false)
	chkAR := vtui.NewCheckbox(x+2, y+6, "&Archive", false)

	dlg.AddItem(chkRO); dlg.AddItem(chkHD); dlg.AddItem(chkSY); dlg.AddItem(chkAR)

	editMTime := vtui.NewEdit(x+18, y+8, 20, item.MTime.Format(timeFormat))
	dlg.AddItem(vtui.NewLabel(x+2, y+8, "Last write:", editMTime))
	dlg.AddItem(editMTime)

	btnSet := vtui.NewButton(x+14, y+12, "{ Set }")
	btnSet.IsDefault = true
	btnCancel := vtui.NewButton(x+26, y+12, "[ Cancel ]")
	dlg.AddItem(btnSet); dlg.AddItem(btnCancel)

	btnSet.OnClick = func() {
		newTime, _ := time.Parse(timeFormat, editMTime.GetText())
		item.MTime = newTime
		// Windows specific attribute logic would go here
		vtui.RunAsync(func(ctx *vtui.TaskContext) {
			v.SetAttributes(ctx.Context, path, item)
			ctx.RunOnUI(func() { dlg.Close(); pf.RefreshAll() })
		})
	}
	btnCancel.OnClick = func() { dlg.Close() }

	vtui.FrameManager.Push(dlg)
}