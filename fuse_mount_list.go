package main

import (
	"fmt"

	"github.com/unxed/f4/fusefs"
	"github.com/unxed/vtui"
)

// The mounts dialog (FUSE.md, iteration 2): one list over the live mounts,
// with Go to and Unmount on the selected one.
//
// It lists the mounts this process owns — the ones the panel command makes.
// Mounts started from a shell live in the cross-process registry and are a
// separate step.
func init() {
	RegisterAction(Action{
		Name:        "Panel.MountList",
		Area:        "Shell",
		Label:       "FUSE Mounts",
		Description: "List the live FUSE mounts, go to one or unmount it",
		MenuPath:    "Commands",
		Visible:     fusefs.Supported,
		Handler: func() bool {
			pf := findPanelsFrameAnyScreen()
			if pf == nil {
				return false
			}
			showMountList(pf)
			return true
		},
	})
}

func showMountList(pf *PanelsFrame) {
	mounts := fusefs.List()
	if len(mounts) == 0 {
		vtui.ShowMessage(" Mounts ", "Nothing is mounted.", []string{"&Ok"})
		return
	}

	menu := vtui.NewVMenu(" Mounts ")
	for i, m := range mounts {
		menu.AddItem(vtui.MenuItem{
			Text:     fmt.Sprintf("%s  ←  %s", m.MountPoint, m.Source),
			UserData: i,
		})
	}

	w, h := 70, len(mounts)+2
	scrW := vtui.FrameManager.GetScreenSize()
	scrH := vtui.FrameManager.GetScreenHeight()
	if maxH := scrH - 2; h > maxH && maxH >= 5 {
		h = maxH
	}
	if w > scrW-2 {
		w = scrW - 2
	}
	x, y := (scrW-w)/2, (scrH-h)/2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	menu.SetPosition(x, y, x+w-1, y+h-1)

	menu.OnAction = func(idx int) {
		menu.Close()
		if idx < 0 || idx >= len(menu.Items) {
			return
		}
		i, ok := menu.Items[idx].UserData.(int)
		if !ok || i < 0 || i >= len(mounts) {
			return
		}
		askMountAction(pf, mounts[i])
	}
	vtui.FrameManager.Push(menu)
}

// askMountAction offers the two things worth doing to a live mount. Unmount
// does not force: a busy mount is a question for the user ("something is
// still in there"), not an error to paper over.
func askMountAction(pf *PanelsFrame, m *fusefs.Mount) {
	point := m.MountPoint
	dlg := vtui.ShowMessage(" Mount ", point, []string{"&Go to", "&Unmount", "&Cancel"})
	dlg.OnResult = func(code int) {
		switch code {
		case 0:
			if fsp := pf.getActivePanel(); fsp != nil {
				pf.NavigateToPath(fsp, point)
			}
		case 1:
			if err := m.Unmount(); err != nil {
				vtui.ShowMessage(" Mount ", fmt.Sprintf("Cannot unmount %s:\n%v", point, err), []string{"&Ok"})
			}
		}
	}
}
