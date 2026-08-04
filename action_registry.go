package main

import (
	"sort"
	"strings"

	"github.com/unxed/vtui"
)

// Action represents a bindable command in the application.
type Action struct {
	Name        string
	Label       string
	Description string
	Handler     func()
}

var actionRegistry = make(map[string]Action)

// RegisterAction adds an action to the global registry.
func RegisterAction(action Action) {
	actionRegistry[strings.ToLower(action.Name)] = action
}

// RunAction executes an action by name if it exists.
func RunAction(name string) bool {
	if a, ok := actionRegistry[strings.ToLower(name)]; ok && a.Handler != nil {
		a.Handler()
		return true
	}
	return false
}

// GetActions returns a list of all registered actions, sorted by name.
func GetActions() []Action {
	var actions []Action
	for _, a := range actionRegistry {
		actions = append(actions, a)
	}
	sort.Slice(actions, func(i, j int) bool {
		return actions[i].Name < actions[j].Name
	})
	return actions
}

// GetAction returns an action by name.
func GetAction(name string) (Action, bool) {
	a, ok := actionRegistry[strings.ToLower(name)]
	return a, ok
}

func init() {
	withPF := func(fn func(pf *PanelsFrame)) func() {
		return func() {
			if pf := findPanelsFrameAnyScreen(); pf != nil {
				fn(pf)
			}
		}
	}

	RegisterAction(Action{
		Name:        "File.Copy",
		Label:       "Copy",
		Description: "Copy selected files or current file",
		Handler:     withPF(func(pf *PanelsFrame) { actionCopyMove(pf, false) }),
	})
	RegisterAction(Action{
		Name:        "File.Move",
		Label:       "Move",
		Description: "Rename or move selected files",
		Handler:     withPF(func(pf *PanelsFrame) { actionCopyMove(pf, true) }),
	})
	RegisterAction(Action{
		Name:        "File.Rename",
		Label:       "Rename",
		Description: "Rename current file",
		Handler:     withPF(func(pf *PanelsFrame) { actionRename(pf) }),
	})
	RegisterAction(Action{
		Name:        "File.Delete",
		Label:       "Delete",
		Description: "Delete selected files",
		Handler:     withPF(func(pf *PanelsFrame) { actionDelete(pf) }),
	})
	RegisterAction(Action{
		Name:        "File.MakeDir",
		Label:       "Make Folder",
		Description: "Create a new directory",
		Handler:     withPF(func(pf *PanelsFrame) { actionMkDir(pf) }),
	})
	RegisterAction(Action{
		Name:        "File.Edit",
		Label:       "Edit",
		Description: "Open file in editor",
		Handler:     withPF(func(pf *PanelsFrame) { actionEditFile(pf) }),
	})
	RegisterAction(Action{
		Name:        "File.View",
		Label:       "View",
		Description: "Open file in viewer",
		Handler:     withPF(func(pf *PanelsFrame) { actionViewFile(pf) }),
	})
	RegisterAction(Action{
		Name:        "File.New",
		Label:       "New File",
		Description: "Create and open a new file in editor",
		Handler:     withPF(func(pf *PanelsFrame) { actionNewFile(pf) }),
	})
	RegisterAction(Action{
		Name:        "File.Find",
		Label:       "Find File",
		Description: "Search for files",
		Handler:     withPF(func(pf *PanelsFrame) { actionFindFile(pf) }),
	})

	RegisterAction(Action{
		Name:        "Panel.Rescan",
		Label:       "Rescan",
		Description: "Refresh panel contents",
		Handler:     withPF(func(pf *PanelsFrame) { pf.RefreshAll() }),
	})
	RegisterAction(Action{
		Name:        "Panel.Swap",
		Label:       "Swap Panels",
		Description: "Swap left and right panels",
		Handler:     withPF(func(pf *PanelsFrame) { vtui.FrameManager.EmitCommand(CmSwapPanels, nil) }),
	})
	RegisterAction(Action{
		Name:        "Panel.Toggle",
		Label:       "Toggle Panels",
		Description: "Show or hide panels",
		Handler: withPF(func(pf *PanelsFrame) {
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
		}),
	})
	RegisterAction(Action{
		Name:        "Panel.FoldersHistory",
		Label:       "Folders History",
		Description: "Show folders history",
		Handler:     withPF(func(pf *PanelsFrame) { actionFoldersHistory(pf) }),
	})
	RegisterAction(Action{
		Name:        "Panel.CommandHistory",
		Label:       "Command History",
		Description: "Show command line history",
		Handler:     withPF(func(pf *PanelsFrame) { actionCommandHistory(pf) }),
	})

	RegisterAction(Action{
		Name:        "Settings.Panel",
		Label:       "Panel Settings",
		Description: "Open panel settings dialog",
		Handler:     withPF(func(pf *PanelsFrame) { actionPanelSettings(pf) }),
	})
	RegisterAction(Action{
		Name:        "Settings.Editor",
		Label:       "Editor Settings",
		Description: "Open editor settings dialog",
		Handler:     withPF(func(pf *PanelsFrame) { actionEditorSettings(pf) }),
	})
	RegisterAction(Action{
		Name:        "Settings.Colorer",
		Label:       "Colorer Settings",
		Description: "Open Colorer settings dialog",
		Handler:     withPF(func(pf *PanelsFrame) { actionColorerSettings(pf) }),
	})
	RegisterAction(Action{
		Name:        "Settings.Appearance",
		Label:       "Appearance Settings",
		Description: "Open appearance settings dialog",
		Handler:     withPF(func(pf *PanelsFrame) { actionAppearanceSettings(pf) }),
	})
	RegisterAction(Action{
		Name:        "Settings.Confirmations",
		Label:       "Confirmations Settings",
		Description: "Open confirmations settings dialog",
		Handler:     withPF(func(pf *PanelsFrame) { actionConfirmationsSettings(pf) }),
	})

	RegisterAction(Action{
		Name:        "Terminal.ViewLog",
		Label:       "View Terminal Log",
		Description: "Open terminal log in viewer",
		Handler:     withPF(func(pf *PanelsFrame) { actionViewTerminalLog(pf) }),
	})
	RegisterAction(Action{
		Name:        "Terminal.EditLog",
		Label:       "Edit Terminal Log",
		Description: "Open terminal log in editor",
		Handler:     withPF(func(pf *PanelsFrame) { actionEditTerminalLog(pf) }),
	})
}
