package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

// Action represents a bindable command in the application.
//
// An action is the single source of truth for everything the user can
// trigger interactively: it is a macro command (by Name), a menu/keybar
// entry (Label/MenuPath), a help topic line (Description) and a default
// hotkey (DefaultKeys) at the same time.
type Action struct {
	Name        string // stable ID and macro command, e.g. "Editor.Save"
	Area        string // primary area: "Shell", "Editor", "Viewer", "Terminal", "Common"
	Label       string // English fallback for menu/keybar
	LabelKey    string // optional i18n key resolved via Msg()
	Description string // English fallback for help
	DescKey     string // optional i18n key resolved via Msg()
	// DefaultKeys are Far-style key names ("F2", "CtrlIns") with an
	// optional ":Condition" suffix per key (e.g. "Esc:EscToggle").
	DefaultKeys []string
	// DefaultAreas lists extra areas (besides Area) that get the default
	// bindings too (e.g. Panel.Toggle works in both Shell and Terminal).
	DefaultAreas []string
	// MenuPath is the top-level menu the action appears in ("File",
	// "Edit", ...). Empty means the action is not listed in menus.
	MenuPath string
	// Checked, when set, reports the toggle state shown in menus ("√ ").
	Checked func() bool
	Handler func() bool
}

// DisplayLabel returns the localized label, falling back to the English one.
func (a Action) DisplayLabel() string {
	if a.LabelKey != "" {
		if s := Msg(a.LabelKey); !strings.HasPrefix(s, "{") {
			return s
		}
	}
	return a.Label
}

// DisplayDescription returns the localized description, falling back to English.
func (a Action) DisplayDescription() string {
	if a.DescKey != "" {
		if s := Msg(a.DescKey); !strings.HasPrefix(s, "{") {
			return s
		}
	}
	return a.Description
}

var actionRegistry = make(map[string]Action)

// actionOrder keeps registration order so generated menus are deterministic.
var actionOrder []string

// RegisterAction adds an action to the global registry.
func RegisterAction(action Action) {
	key := strings.ToLower(action.Name)
	if _, exists := actionRegistry[key]; !exists {
		actionOrder = append(actionOrder, key)
	}
	actionRegistry[key] = action
}

// RunAction executes an action by name if it exists.
func RunAction(name string) bool {
	if a, ok := actionRegistry[strings.ToLower(name)]; ok && a.Handler != nil {
		return a.Handler()
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

// GetOrderedActions returns all registered actions in registration order.
func GetOrderedActions() []Action {
	actions := make([]Action, 0, len(actionOrder))
	for _, key := range actionOrder {
		actions = append(actions, actionRegistry[key])
	}
	return actions
}

// GetAction returns an action by name.
func GetAction(name string) (Action, bool) {
	a, ok := actionRegistry[strings.ToLower(name)]
	return a, ok
}

// plainLabel strips hotkey markers ('&') from a menu label for contexts
// that cannot render them (keybar, plain lists). '&&' unescapes to '&'.
func plainLabel(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '&' {
			if i+1 < len(s) && s[i+1] == '&' {
				b.WriteByte('&')
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func init() {
	withPF := func(fn func(pf *PanelsFrame)) func() bool {
		return func() bool {
			if pf := findPanelsFrameAnyScreen(); pf != nil {
				fn(pf)
				return true
			}
			return false
		}
	}

	withEditor := func(fn func(ev *EditorView)) func() bool {
		return func() bool {
			if vtui.FrameManager == nil {
				return false
			}
			if ev, ok := vtui.FrameManager.GetTopFrame().(*EditorView); ok {
				fn(ev)
				return true
			}
			return false
		}
	}

	withViewer := func(fn func(vv *ViewerView)) func() bool {
		return func() bool {
			if vtui.FrameManager == nil {
				return false
			}
			if vv, ok := vtui.FrameManager.GetTopFrame().(*ViewerView); ok {
				fn(vv)
				return true
			}
			return false
		}
	}

	editorState := func(fn func(ev *EditorView) bool) func() bool {
		return func() bool {
			if vtui.FrameManager == nil {
				return false
			}
			if ev, ok := vtui.FrameManager.GetTopFrame().(*EditorView); ok {
				return fn(ev)
			}
			return false
		}
	}

	viewerState := func(fn func(vv *ViewerView) bool) func() bool {
		return func() bool {
			if vtui.FrameManager == nil {
				return false
			}
			if vv, ok := vtui.FrameManager.GetTopFrame().(*ViewerView); ok {
				return fn(vv)
			}
			return false
		}
	}

	// --- Common actions (available in every area) ---
	RegisterAction(Action{
		Name:        "App.ScreenGrab",
		Area:        "Common",
		Label:       "Screen Grab",
		LabelKey:    "Action.App.ScreenGrab",
		Description: "Select and copy a screen region",
		DescKey:     "Action.App.ScreenGrab.Desc",
		DefaultKeys: []string{"AltIns"},
		MenuPath:    "File",
		Handler:     func() bool { OpenGrabber(); return true },
	})

	// --- Shell (panels) actions ---
	RegisterAction(Action{
		Name:        "File.Copy",
		Area:        "Shell",
		Label:       "Copy",
		Description: "Copy selected files or current file",
		DefaultKeys: []string{"F5"},
		Handler:     withPF(func(pf *PanelsFrame) { actionCopyMove(pf, false) }),
	})
	RegisterAction(Action{
		Name:        "File.Move",
		Area:        "Shell",
		Label:       "Move",
		Description: "Rename or move selected files",
		DefaultKeys: []string{"F6"},
		Handler:     withPF(func(pf *PanelsFrame) { actionCopyMove(pf, true) }),
	})
	RegisterAction(Action{
		Name:        "File.Rename",
		Area:        "Shell",
		Label:       "Rename",
		Description: "Rename current file",
		DefaultKeys: []string{"ShiftF6"},
		Handler:     withPF(func(pf *PanelsFrame) { actionRename(pf) }),
	})
	RegisterAction(Action{
		Name:        "File.Delete",
		Area:        "Shell",
		Label:       "Delete",
		Description: "Delete selected files",
		DefaultKeys: []string{"F8"},
		Handler:     withPF(func(pf *PanelsFrame) { actionDelete(pf) }),
	})
	RegisterAction(Action{
		Name:        "File.MakeDir",
		Area:        "Shell",
		Label:       "Make Folder",
		Description: "Create a new directory",
		DefaultKeys: []string{"F7"},
		Handler:     withPF(func(pf *PanelsFrame) { actionMkDir(pf) }),
	})
	RegisterAction(Action{
		Name:        "File.Edit",
		Area:        "Shell",
		Label:       "Edit",
		Description: "Open file in editor",
		DefaultKeys: []string{"F4"},
		Handler:     withPF(func(pf *PanelsFrame) { actionEditFile(pf) }),
	})
	RegisterAction(Action{
		Name:        "File.View",
		Area:        "Shell",
		Label:       "View",
		Description: "Open file in viewer",
		DefaultKeys: []string{"F3"},
		Handler:     withPF(func(pf *PanelsFrame) { actionViewFile(pf) }),
	})
	RegisterAction(Action{
		Name:        "File.New",
		Area:        "Shell",
		Label:       "New File",
		Description: "Create and open a new file in editor",
		DefaultKeys: []string{"ShiftF4"},
		Handler:     withPF(func(pf *PanelsFrame) { actionNewFile(pf) }),
	})
	RegisterAction(Action{
		Name:        "File.Find",
		Area:        "Shell",
		Label:       "Find File",
		Description: "Search for files",
		DefaultKeys: []string{"AltF7"},
		Handler:     withPF(func(pf *PanelsFrame) { actionFindFile(pf) }),
	})

	RegisterAction(Action{
		Name:        "Panel.Rescan",
		Area:        "Shell",
		Label:       "Rescan",
		Description: "Refresh panel contents",
		DefaultKeys: []string{"CtrlR"},
		Handler:     withPF(func(pf *PanelsFrame) { pf.RefreshAll() }),
	})
	RegisterAction(Action{
		Name:        "Panel.Swap",
		Area:        "Shell",
		Label:       "Swap Panels",
		Description: "Swap left and right panels",
		DefaultKeys: []string{"CtrlU"},
		Handler:     withPF(func(pf *PanelsFrame) { vtui.FrameManager.EmitCommand(CmSwapPanels, nil) }),
	})
	RegisterAction(Action{
		Name:         "Panel.Toggle",
		Area:         "Shell",
		Label:        "Toggle Panels",
		Description:  "Show or hide panels",
		DefaultKeys:  []string{"CtrlO", "Esc:EscToggle"},
		DefaultAreas: []string{"Terminal"},
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
		Area:        "Shell",
		Label:       "Folders History",
		Description: "Show folders history",
		DefaultKeys: []string{"AltF12"},
		Handler:     withPF(func(pf *PanelsFrame) { actionFoldersHistory(pf) }),
	})
	RegisterAction(Action{
		Name:        "Panel.ViewBrief",
		Area:        "Shell",
		Label:       "Brief Mode",
		Description: "Set active panel to brief mode",
		DefaultKeys: []string{"Ctrl1"},
		Handler:     withPF(func(pf *PanelsFrame) { pf.setPanelViewMode(pf.activeIdx, ViewModeBrief) }),
	})
	RegisterAction(Action{
		Name:        "Panel.ViewMedium",
		Area:        "Shell",
		Label:       "Medium Mode",
		Description: "Set active panel to medium mode",
		DefaultKeys: []string{"Ctrl2"},
		Handler:     withPF(func(pf *PanelsFrame) { pf.setPanelViewMode(pf.activeIdx, ViewModeMedium) }),
	})
	RegisterAction(Action{
		Name:        "Panel.ViewDetailed",
		Area:        "Shell",
		Label:       "Detailed Mode",
		Description: "Set active panel to detailed mode",
		DefaultKeys: []string{"Ctrl3"},
		Handler:     withPF(func(pf *PanelsFrame) { pf.setPanelViewMode(pf.activeIdx, ViewModeDetailed) }),
	})
	RegisterAction(Action{
		Name:        "Panel.ViewWide",
		Area:        "Shell",
		Label:       "Wide Mode",
		Description: "Set active panel to wide mode",
		DefaultKeys: []string{"Ctrl4"},
		Handler:     withPF(func(pf *PanelsFrame) { pf.setWidePanel(pf.activeIdx) }),
	})
	RegisterAction(Action{
		Name:        "Panel.Bookmarks",
		Area:        "Shell",
		Label:       "Bookmarks",
		Description: "Show folder bookmarks dialog",
		Handler:     withPF(func(pf *PanelsFrame) { ShowBookmarksDialog(pf) }),
	})
	RegisterAction(Action{
		Name:        "Panel.CommandHistory",
		Area:        "Shell",
		Label:       "Command History",
		Description: "Show command line history",
		DefaultKeys: []string{"AltF8"},
		Handler:     withPF(func(pf *PanelsFrame) { actionCommandHistory(pf) }),
	})

	RegisterAction(Action{
		Name:        "Settings.Language",
		Area:        "Shell",
		Label:       "Language",
		Description: "Open language selection dialog",
		Handler:     withPF(func(pf *PanelsFrame) { actionLanguage(pf) }),
	})
	RegisterAction(Action{
		Name:        "Settings.HelpLanguage",
		Area:        "Shell",
		Label:       "Help Language",
		Description: "Open help language selection dialog",
		Handler:     withPF(func(pf *PanelsFrame) { actionHelpLanguage(pf) }),
	})
	RegisterAction(Action{
		Name:        "Settings.Plugins",
		Area:        "Shell",
		Label:       "Plugins Menu",
		Description: "Manage plugins dialog",
		Handler:     withPF(func(pf *PanelsFrame) { actionManagePlugins(pf) }),
	})
	RegisterAction(Action{
		Name:        "Settings.Panel",
		Area:        "Shell",
		Label:       "Panel Settings",
		Description: "Open panel settings dialog",
		Handler:     withPF(func(pf *PanelsFrame) { actionPanelSettings(pf) }),
	})
	RegisterAction(Action{
		Name:        "Settings.Editor",
		Area:        "Shell",
		Label:       "Editor Settings",
		Description: "Open editor settings dialog",
		Handler:     withPF(func(pf *PanelsFrame) { actionEditorSettings(pf) }),
	})
	RegisterAction(Action{
		Name:        "Settings.Colorer",
		Area:        "Shell",
		Label:       "Colorer Settings",
		Description: "Open Colorer settings dialog",
		Handler:     withPF(func(pf *PanelsFrame) { actionColorerSettings(pf) }),
	})
	RegisterAction(Action{
		Name:        "Settings.Appearance",
		Area:        "Shell",
		Label:       "Appearance Settings",
		Description: "Open appearance settings dialog",
		Handler:     withPF(func(pf *PanelsFrame) { actionAppearanceSettings(pf) }),
	})
	RegisterAction(Action{
		Name:        "Settings.Confirmations",
		Area:        "Shell",
		Label:       "Confirmations Settings",
		Description: "Open confirmations settings dialog",
		Handler:     withPF(func(pf *PanelsFrame) { actionConfirmationsSettings(pf) }),
	})
	RegisterAction(Action{
		Name:        "Settings.Hotkeys",
		Area:        "Shell",
		Label:       "Hotkey Configuration",
		Description: "Open Hotkey Configurator",
		Handler:     withPF(func(pf *PanelsFrame) { actionHotkeyConfig(pf) }),
	})

	// --- Terminal actions ---
	RegisterAction(Action{
		Name:        "Terminal.ViewLog",
		Area:        "Terminal",
		Label:       "View Terminal Log",
		Description: "Open terminal log in viewer",
		Handler:     withPF(func(pf *PanelsFrame) { actionViewTerminalLog(pf) }),
	})
	RegisterAction(Action{
		Name:        "Terminal.EditLog",
		Area:        "Terminal",
		Label:       "Edit Terminal Log",
		Description: "Open terminal log in editor",
		Handler:     withPF(func(pf *PanelsFrame) { actionEditTerminalLog(pf) }),
	})

	// --- Editor actions (menu order follows registration order) ---
	RegisterAction(Action{
		Name:        "Editor.Save",
		Area:        "Editor",
		Label:       "Save",
		LabelKey:    "Action.Editor.Save",
		Description: "Save file",
		DescKey:     "Action.Editor.Save.Desc",
		DefaultKeys: []string{"F2"},
		MenuPath:    "File",
		Handler:     withEditor(func(ev *EditorView) { ev.SaveToFile(nil) }),
	})
	RegisterAction(Action{
		Name:        "Editor.SwitchToViewer",
		Area:        "Editor",
		Label:       "Switch to Viewer",
		LabelKey:    "Action.Editor.SwitchToViewer",
		Description: "Switch to viewer mode",
		DescKey:     "Action.Editor.SwitchToViewer.Desc",
		DefaultKeys: []string{"F6"},
		MenuPath:    "File",
		Handler:     withEditor(func(ev *EditorView) { vtui.FrameManager.EmitCommand(CmSwitchToViewer, ev) }),
	})
	RegisterAction(Action{
		Name:        "Editor.Quit",
		Area:        "Editor",
		Label:       "Quit",
		LabelKey:    "Action.Editor.Quit",
		Description: "Close editor",
		DescKey:     "Action.Editor.Quit.Desc",
		DefaultKeys: []string{"F10", "Esc", "F4"},
		MenuPath:    "File",
		Handler:     withEditor(func(ev *EditorView) { ev.tryClose() }),
	})

	RegisterAction(Action{
		Name:        "Editor.Undo",
		Area:        "Editor",
		Label:       "Undo",
		LabelKey:    "Action.Editor.Undo",
		Description: "Undo last change",
		DescKey:     "Action.Editor.Undo.Desc",
		DefaultKeys: []string{"CtrlZ"},
		MenuPath:    "Edit",
		Handler:     withEditor(func(ev *EditorView) { ev.Undo() }),
	})
	RegisterAction(Action{
		Name:        "Editor.Redo",
		Area:        "Editor",
		Label:       "Redo",
		LabelKey:    "Action.Editor.Redo",
		Description: "Redo last undone change",
		DescKey:     "Action.Editor.Redo.Desc",
		DefaultKeys: []string{"CtrlShiftZ"},
		MenuPath:    "Edit",
		Handler:     withEditor(func(ev *EditorView) { ev.Redo() }),
	})
	RegisterAction(Action{
		Name:        "Editor.Copy",
		Area:        "Editor",
		Label:       "Copy",
		LabelKey:    "Action.Editor.Copy",
		Description: "Copy selection to clipboard",
		DescKey:     "Action.Editor.Copy.Desc",
		DefaultKeys: []string{"CtrlC", "CtrlIns"},
		MenuPath:    "Edit",
		Handler: withEditor(func(ev *EditorView) {
			if ev.selActive || ev.rectSelActive {
				ev.CopySelection()
			}
		}),
	})
	RegisterAction(Action{
		Name:        "Editor.Cut",
		Area:        "Editor",
		Label:       "Cut",
		LabelKey:    "Action.Editor.Cut",
		Description: "Cut selection to clipboard",
		DescKey:     "Action.Editor.Cut.Desc",
		// Ctrl+X is intentionally not a default key: the editor keeps it as
		// the classic down-movement alias when no selection exists (see
		// EditorView.ProcessKey), and delegates to this action when there
		// is a selection. Shift+Del is the advertised Cut hotkey.
		DefaultKeys: []string{"ShiftDel"},
		MenuPath:    "Edit",
		Handler: withEditor(func(ev *EditorView) {
			if ev.selActive || ev.rectSelActive {
				ev.CopySelection()
				ev.DeleteSelection()
			}
		}),
	})
	RegisterAction(Action{
		Name:        "Editor.Paste",
		Area:        "Editor",
		Label:       "Paste",
		LabelKey:    "Action.Editor.Paste",
		Description: "Paste text from clipboard",
		DescKey:     "Action.Editor.Paste.Desc",
		DefaultKeys: []string{"ShiftIns", "CtrlV"},
		MenuPath:    "Edit",
		Handler: withEditor(func(ev *EditorView) {
			if text := vtui.GetClipboard(); text != "" {
				ev.PasteText(text)
			}
		}),
	})
	RegisterAction(Action{
		Name:        "Editor.SelectAll",
		Area:        "Editor",
		Label:       "Select All",
		LabelKey:    "Action.Editor.SelectAll",
		Description: "Select all text",
		DescKey:     "Action.Editor.SelectAll.Desc",
		DefaultKeys: []string{"CtrlA"},
		MenuPath:    "Edit",
		Handler: withEditor(func(ev *EditorView) {
			ev.rectSelActive = false
			ev.selActive = true
			ev.selAnchorOffset = 0
			lastLine := ev.li.LineCount() - 1
			ev.CursorLine = lastLine
			ev.CursorPos = ev.getLineLength(lastLine)
			ev.ensureCursorVisible()
		}),
	})
	RegisterAction(Action{
		Name:        "Editor.DeleteLine",
		Area:        "Editor",
		Label:       "Delete Line",
		LabelKey:    "Action.Editor.DeleteLine",
		Description: "Delete current line",
		DescKey:     "Action.Editor.DeleteLine.Desc",
		DefaultKeys: []string{"CtrlY"},
		MenuPath:    "Edit",
		Handler:     withEditor(func(ev *EditorView) { ev.DeleteCurrentLine() }),
	})
	RegisterAction(Action{
		Name:        "Editor.ToggleOvertype",
		Area:        "Editor",
		Label:       "Insert/Overtype",
		LabelKey:    "Action.Editor.ToggleOvertype",
		Description: "Toggle insert/overtype mode",
		DescKey:     "Action.Editor.ToggleOvertype.Desc",
		DefaultKeys: []string{"Ins"},
		MenuPath:    "Edit",
		Checked:     editorState(func(ev *EditorView) bool { return ev.overtype }),
		Handler: withEditor(func(ev *EditorView) {
			ev.overtype = !ev.overtype
			ev.ensureCursorVisible()
		}),
	})

	RegisterAction(Action{
		Name:        "Editor.Search",
		Area:        "Editor",
		Label:       "Search",
		LabelKey:    "Action.Editor.Search",
		Description: "Find text",
		DescKey:     "Action.Editor.Search.Desc",
		DefaultKeys: []string{"F7"},
		MenuPath:    "Search",
		Handler:     withEditor(func(ev *EditorView) { vtui.FrameManager.EmitCommand(CmSearch, nil) }),
	})
	RegisterAction(Action{
		Name:        "Editor.Replace",
		Area:        "Editor",
		Label:       "Replace",
		LabelKey:    "Action.Editor.Replace",
		Description: "Replace text",
		DescKey:     "Action.Editor.Replace.Desc",
		DefaultKeys: []string{"CtrlF7"},
		MenuPath:    "Search",
		Handler:     withEditor(func(ev *EditorView) { vtui.FrameManager.EmitCommand(CmReplace, nil) }),
	})
	RegisterAction(Action{
		Name:        "Editor.SearchNext",
		Area:        "Editor",
		Label:       "Search Next",
		LabelKey:    "Action.Editor.SearchNext",
		Description: "Continue search",
		DescKey:     "Action.Editor.SearchNext.Desc",
		DefaultKeys: []string{"ShiftF7"},
		MenuPath:    "Search",
		Handler: withEditor(func(ev *EditorView) {
			if LastEditorSearch != "" {
				ev.Search(LastEditorSearch, LastEditorSearchCase, LastEditorSearchReverse, LastEditorSearchRegexp, LastEditorSearchWholeWord, true)
			}
		}),
	})

	RegisterAction(Action{
		Name:        "Editor.WordWrap",
		Area:        "Editor",
		Label:       "Word Wrap",
		LabelKey:    "Action.Editor.WordWrap",
		Description: "Toggle word wrap",
		DescKey:     "Action.Editor.WordWrap.Desc",
		DefaultKeys: []string{"F3"},
		MenuPath:    "Options",
		Checked:     editorState(func(ev *EditorView) bool { return ev.WordWrap }),
		Handler: withEditor(func(ev *EditorView) {
			ev.WordWrap = !ev.WordWrap
			ev.ScrollLeft = 0
			ev.clearCaches()
			ev.ensureCursorVisible()
		}),
	})
	RegisterAction(Action{
		Name:        "Editor.ShowWhitespaces",
		Area:        "Editor",
		Label:       "Show Whitespaces",
		LabelKey:    "Action.Editor.ShowWhitespaces",
		Description: "Toggle visible whitespaces",
		DescKey:     "Action.Editor.ShowWhitespaces.Desc",
		DefaultKeys: []string{"F5"},
		MenuPath:    "Options",
		Checked:     editorState(func(ev *EditorView) bool { return ev.ShowWhitespaces }),
		Handler:     withEditor(func(ev *EditorView) { ev.ShowWhitespaces = !ev.ShowWhitespaces }),
	})
	RegisterAction(Action{
		Name:        "Editor.CodepageNext",
		Area:        "Editor",
		Label:       "Next Codepage",
		LabelKey:    "Action.Editor.CodepageNext",
		Description: "Cycle to next codepage",
		DescKey:     "Action.Editor.CodepageNext.Desc",
		DefaultKeys: []string{"F8"},
		MenuPath:    "Options",
		Handler: withEditor(func(ev *EditorView) {
			next := vfs.GetNextFastSwitchCodepage(ev.Codepage)
			ev.ReloadWithCodepage(next)
			vtui.ShowToast(fmt.Sprintf("Codepage: %d", next), time.Second)
		}),
	})
	RegisterAction(Action{
		Name:        "Editor.CodepageMenu",
		Area:        "Editor",
		Label:       "Codepage Menu",
		LabelKey:    "Action.Editor.CodepageMenu",
		Description: "Select codepage",
		DescKey:     "Action.Editor.CodepageMenu.Desc",
		DefaultKeys: []string{"ShiftF8"},
		MenuPath:    "Options",
		Handler:     withEditor(func(ev *EditorView) { ev.showCodepageDialog() }),
	})

	RegisterAction(Action{
		Name:        "Editor.InsertLeftPanelPath",
		Area:        "Editor",
		Label:       "Insert Left Panel Path",
		LabelKey:    "Action.Editor.InsertLeftPanelPath",
		Description: "Insert the left panel's path at cursor",
		DescKey:     "Action.Editor.InsertLeftPanelPath.Desc",
		DefaultKeys: []string{"CtrlVK_DB"},
		MenuPath:    "Insert",
		Handler: withEditor(func(ev *EditorView) {
			if s := leftPanelPathForEditor(); s != "" {
				ev.insertTextAtCursor([]byte(s))
			}
		}),
	})
	RegisterAction(Action{
		Name:        "Editor.InsertRightPanelPath",
		Area:        "Editor",
		Label:       "Insert Right Panel Path",
		LabelKey:    "Action.Editor.InsertRightPanelPath",
		Description: "Insert the right panel's path at cursor",
		DescKey:     "Action.Editor.InsertRightPanelPath.Desc",
		DefaultKeys: []string{"CtrlVK_DD"},
		MenuPath:    "Insert",
		Handler: withEditor(func(ev *EditorView) {
			if s := rightPanelPathForEditor(); s != "" {
				ev.insertTextAtCursor([]byte(s))
			}
		}),
	})
	RegisterAction(Action{
		Name:        "Editor.InsertActivePanelFileName",
		Area:        "Editor",
		Label:       "Insert Current File Name",
		LabelKey:    "Action.Editor.InsertActivePanelFileName",
		Description: "Insert the active panel's current file name at cursor",
		DescKey:     "Action.Editor.InsertActivePanelFileName.Desc",
		DefaultKeys: []string{"CtrlEnter"},
		MenuPath:    "Insert",
		Handler: withEditor(func(ev *EditorView) {
			if s := activePanelNameForEditor(); s != "" {
				ev.insertTextAtCursor([]byte(s))
			}
		}),
	})
	RegisterAction(Action{
		Name:        "Editor.DeleteSpacersForward",
		Area:        "Editor",
		Label:       "Delete Word Forward",
		LabelKey:    "Action.Editor.DeleteSpacersForward",
		Description: "Delete spaces and word forward",
		DescKey:     "Action.Editor.DeleteSpacersForward.Desc",
		DefaultKeys: []string{"CtrlDel"},
		MenuPath:    "Insert",
		Handler:     withEditor(func(ev *EditorView) { ev.deleteSpacersForward() }),
	})

	// --- Viewer actions ---
	RegisterAction(Action{
		Name:        "Viewer.SwitchToEditor",
		Area:        "Viewer",
		Label:       "Switch to Editor",
		LabelKey:    "Action.Viewer.SwitchToEditor",
		Description: "Switch to editor mode",
		DescKey:     "Action.Viewer.SwitchToEditor.Desc",
		DefaultKeys: []string{"F6"},
		MenuPath:    "File",
		Handler:     withViewer(func(vv *ViewerView) { vtui.FrameManager.EmitCommand(CmSwitchToEditor, vv) }),
	})
	RegisterAction(Action{
		Name:        "Viewer.Quit",
		Area:        "Viewer",
		Label:       "Quit",
		LabelKey:    "Action.Viewer.Quit",
		Description: "Close viewer",
		DescKey:     "Action.Viewer.Quit.Desc",
		DefaultKeys: []string{"Esc", "F10", "F3"},
		MenuPath:    "File",
		Handler:     withViewer(func(vv *ViewerView) { vv.Close() }),
	})

	RegisterAction(Action{
		Name:        "Viewer.WrapMode",
		Area:        "Viewer",
		Label:       "Wrap Mode",
		LabelKey:    "Action.Viewer.WrapMode",
		Description: "Toggle word wrap",
		DescKey:     "Action.Viewer.WrapMode.Desc",
		DefaultKeys: []string{"F2"},
		MenuPath:    "View",
		Checked:     viewerState(func(vv *ViewerView) bool { return vv.WrapMode }),
		Handler:     withViewer(func(vv *ViewerView) { vv.WrapMode = !vv.WrapMode }),
	})
	RegisterAction(Action{
		Name:        "Viewer.HexMode",
		Area:        "Viewer",
		Label:       "Hex Mode",
		LabelKey:    "Action.Viewer.HexMode",
		Description: "Toggle hex view",
		DescKey:     "Action.Viewer.HexMode.Desc",
		DefaultKeys: []string{"F4"},
		MenuPath:    "View",
		Checked:     viewerState(func(vv *ViewerView) bool { return vv.HexMode }),
		Handler: withViewer(func(vv *ViewerView) {
			vv.HexMode = !vv.HexMode
			if vv.HexMode {
				vv.TopOffset &= ^int64(0xF)
			}
		}),
	})

	RegisterAction(Action{
		Name:        "Viewer.Search",
		Area:        "Viewer",
		Label:       "Search",
		LabelKey:    "Action.Viewer.Search",
		Description: "Find text",
		DescKey:     "Action.Viewer.Search.Desc",
		DefaultKeys: []string{"F7"},
		MenuPath:    "Search",
		Handler:     withViewer(func(vv *ViewerView) { vtui.FrameManager.EmitCommand(CmSearch, nil) }),
	})

	RegisterAction(Action{
		Name:        "Viewer.CodepageNext",
		Area:        "Viewer",
		Label:       "Next Codepage",
		LabelKey:    "Action.Viewer.CodepageNext",
		Description: "Cycle to next codepage",
		DescKey:     "Action.Viewer.CodepageNext.Desc",
		DefaultKeys: []string{"F8"},
		MenuPath:    "Options",
		Handler: withViewer(func(vv *ViewerView) {
			next := vfs.GetNextFastSwitchCodepage(vv.Codepage)
			vv.ReloadWithCodepage(next)
			vtui.ShowToast(fmt.Sprintf("Codepage: %d", next), time.Second)
		}),
	})
	RegisterAction(Action{
		Name:        "Viewer.CodepageMenu",
		Area:        "Viewer",
		Label:       "Codepage Menu",
		LabelKey:    "Action.Viewer.CodepageMenu",
		Description: "Select codepage",
		DescKey:     "Action.Viewer.CodepageMenu.Desc",
		DefaultKeys: []string{"ShiftF8"},
		MenuPath:    "Options",
		Handler:     withViewer(func(vv *ViewerView) { vv.showCodepageDialog() }),
	})
}
