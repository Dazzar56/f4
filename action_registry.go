package main

import (
	"strings"

	"github.com/unxed/vtui"
)

var actionRegistry = make(map[string]func())

// RegisterAction adds an action to the global registry.
func RegisterAction(name string, handler func()) {
	actionRegistry[strings.ToLower(name)] = handler
}

// RunAction executes an action by name if it exists.
func RunAction(name string) bool {
	if handler, ok := actionRegistry[strings.ToLower(name)]; ok {
		handler()
		return true
	}
	return false
}

func init() {
	withPF := func(fn func(pf *PanelsFrame)) func() {
		return func() {
			if pf := findPanelsFrameAnyScreen(); pf != nil {
				fn(pf)
			}
		}
	}

	RegisterAction("File.Copy", withPF(func(pf *PanelsFrame) { actionCopyMove(pf, false) }))
	RegisterAction("File.Move", withPF(func(pf *PanelsFrame) { actionCopyMove(pf, true) }))
	RegisterAction("File.Rename", withPF(func(pf *PanelsFrame) { actionRename(pf) }))
	RegisterAction("File.Delete", withPF(func(pf *PanelsFrame) { actionDelete(pf) }))
	RegisterAction("File.MakeDir", withPF(func(pf *PanelsFrame) { actionMkDir(pf) }))
	RegisterAction("File.Edit", withPF(func(pf *PanelsFrame) { actionEditFile(pf) }))
	RegisterAction("File.View", withPF(func(pf *PanelsFrame) { actionViewFile(pf) }))
	RegisterAction("File.New", withPF(func(pf *PanelsFrame) { actionNewFile(pf) }))
	RegisterAction("File.Find", withPF(func(pf *PanelsFrame) { actionFindFile(pf) }))

	RegisterAction("Panel.Rescan", withPF(func(pf *PanelsFrame) { pf.RefreshAll() }))
	RegisterAction("Panel.Swap", withPF(func(pf *PanelsFrame) { vtui.FrameManager.EmitCommand(CmSwapPanels, nil) }))
	RegisterAction("Panel.Toggle", withPF(func(pf *PanelsFrame) {
		pf.exitWide()
		pf.showPanels = !pf.showPanels
		if pf.showPanels && !pf.showLeftPanel && !pf.showRightPanel {
			pf.showLeftPanel = true
			pf.showRightPanel = true
		}
		vtui.FrameManager.HardRefresh()
		if pf.showPanels {
			pf.RefreshAll()
		}
	}))
	RegisterAction("Panel.FoldersHistory", withPF(func(pf *PanelsFrame) { actionFoldersHistory(pf) }))
	RegisterAction("Panel.CommandHistory", withPF(func(pf *PanelsFrame) { actionCommandHistory(pf) }))

	RegisterAction("Settings.Panel", withPF(func(pf *PanelsFrame) { actionPanelSettings(pf) }))
	RegisterAction("Settings.Editor", withPF(func(pf *PanelsFrame) { actionEditorSettings(pf) }))
	RegisterAction("Settings.Colorer", withPF(func(pf *PanelsFrame) { actionColorerSettings(pf) }))
	RegisterAction("Settings.Appearance", withPF(func(pf *PanelsFrame) { actionAppearanceSettings(pf) }))
	RegisterAction("Settings.Confirmations", withPF(func(pf *PanelsFrame) { actionConfirmationsSettings(pf) }))

	RegisterAction("Terminal.ViewLog", withPF(func(pf *PanelsFrame) { actionViewTerminalLog(pf) }))
	RegisterAction("Terminal.EditLog", withPF(func(pf *PanelsFrame) { actionEditTerminalLog(pf) }))
}