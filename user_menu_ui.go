package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

// User menu UI: F2 opens a vertical menu loaded from FarMenu.ini in the
// current directory (walking up to the root), or from FarMenu.ini next
// to the executable, or from ~/.config/f4/settings/main_menu.ini.
// Shift+F2 cycles the source as far2l does.

// MenuMode picks where the menu items come from.
type MenuMode int

const (
	MenuModeLocal MenuMode = iota // FarMenu.ini in cwd or any parent
	MenuModeFar                   // FarMenu.ini next to the f4 binary
	MenuModeMain                  // main_menu.ini in user config
)

const farMenuFileName = "FarMenu.ini"

// MainMenuFilePath returns the user-config location for the persistent
// main menu. The filename matches far2l so the same file can be shared
// between ~/.config/far2l/settings/user_menu.ini and the f4 directory
// without renaming.
func MainMenuFilePath() string {
	configDir, _ := os.UserConfigDir()
	return filepath.Join(configDir, "f4", "settings", "user_menu.ini")
}

// findLocalFarMenu walks startDir upward looking for FarMenu.ini.
func findLocalFarMenu(startDir string) (path string, found bool) {
	dir := startDir
	for {
		candidate := filepath.Join(dir, farMenuFileName)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// findFarMenuNearBinary looks for FarMenu.ini next to the running executable.
func findFarMenuNearBinary() (path string, found bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	candidate := filepath.Join(filepath.Dir(exe), farMenuFileName)
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		return candidate, true
	}
	return "", false
}

// loadFarMenuFile reads a FarMenu.ini (text format) into a slice.
func loadFarMenuFile(path string) ([]UserMenuItem, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseFarMenu(f)
}

// loadMenuForMode returns (items, title, sourcePath, ok). ok=false means
// this mode has no data — caller may want to try the next mode.
func loadMenuForMode(pf *PanelsFrame, mode MenuMode) (items []UserMenuItem, title, source string, ok bool) {
	switch mode {
	case MenuModeLocal:
		fsp, _ := pf.panels[pf.activeIdx].(*FileSystemPanel)
		if fsp == nil {
			return nil, Msg("UserMenu.LocalMenuTitle"), "", false
		}
		path, found := findLocalFarMenu(fsp.vfs.GetPath())
		if !found {
			return nil, Msg("UserMenu.LocalMenuTitle"), "", false
		}
		loaded, err := loadFarMenuFile(path)
		if err != nil {
			return nil, Msg("UserMenu.LocalMenuTitle"), path, false
		}
		return loaded, Msg("UserMenu.LocalMenuTitle"), path, true
	case MenuModeFar:
		path, found := findFarMenuNearBinary()
		if !found {
			return nil, fmt.Sprintf("%s (%s)", Msg("UserMenu.MainMenuTitle"), Msg("UserMenu.MainMenuFAR")), "", false
		}
		loaded, err := loadFarMenuFile(path)
		if err != nil {
			return nil, fmt.Sprintf("%s (%s)", Msg("UserMenu.MainMenuTitle"), Msg("UserMenu.MainMenuFAR")), path, false
		}
		return loaded, fmt.Sprintf("%s (%s)", Msg("UserMenu.MainMenuTitle"), Msg("UserMenu.MainMenuFAR")), path, true
	case MenuModeMain:
		path := MainMenuFilePath()
		loaded, err := LoadMainMenu(path)
		if err != nil {
			return nil, Msg("UserMenu.MainMenuTitle"), path, false
		}
		return loaded, Msg("UserMenu.MainMenuTitle"), path, len(loaded) > 0
	}
	return nil, "", "", false
}

// resolveMenuStart finds the first non-empty mode at or after initial,
// returning items/title and the mode actually used.
func resolveMenuStart(pf *PanelsFrame, initial MenuMode) (items []UserMenuItem, title string, mode MenuMode) {
	for offset := 0; offset < 3; offset++ {
		m := MenuMode((int(initial) + offset) % 3)
		if loaded, t, _, ok := loadMenuForMode(pf, m); ok {
			return loaded, t, m
		}
	}
	// Nothing found anywhere — fall back to main with an empty list so
	// the user still sees the menu chrome and can press Shift+F2 / Esc.
	_, t, _, _ := loadMenuForMode(pf, MenuModeMain)
	return nil, t, MenuModeMain
}

// userMenuState shares ownership of the pushed VMenu chain so we can
// close the whole stack on Shift+F2 cycle or shellable-action.
type userMenuState struct {
	pf    *PanelsFrame
	mode  MenuMode
	stack []*vtui.VMenu
}

// ShowUserMenu is the entry point. It loads the user menu starting from
// the local (cwd-relative) mode and pushes a modal VMenu.
func ShowUserMenu(pf *PanelsFrame) {
	items, title, mode := resolveMenuStart(pf, MenuModeLocal)
	s := &userMenuState{pf: pf, mode: mode}
	s.pushLevel(items, title)
	_ = title // referenced for future submenu breadcrumbs
}

func (s *userMenuState) pushLevel(items []UserMenuItem, title string) {
	menu := vtui.NewVMenu(" " + title + " ")
	s.stack = append(s.stack, menu)

	// Map F1..F24 hotkeys to item indices for fast lookup in OnKeyDown.
	// vtui already handles single-char (&-prefixed) hotkeys natively.
	fnKeyTarget := map[uint32]int{}

	for i, it := range items {
		if it.IsSeparator() {
			menu.AddSeparator()
			continue
		}
		if fn := parseFunctionKey(it.HotKey); fn > 0 {
			fnKeyTarget[fn] = i
		}
		menu.AddItem(vtui.MenuItem{
			Text:     formatMenuItemText(it),
			UserData: i,
		})
	}

	w, h := menuSize(s.pf, menu.GetItemCount(), items)
	x := (s.pf.lastW - w) / 2
	y := (s.pf.lastH - h) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	menu.SetPosition(x, y, x+w-1, y+h-1)

	menu.OnKeyDown = func(e *vtinput.InputEvent) bool {
		if !e.KeyDown {
			return false
		}
		shift := e.ControlKeyState&vtinput.ShiftPressed != 0
		ctrl := e.ControlKeyState&(vtinput.LeftCtrlPressed|vtinput.RightCtrlPressed) != 0
		alt := e.ControlKeyState&(vtinput.LeftAltPressed|vtinput.RightAltPressed) != 0

		// Shift+F2 cycles the menu source mode.
		if e.VirtualKeyCode == vtinput.VK_F2 && shift && !ctrl && !alt {
			s.closeAll()
			next := MenuMode((int(s.mode) + 1) % 3)
			vtui.FrameManager.PostTask(func() {
				items, title, mode := resolveMenuStart(s.pf, next)
				s.mode = mode
				s.stack = nil
				s.pushLevel(items, title)
			})
			return true
		}
		// F1..F12: jump to the item whose HotKey is "F<n>".
		if !shift && !ctrl && !alt && e.VirtualKeyCode >= vtinput.VK_F1 && e.VirtualKeyCode <= vtinput.VK_F12 {
			fn := uint32(e.VirtualKeyCode-vtinput.VK_F1) + 1
			if uiIdx, ok := findMenuItemByUserData(menu, fnKeyTarget[fn]); ok {
				menu.SetSelectPos(uiIdx)
				menu.ProcessKey(&vtinput.InputEvent{
					Type: vtinput.KeyEventType, KeyDown: true,
					VirtualKeyCode: vtinput.VK_RETURN,
				})
				return true
			}
		}
		// Right arrow → enter submenu (matches Enter for these items).
		if !shift && !ctrl && !alt && e.VirtualKeyCode == vtinput.VK_RIGHT {
			pos := menu.SelectPos
			if pos >= 0 && pos < len(menu.Items) {
				if idx, ok := menu.Items[pos].UserData.(int); ok && idx >= 0 && idx < len(items) {
					if items[idx].IsSubmenu() {
						menu.ProcessKey(&vtinput.InputEvent{
							Type: vtinput.KeyEventType, KeyDown: true,
							VirtualKeyCode: vtinput.VK_RETURN,
						})
						return true
					}
				}
			}
		}
		return false
	}

	menu.OnAction = func(uiIdx int) {
		if uiIdx < 0 || uiIdx >= len(menu.Items) {
			return
		}
		idx, ok := menu.Items[uiIdx].UserData.(int)
		if !ok || idx < 0 || idx >= len(items) {
			return
		}
		chosen := items[idx]
		menu.Close()
		// Defer to post-task so the menu is fully popped before we either
		// push a child or set the command line.
		if chosen.IsSubmenu() {
			parentTitle := title
			label := stripAmpersand(chosen.Label)
			vtui.FrameManager.PostTask(func() {
				// Track the now-closed parent's removal from our stack.
				s.popClosed()
				s.pushLevel(chosen.Submenu, parentTitle+" -> "+label)
			})
		} else {
			cmds := chosen.Commands
			vtui.FrameManager.PostTask(func() {
				s.popClosed()
				executeMenuCommands(s.pf, cmds)
			})
		}
	}

	vtui.FrameManager.Push(menu)
}

func (s *userMenuState) closeAll() {
	for _, m := range s.stack {
		m.Close()
	}
	s.stack = nil
}

// popClosed drops the topmost menu from our local stack after it has
// been dismissed by vtui.
func (s *userMenuState) popClosed() {
	if len(s.stack) > 0 {
		s.stack = s.stack[:len(s.stack)-1]
	}
}

// formatMenuItemText builds the displayed string for an item:
//
//	"&a    label"   – single-char hotkey, vtui underlines via the '&'
//	"F3    label"   – function key (vtui has no special handling needed)
//	"      label"   – no hotkey
func formatMenuItemText(it UserMenuItem) string {
	const labelCol = 6
	label := escapeAmpersand(it.Label)
	if fn := parseFunctionKey(it.HotKey); fn > 0 {
		return fmt.Sprintf("%-*s%s", labelCol, it.HotKey, label)
	}
	if it.HotKey == "" {
		return strings.Repeat(" ", labelCol) + label
	}
	// Single char (printable) — let vtui wire up the hotkey.
	return fmt.Sprintf("&%s%s%s", it.HotKey, strings.Repeat(" ", labelCol-1-len(it.HotKey)), label)
}

// escapeAmpersand doubles literal '&' so vtui doesn't treat them as
// hotkey markers in the label portion.
func escapeAmpersand(s string) string {
	return strings.ReplaceAll(s, "&", "&&")
}

func stripAmpersand(s string) string {
	// Drop a single (non-doubled) '&' for display in breadcrumbs.
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '&' {
			if i+1 < len(s) && s[i+1] == '&' {
				b.WriteByte('&')
				i++
				continue
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// parseFunctionKey returns 1..24 for "F1".."F24", or 0 otherwise.
func parseFunctionKey(hk string) uint32 {
	if len(hk) < 2 || (hk[0] != 'F' && hk[0] != 'f') {
		return 0
	}
	n := 0
	for _, c := range hk[1:] {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
		if n > 24 {
			return 0
		}
	}
	if n < 1 || n > 24 {
		return 0
	}
	return uint32(n)
}

// findMenuItemByUserData returns the UI index of the item whose UserData
// matches itemIdx.
func findMenuItemByUserData(menu *vtui.VMenu, itemIdx int) (int, bool) {
	for i, it := range menu.Items {
		if v, ok := it.UserData.(int); ok && v == itemIdx {
			return i, true
		}
	}
	return -1, false
}

// menuSize returns suggested width/height for a menu given its content.
func menuSize(pf *PanelsFrame, itemCount int, items []UserMenuItem) (int, int) {
	maxLabel := 20
	for _, it := range items {
		if it.IsSeparator() {
			continue
		}
		w := len(it.Label) + 8 // hotkey + spacing
		if w > maxLabel {
			maxLabel = w
		}
	}
	w := maxLabel + 4
	if pf.lastW > 0 && w > pf.lastW-4 {
		w = pf.lastW - 4
	}
	if w < 24 {
		w = 24
	}
	h := itemCount + 2
	maxH := pf.lastH - 6
	if maxH < 5 {
		maxH = 5
	}
	if h > maxH {
		h = maxH
	}
	if h < 3 {
		h = 3
	}
	return w, h
}

// executeMenuCommands resolves substitutions on a list of commands taken
// from a single menu item and dispatches the result through the command
// line as if the user had typed it and pressed Enter.
func executeMenuCommands(pf *PanelsFrame, commands []string) {
	if len(commands) == 0 || pf.cmdLine == nil {
		return
	}

	active := snapshotPanel(pf, pf.activeIdx)
	passive := snapshotPanel(pf, 1-pf.activeIdx)
	ctx := &SubstContext{Active: active, Passive: passive}

	var lines []string
	for _, raw := range commands {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		// Skip REM-style comments (case-insensitive, with separator) and ::.
		if isMenuComment(t) {
			continue
		}
		// "@" silent prefix isn't supported yet (would need to suppress
		// panel show/hide). Strip it so the command at least runs.
		t = strings.TrimPrefix(t, "@")

		res := SubstFileName(t, ctx)
		if res.Cancelled {
			return
		}
		if res.Command != "" {
			lines = append(lines, res.Command)
		}
	}
	if len(lines) == 0 {
		return
	}

	// Join with ';' so multiple commands run sequentially in one shell.
	// We lose the panel-follows-cd nicety that far2l gets from intercepting
	// each cd in cmdline, but the user can still use a single "cd" command
	// per menu item to get that effect.
	joined := strings.Join(lines, "; ")
	pf.cmdLine.Edit.SetText(joined)
	pf.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_RETURN,
	})
}

func isMenuComment(line string) bool {
	if strings.HasPrefix(line, "::") {
		return true
	}
	if len(line) < 3 {
		return false
	}
	if !strings.EqualFold(line[:3], "REM") {
		return false
	}
	if len(line) == 3 {
		return true
	}
	c := line[3]
	return c == ' ' || c == '\t'
}

// snapshotPanel captures the panel state SubstContext needs. Returns a
// zero-value snapshot when the slot isn't a file system panel.
func snapshotPanel(pf *PanelsFrame, idx int) PanelSnapshot {
	if idx < 0 || idx >= len(pf.panels) {
		return PanelSnapshot{}
	}
	fsp, ok := pf.panels[idx].(*FileSystemPanel)
	if !ok || fsp == nil || fsp.vfs == nil {
		return PanelSnapshot{}
	}
	snap := PanelSnapshot{
		CurDir: fsp.vfs.GetPath(),
	}
	if cur := fsp.getRawSelectedName(); cur != "" && cur != ".." {
		snap.CurrentFile = cur
	}
	for _, name := range fsp.GetSelectedNames() {
		if name == ".." {
			continue
		}
		snap.Marked = append(snap.Marked, name)
	}
	return snap
}
