package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/unxed/f4/piecetable"
	"github.com/unxed/f4/vfs"
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

// submenuMarker is the right-aligned glyph that flags an item as opening
// a nested submenu, matching far2l's choice (vmenu.cpp:1980).
const submenuMarker = "►" // ►

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

// saveFarMenuFile writes a FarMenu.ini text-format file atomically.
func saveFarMenuFile(path string, items []UserMenuItem) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := WriteFarMenu(f, items); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// loadRootForMode reads the current root menu from disk based on the
// source mode and path. Missing files are not an error — they yield an
// empty slice so a fresh menu can be authored.
func loadRootForMode(mode MenuMode, path string) []UserMenuItem {
	switch mode {
	case MenuModeMain:
		items, _ := LoadMainMenu(path)
		return items
	default:
		items, err := loadFarMenuFile(path)
		if err != nil {
			return nil
		}
		return items
	}
}

// saveRootForMode writes items back to the source file using whichever
// on-disk format that source uses (flat INI for the main menu, FarMenu.ini
// text for the per-directory and near-binary files).
func saveRootForMode(mode MenuMode, path string, items []UserMenuItem) error {
	switch mode {
	case MenuModeMain:
		return SaveMainMenu(path, items)
	default:
		return saveFarMenuFile(path, items)
	}
}

// defaultSavePath returns the path Ctrl+F4 should open in the editor
// for a given mode when no menu file exists yet, so the user can
// author one from scratch and save it where the loader will find it.
func defaultSavePath(pf *PanelsFrame, mode MenuMode) string {
	switch mode {
	case MenuModeLocal:
		if fsp, ok := pf.panels[pf.activeIdx].(*FileSystemPanel); ok && fsp != nil && fsp.vfs != nil {
			return filepath.Join(fsp.vfs.GetPath(), farMenuFileName)
		}
		return ""
	case MenuModeFar:
		if exe, err := os.Executable(); err == nil {
			return filepath.Join(filepath.Dir(exe), farMenuFileName)
		}
		return ""
	case MenuModeMain:
		return MainMenuFilePath()
	}
	return ""
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
// returning items/title/source and the mode actually used.
func resolveMenuStart(pf *PanelsFrame, initial MenuMode) (items []UserMenuItem, title string, mode MenuMode, source string) {
	for offset := 0; offset < 3; offset++ {
		m := MenuMode((int(initial) + offset) % 3)
		if loaded, t, src, ok := loadMenuForMode(pf, m); ok {
			return loaded, t, m, src
		}
	}
	// Nothing found anywhere — fall back to main with an empty list so
	// the user still sees the menu chrome and can press Shift+F2 / Esc.
	_, t, _, _ := loadMenuForMode(pf, MenuModeMain)
	return nil, t, MenuModeMain, defaultSavePath(pf, MenuModeMain)
}

// menuLevel snapshots the data needed to recreate a parent menu when
// the user returns to it from a submenu. far2l does the same: the
// inner ProcessSingleMenu loop recreates VMenu with the saved MenuPos
// on EC_CLOSE_LEVEL (usermenu.cpp:738-744). Showing only one menu at a
// time keeps the screen uncluttered.
type menuLevel struct {
	items    []UserMenuItem
	title    string
	selected int
}

// userMenuState carries navigation history across pushLevel invocations.
type userMenuState struct {
	pf         *PanelsFrame
	mode       MenuMode
	sourcePath string // file Ctrl+F4 opens and edits save back to
	history    []menuLevel
}

// ShowUserMenu is the entry point. It loads the user menu starting from
// the local (cwd-relative) mode and pushes a modal VMenu.
func ShowUserMenu(pf *PanelsFrame) {
	items, title, mode, source := resolveMenuStart(pf, MenuModeLocal)
	if source == "" {
		source = defaultSavePath(pf, mode)
	}
	s := &userMenuState{pf: pf, mode: mode, sourcePath: source}
	s.pushLevel(items, title, 0)
}

func (s *userMenuState) pushLevel(items []UserMenuItem, title string, initialSelect int) {
	menu := vtui.NewVMenu(" " + title + " ")

	// Map F1..F24 hotkeys to item indices for fast lookup in OnKeyDown.
	// vtui already handles single-char (&-prefixed) hotkeys natively.
	fnKeyTarget := map[uint32]int{}

	hasSubmenus := false
	for i, it := range items {
		if it.IsSeparator() {
			menu.AddSeparator()
			continue
		}
		if fn := parseFunctionKey(it.HotKey); fn > 0 {
			fnKeyTarget[fn] = i
		}
		mi := vtui.MenuItem{
			Text:     formatMenuItemText(it),
			UserData: i,
		}
		if it.IsSubmenu() {
			// far2l uses U+25BA as the submenu marker (vmenu.cpp:1980);
			// the right-aligned Shortcut slot is the closest analogue.
			mi.Shortcut = submenuMarker
			hasSubmenus = true
		}
		menu.AddItem(mi)
	}

	w, h := menuSize(s.pf, menu.GetItemCount(), items, hasSubmenus)
	x := (s.pf.lastW - w) / 2
	y := (s.pf.lastH - h) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	menu.SetPosition(x, y, x+w-1, y+h-1)
	if initialSelect > 0 && initialSelect < menu.GetItemCount() {
		menu.SetSelectPos(initialSelect)
	}

	menu.OnKeyDown = func(e *vtinput.InputEvent) bool {
		if !e.KeyDown {
			return false
		}
		shift := e.ControlKeyState&vtinput.ShiftPressed != 0
		ctrl := e.ControlKeyState&(vtinput.LeftCtrlPressed|vtinput.RightCtrlPressed) != 0
		alt := e.ControlKeyState&(vtinput.LeftAltPressed|vtinput.RightAltPressed) != 0

		// Shift+F2 cycles the menu source mode. Clear history so the
		// fresh root doesn't try to "return" to the previous mode's chain.
		if e.VirtualKeyCode == vtinput.VK_F2 && shift && !ctrl && !alt {
			s.history = nil
			menu.Close()
			next := MenuMode((int(s.mode) + 1) % 3)
			vtui.FrameManager.PostTask(func() {
				newItems, newTitle, newMode, newSrc := resolveMenuStart(s.pf, next)
				s.mode = newMode
				if newSrc == "" {
					newSrc = defaultSavePath(s.pf, newMode)
				}
				s.sourcePath = newSrc
				s.pushLevel(newItems, newTitle, 0)
			})
			return true
		}
		// Ctrl+F4 edits the menu via a temp file, matching far2l
		// usermenu.cpp:619-678: dump the current root tree as FarMenu.ini
		// text, open the temp file in the editor, on close parse it back
		// and write it to the real source path in that source's native
		// format. If the user didn't change anything we skip the save; if
		// parsing fails we surface an error and keep the original intact.
		if e.VirtualKeyCode == vtinput.VK_F4 && ctrl && !shift && !alt {
			sourcePath := s.sourcePath
			if sourcePath == "" {
				sourcePath = defaultSavePath(s.pf, s.mode)
			}
			if sourcePath == "" {
				return true
			}
			pf := s.pf
			mode := s.mode
			s.history = nil
			menu.Close()
			vtui.FrameManager.PostTask(func() {
				editCurrentMenuInExternalEditor(pf, mode, sourcePath)
			})
			return true
		}
		// Shift+F10 quits the entire menu chain (usermenu.cpp:685).
		if e.VirtualKeyCode == vtinput.VK_F10 && shift && !ctrl && !alt {
			s.history = nil
			menu.Close()
			return true
		}
		// Esc and F10: back one level, or close at the root.
		if !shift && !ctrl && !alt &&
			(e.VirtualKeyCode == vtinput.VK_ESCAPE || e.VirtualKeyCode == vtinput.VK_F10) {
			s.goBack(menu)
			return true
		}
		// F1..F12: jump to the item whose HotKey is "F<n>" and activate it.
		// Any F-key not bound to a menu item is swallowed so it doesn't
		// fall through to the panel-level handler underneath (F3=View,
		// F5=Copy, etc. would be very surprising while the menu is open).
		if !shift && !ctrl && !alt && e.VirtualKeyCode >= vtinput.VK_F1 && e.VirtualKeyCode <= vtinput.VK_F12 {
			fn := uint32(e.VirtualKeyCode-vtinput.VK_F1) + 1
			target, mapped := fnKeyTarget[fn]
			if !mapped {
				return true
			}
			uiIdx, ok := findMenuItemByUserData(menu, target)
			if !ok {
				return true
			}
			menu.SetSelectPos(uiIdx)
			if target >= 0 && target < len(items) && items[target].IsSubmenu() {
				s.enterSubmenu(menu, items, title, items[target])
				return true
			}
			// Leaf: simulate Enter so vtui's default OnAction fires.
			menu.ProcessKey(&vtinput.InputEvent{
				Type: vtinput.KeyEventType, KeyDown: true,
				VirtualKeyCode: vtinput.VK_RETURN,
			})
			return true
		}
		// Enter / Right on a submenu item: descend into the child.
		if !shift && !ctrl && !alt &&
			(e.VirtualKeyCode == vtinput.VK_RETURN || e.VirtualKeyCode == vtinput.VK_RIGHT) {
			pos := menu.SelectPos
			if pos >= 0 && pos < len(menu.Items) {
				if idx, ok := menu.Items[pos].UserData.(int); ok && idx >= 0 && idx < len(items) {
					if items[idx].IsSubmenu() {
						s.enterSubmenu(menu, items, title, items[idx])
						return true
					}
				}
			}
			// Right on a leaf is a no-op; Enter on a leaf falls through to
			// vtui's default RETURN handler so OnAction runs.
			if e.VirtualKeyCode == vtinput.VK_RIGHT {
				return true
			}
			return false
		}
		// Left arrow → back to the parent menu, no-op at the root.
		if !shift && !ctrl && !alt && e.VirtualKeyCode == vtinput.VK_LEFT {
			s.goBack(menu)
			return true
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
		if chosen.IsSubmenu() {
			// Submenus are handled by OnKeyDown for the keyboard paths.
			// We only land here on mouse click; do the same thing.
			s.enterSubmenu(menu, items, title, chosen)
			return
		}
		// Leaf item: vtui will pop this menu on its own after we return.
		// Drop the history so we don't try to reopen a parent on the way
		// out, then dispatch the commands once the frame stack has settled.
		cmds := chosen.Commands
		s.history = nil
		vtui.FrameManager.PostTask(func() {
			executeMenuCommands(s.pf, cmds)
		})
	}

	vtui.FrameManager.Push(menu)
}

// enterSubmenu records the current level so goBack can recreate it,
// closes the current menu, and pushes the child after the pop settles.
func (s *userMenuState) enterSubmenu(current *vtui.VMenu, parentItems []UserMenuItem, parentTitle string, sub UserMenuItem) {
	s.history = append(s.history, menuLevel{
		items:    parentItems,
		title:    parentTitle,
		selected: current.SelectPos,
	})
	current.Close()
	childTitle := parentTitle + " -> " + stripAmpersand(sub.Label)
	childItems := sub.Submenu
	vtui.FrameManager.PostTask(func() {
		s.pushLevel(childItems, childTitle, 0)
	})
}

// goBack closes the current menu. If there's a saved parent level, it
// is reopened with its prior cursor position; otherwise the user lands
// back on the panels.
func (s *userMenuState) goBack(current *vtui.VMenu) {
	current.Close()
	if len(s.history) == 0 {
		return
	}
	prev := s.history[len(s.history)-1]
	s.history = s.history[:len(s.history)-1]
	vtui.FrameManager.PostTask(func() {
		s.pushLevel(prev.items, prev.title, prev.selected)
	})
}

// editCurrentMenuInExternalEditor implements the Ctrl+F4 flow modelled
// after far2l (usermenu.cpp:619-678): write the current root tree to a
// temp file as FarMenu.ini text, open the editor on the temp file, and
// on close re-parse the file and persist it back to the source path
// using that source's native format. The original source is left
// untouched if the user made no changes or if parsing fails.
func editCurrentMenuInExternalEditor(pf *PanelsFrame, mode MenuMode, sourcePath string) {
	items := loadRootForMode(mode, sourcePath)

	tmp, err := os.CreateTemp("", "f4-usermenu-*.ini")
	if err != nil {
		vtui.ShowMessage(" User menu ", fmt.Sprintf("Cannot create temp file:\n%v", err), []string{"&Ok"})
		return
	}
	tmpPath := tmp.Name()
	if werr := WriteFarMenu(tmp, items); werr != nil {
		tmp.Close()
		os.Remove(tmpPath)
		vtui.ShowMessage(" User menu ", fmt.Sprintf("Cannot write temp file:\n%v", werr), []string{"&Ok"})
		return
	}
	if cerr := tmp.Close(); cerr != nil {
		os.Remove(tmpPath)
		vtui.ShowMessage(" User menu ", fmt.Sprintf("Cannot close temp file:\n%v", cerr), []string{"&Ok"})
		return
	}
	initStat, _ := os.Stat(tmpPath)

	onClose := func() {
		defer os.Remove(tmpPath)
		// Always reopen the menu after the editor closes so the user
		// sees their edits applied immediately (matches far2l: control
		// returns to the menu loop after FrameManager->ExecuteModalEV).
		defer vtui.FrameManager.PostTask(func() { ShowUserMenu(pf) })

		stat, statErr := os.Stat(tmpPath)
		if statErr != nil {
			return
		}
		if initStat != nil && stat.Size() == initStat.Size() && stat.ModTime().Equal(initStat.ModTime()) {
			return
		}
		parsed, parseErr := loadFarMenuFile(tmpPath)
		if parseErr != nil {
			vtui.FrameManager.PostTask(func() {
				vtui.ShowMessage(" User menu ",
					fmt.Sprintf("Failed to parse edited menu:\n%v\n\nOriginal kept.", parseErr),
					[]string{"&Ok"})
			})
			return
		}
		if saveErr := saveRootForMode(mode, sourcePath, parsed); saveErr != nil {
			vtui.FrameManager.PostTask(func() {
				vtui.ShowMessage(" User menu ",
					fmt.Sprintf("Failed to save menu:\n%v", saveErr),
					[]string{"&Ok"})
			})
		}
	}

	openTempInEditor(pf, tmpPath, onClose)
}

// openTempInEditor creates an EditorView on the given path with an
// OnClose hook. It mirrors actionOpenEditor's setup but reads
// synchronously since user-menu temp files are tiny.
func openTempInEditor(pf *PanelsFrame, path string, onClose func()) {
	dir := filepath.Dir(path)
	v := vfs.NewOSVFS(dir)

	data, _ := os.ReadFile(path)
	pt := piecetable.New(data)

	editor := NewEditorView(pt, v, path)
	editor.OnClose = onClose
	editor.ResizeConsole(pf.lastW, pf.lastH)
	editor.StartIndexing()
	vtui.FrameManager.AddScreen(editor)
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
// hasSubmenus reserves space for the right-aligned ► marker.
func menuSize(pf *PanelsFrame, itemCount int, items []UserMenuItem, hasSubmenus bool) (int, int) {
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
	if hasSubmenus {
		w += 2 // reserve room for the trailing "► "
	}
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
