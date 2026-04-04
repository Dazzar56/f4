package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

// fileEntry implements vtui.TableRow for display in a table.
type fileEntry struct {
	vfs.VFSItem
	Selected bool
}
type mediumRow struct {
	fp *FileSystemPanel
	r  int
}

func (m *mediumRow) GetCellText(col int) string {
	H := m.fp.table.ViewHeight
	if H <= 0 { H = 1 }
	idx := m.r
	if col == 1 {
		idx += H
	}
	if idx >= len(m.fp.entries) {
		return ""
	}
	e := m.fp.entries[idx]
	if e.IsDir {
		if e.Name == ".." { return ".." }
		return "/" + e.Name
	}
	return e.Name
}

func (m *mediumRow) IsColSelected(col int) bool {
	H := m.fp.table.ViewHeight
	if H <= 0 { H = 1 }
	idx := m.r
	if col == 1 {
		idx += H
	}
	if idx >= len(m.fp.entries) {
		return false
	}
	return m.fp.entries[idx].Selected
}

type ViewMode int
const (
	ViewModeMedium ViewMode = iota
	ViewModeDetailed
)

func (f *fileEntry) IsSelected() bool {
	return f.Selected
}

func (f *fileEntry) GetCellText(col int) string {
	switch col {
	case 0:
		if f.IsDir {
			if f.Name == ".." { return ".." }
			return "/" + f.Name
		}
		return f.Name
	case 1:
		if f.IsDir {
			if f.Name == ".." {
				return Msg("Panel.UpDir")
			}
			return ""
		}
		return fmt.Sprintf("%d", f.Size)
	}
	return ""
}

// FileSystemPanel is a panel displaying files on disk.
type FileSystemPanel struct {
	vtui.ScreenObject
	table     *vtui.Table
	frame     *vtui.BorderedFrame
	vfs       vfs.VFS
	entries   []*fileEntry
	viewMode            ViewMode
	cursorIdx           int
	lastRightClickedIdx int

	loadCtx          context.Context
	cancelLoad       context.CancelFunc
	isLoading        bool
	loadingTimer     *time.Timer
	pendingSelection string
	fastFindMode bool
	fastFindStr  string
}

func NewFileSystemPanel(x, y, w, h int, vfs vfs.VFS) *FileSystemPanel {
	path := vfs.GetPath()

	fp := &FileSystemPanel{
		vfs:                 vfs,
		frame:               vtui.NewBorderedFrame(x, y, x+w-1, y+h-1, vtui.SingleBox, path),
		table:               vtui.NewTable(x+1, y+1, w-2, h-2, nil),
		viewMode:            ViewModeMedium,
		lastRightClickedIdx: -1,
		//entries:             []*fileEntry{{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}}},
	}
	fp.frame.ColorBoxIdx = ColPanelBox
	fp.frame.ColorTitleIdx = ColPanelTitle
	fp.table.ColorTextIdx = ColPanelText
	fp.table.ColorSelectedTextIdx = ColPanelCursor
	fp.table.ColorItemSelectTextIdx = ColPanelSelectedText
	fp.table.ColorItemSelectCursorIdx = ColPanelSelectedCursor
	fp.table.ColorTitleIdx = ColPanelColumnTitle
	fp.table.ColorBoxIdx = ColPanelBox
	fp.table.ShowScrollBar = false
	fp.SetCanFocus(true)
	fp.SetPosition(x, y, x+w-1, y+h-1)
	fp.SetViewMode(ViewModeMedium)
	fp.ReadDirectory()
	return fp
}

func (fp *FileSystemPanel) SetViewMode(mode ViewMode) {
	fp.viewMode = mode
	if mode == ViewModeMedium {
		fp.table.CellSelection = true
	} else {
		fp.table.CellSelection = false
		fp.table.SelectCol = 0
	}
	fp.Resize(fp.X2-fp.X1+1, fp.Y2-fp.Y1+1)
}

func (fp *FileSystemPanel) GetCursorIndex() int {
	if fp.cursorIdx >= len(fp.entries) {
		fp.cursorIdx = len(fp.entries) - 1
	}
	if fp.cursorIdx < 0 {
		fp.cursorIdx = 0
	}
	return fp.cursorIdx
}

func (fp *FileSystemPanel) SetCursorIndex(idx int) {
	if len(fp.entries) == 0 {
		fp.cursorIdx = 0
		return
	}
	if idx < 0 { idx = 0 }
	if idx >= len(fp.entries) { idx = len(fp.entries) - 1 }
	fp.cursorIdx = idx

	// Sync table visual state
	if fp.viewMode == ViewModeDetailed {
		fp.table.SetSelectPos(fp.cursorIdx)
		fp.table.SelectCol = 0
	} else {
		H := fp.table.ViewHeight
		if H <= 0 { H = 1 }

		// In Medium mode, table.SelectPos is the ROW (0..H-1)
		// and table.SelectCol is the COLUMN (0..1)
		// Absolute index = table.TopPos + row + col*H

		// 1. Ensure TopPos is sane for the current cursor
		if fp.cursorIdx < fp.table.TopPos {
			fp.table.TopPos = fp.cursorIdx
		} else if fp.cursorIdx >= fp.table.TopPos + 2*H {
			fp.table.TopPos = fp.cursorIdx - 2*H + 1
		}

		// Far-style 2-column scrolling: ensure cursorIdx is in [TopPos, TopPos + 2*H)
		if fp.cursorIdx < fp.table.TopPos {
			fp.table.TopPos = fp.cursorIdx
		} else if fp.cursorIdx >= fp.table.TopPos+2*H {
			fp.table.TopPos = fp.cursorIdx - 2*H + 1
		}

		if fp.table.TopPos < 0 {
			fp.table.TopPos = 0
		}

		rel := fp.cursorIdx - fp.table.TopPos
		fp.table.SelectCol = rel / H
		// Table internal rendering expects SelectPos to be absolute index in its row space
		// to correctly calculate vertical offset: y = Y1 + (SelectPos - TopPos)
		fp.table.SelectPos = fp.table.TopPos + (rel % H)

		// If we landed on a column that is theoretically correct but visually empty,
		// the table will handle it during Show, but we keep the absolute index.
	}
}

func (fp *FileSystemPanel) updateTitle(err error) {
	title := fp.vfs.GetPath()
	if err != nil && err != context.Canceled {
		title += " [Error]"
	} else if fp.isLoading {
		title += " [Loading...]"
	}
	fp.frame.SetTitle(title)
}

func (fp *FileSystemPanel) ReadDirectory() {
	if fp.cancelLoad != nil {
		fp.cancelLoad()
		fp.cancelLoad = nil
	}
	if fp.loadingTimer != nil {
		fp.loadingTimer.Stop()
		fp.loadingTimer = nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	fp.loadCtx = ctx
	fp.cancelLoad = cancel
	fp.isLoading = true

	// Delay the "Loading..." indicator to avoid flickering on fast operations
	fp.loadingTimer = time.AfterFunc(300*time.Millisecond, func() {
		vtui.FrameManager.PostTask(func() {
			if fp.isLoading {
				fp.updateTitle(nil)
				vtui.FrameManager.Redraw()
			}
		})
	})

	// Запоминаем выделение, чтобы восстановить его, когда прилетят новые файлы
	if fp.pendingSelection == "" {
		oldName := fp.GetSelectedName()
		if oldName != "" && oldName != ".." {
			fp.pendingSelection = oldName
		}
	}

	path := fp.vfs.GetPath()

	go func() {
		firstChunk := true
		err := fp.vfs.ReadDir(ctx, path, func(chunk []vfs.VFSItem) {
			if ctx.Err() != nil { return }

			newEntries := make([]*fileEntry, len(chunk))
			for i, item := range chunk {
				newEntries[i] = &fileEntry{VFSItem: item}
			}

			vtui.FrameManager.PostTask(func() {
				if ctx.Err() != nil { return }

				// Запоминаем, на каком файле стоял пользователь ПРЯМО СЕЙЧАС
				currentSelected := fp.GetSelectedName()

				if firstChunk {
					fp.entries = []*fileEntry{{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}}}
					firstChunk = false
				}

				fp.entries = append(fp.entries, newEntries...)
				sort.Slice(fp.entries, func(i, j int) bool {
					if fp.entries[i].Name == ".." { return true }
					if fp.entries[j].Name == ".." { return false }
					if fp.entries[i].IsDir != fp.entries[j].IsDir { return fp.entries[i].IsDir }
					return fp.entries[i].Name < fp.entries[j].Name
				})

				// Try to snap focus as soon as the target item appears in the stream
				snapped := false
				if fp.pendingSelection != "" {
					for i, entry := range fp.entries {
						if entry.Name == fp.pendingSelection {
							fp.SetCursorIndex(i)
							fp.pendingSelection = "" // Target found and focused
							snapped = true
							break
						}
					}
				}

				// If we have an active selection from the user, keep it
				if !snapped && currentSelected != "" && currentSelected != ".." {
					for i, entry := range fp.entries {
						if entry.Name == currentSelected {
							fp.SetCursorIndex(i)
							snapped = true
							break
						}
					}
				}

				// If we couldn't snap to anything specific yet, and it's the first time
				// we see data, or the current index is out of bounds, default to 0.
				if !snapped && (fp.cursorIdx >= len(fp.entries) || fp.cursorIdx < 0) {
					fp.SetCursorIndex(0)
				}

				fp.Refresh()
			})
		})

		vtui.FrameManager.PostTask(func() {
			if ctx.Err() != nil {
				return
			}
			if fp.loadingTimer != nil {
				fp.loadingTimer.Stop()
				fp.loadingTimer = nil
			}

			if firstChunk {
				fp.entries = []*fileEntry{{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}}}
				fp.SetCursorIndex(0)
			}
			if fp.pendingSelection != "" {
				fp.SelectName(fp.pendingSelection)
				fp.pendingSelection = ""
			}

			fp.isLoading = false
			fp.updateTitle(err)
			fp.Refresh()
		})
	}()
}

func (fp *FileSystemPanel) Refresh() {
	idx := fp.GetCursorIndex()
	if fp.viewMode == ViewModeDetailed {
		rows := make([]vtui.TableRow, len(fp.entries))
		for i, e := range fp.entries {
			rows[i] = e
		}
		fp.table.SetRows(rows)
	} else {
		rows := make([]vtui.TableRow, len(fp.entries))
		for i := 0; i < len(rows); i++ {
			rows[i] = &mediumRow{fp: fp, r: i}
		}
		fp.table.SetRows(rows)
	}
	fp.SetCursorIndex(idx)
}

func (fp *FileSystemPanel) Show(scr *vtui.ScreenBuf) {
	fp.frame.Show(scr)
	fp.table.SetFocus(fp.IsFocused())
	fp.table.Show(scr)
	if fp.fastFindMode {
		boxW := 24
		boxH := 3

		fx1 := fp.X1 + 9
		if fx1+boxW-1 >= scr.Width() {
			fx1 = scr.Width() - boxW
		}
		if fx1 < 0 {
			fx1 = 0
		}
		fx2 := fx1 + boxW - 1

		fy1 := fp.Y2 - 2
		if fy1 < 0 {
			fy1 = 0
		}
		fy2 := fy1 + boxH - 1

		p := vtui.NewPainter(scr)

		p.Fill(fx1, fy1, fx2, fy2, ' ', vtui.Palette[vtui.ColDialogText])
		p.DrawBox(fx1, fy1, fx2, fy2, vtui.Palette[vtui.ColDialogBox], vtui.DoubleBox)
		p.DrawTitle(fx1, fy1, fx2, Msg("Viewer.SearchTitle"), vtui.Palette[vtui.ColDialogBoxTitle])

		searchStr := fp.fastFindStr
		for runewidth.StringWidth(searchStr) > boxW-4 {
			runes := []rune(searchStr)
			searchStr = string(runes[1:])
		}

		p.DrawString(fx1+2, fy1+1, searchStr, vtui.Palette[vtui.ColDialogText])

		scr.SetCursorPos(fx1+2+runewidth.StringWidth(searchStr), fy1+1)
		scr.SetCursorVisible(true)
	}
}

func (fp *FileSystemPanel) SetPosition(x1, y1, x2, y2 int) {
	fp.ScreenObject.SetPosition(x1, y1, x2, y2)
	fp.frame.SetPosition(x1, y1, x2, y2)
	// Table stays inside the frame
	fp.table.SetPosition(x1+1, y1+1, x2-1, y2-1)
}

func (fp *FileSystemPanel) Resize(w, h int) {
	fp.SetPosition(fp.X1, fp.Y1, fp.X1+w-1, fp.Y1+h-1)

	if fp.viewMode == ViewModeDetailed {
		nameW := w - 15 - 2
		if nameW < 5 { nameW = 5 }
		fp.table.Columns = []vtui.TableColumn{
			{Title: Msg("Panel.Column.Name"), Width: nameW},
			{Title: Msg("Panel.Column.Size"), Width: 12, Alignment: vtui.AlignRight},
		}
	} else {
		colW := (w - 2 - 1) / 2 // 2 borders, 1 separator
		if colW < 5 { colW = 5 }
		fp.table.Columns = []vtui.TableColumn{
			{Title: Msg("Panel.Column.Name"), Width: colW},
			{Title: Msg("Panel.Column.Name"), Width: w - 2 - colW - 1},
		}
	}
	fp.Refresh()
}

func (fp *FileSystemPanel) ProcessKey(e *vtinput.InputEvent) bool {
	if !e.KeyDown {
		return false
	}

	shift := (e.ControlKeyState & vtinput.ShiftPressed) != 0

	alt := (e.ControlKeyState & (vtinput.LeftAltPressed | vtinput.RightAltPressed)) != 0
	ctrl := (e.ControlKeyState & (vtinput.LeftCtrlPressed | vtinput.RightCtrlPressed)) != 0

	if fp.fastFindMode {
		if e.VirtualKeyCode == vtinput.VK_ESCAPE {
			fp.fastFindMode = false
			fp.fastFindStr = ""
			vtui.FrameManager.Redraw()
			return true
		}
		if e.VirtualKeyCode == vtinput.VK_BACK {
			if len(fp.fastFindStr) > 0 {
				runes := []rune(fp.fastFindStr)
				fp.fastFindStr = string(runes[:len(runes)-1])
				if len(fp.fastFindStr) == 0 {
					fp.fastFindMode = false
				} else {
					fp.doFastFind(0)
				}
			}
			vtui.FrameManager.Redraw()
			return true
		}
		if e.VirtualKeyCode == vtinput.VK_UP {
			fp.doFastFind(-1)
			vtui.FrameManager.Redraw()
			return true
		}
		if e.VirtualKeyCode == vtinput.VK_DOWN {
			fp.doFastFind(1)
			vtui.FrameManager.Redraw()
			return true
		}
		if e.VirtualKeyCode == vtinput.VK_RETURN {
			fp.fastFindMode = false
			fp.fastFindStr = ""
			vtui.FrameManager.Redraw()
			// Проваливаемся ниже, чтобы обработать Enter как вход в файл/директорию
		} else if e.Char != 0 && !ctrl {
			fp.fastFindStr += string(e.Char)
			fp.doFastFind(0)
			vtui.FrameManager.Redraw()
			return true
		}
	} else {
		if e.Char != 0 && alt && !ctrl && unicode.IsPrint(e.Char) {
			fp.fastFindMode = true
			fp.fastFindStr = string(e.Char)
			fp.doFastFind(0)
			vtui.FrameManager.Redraw()
			return true
		}
	}


	switch e.VirtualKeyCode {
	case vtinput.VK_INSERT:
		idx := fp.GetCursorIndex()
		if idx < len(fp.entries) && fp.entries[idx].Name != ".." {
			fp.entries[idx].Selected = !fp.entries[idx].Selected
		}
		fp.SetCursorIndex(idx + 1)
		return true

	case vtinput.VK_UP, vtinput.VK_DOWN, vtinput.VK_LEFT, vtinput.VK_RIGHT, vtinput.VK_PRIOR, vtinput.VK_NEXT, vtinput.VK_HOME, vtinput.VK_END:
		if shift {
			idx := fp.GetCursorIndex()
			if idx < len(fp.entries) && fp.entries[idx].Name != ".." {
				fp.entries[idx].Selected = !fp.entries[idx].Selected
			}
		}

		idx := fp.GetCursorIndex()
		H := fp.table.ViewHeight
		if H <= 0 { H = 1 }

		if fp.viewMode == ViewModeMedium {
			switch e.VirtualKeyCode {
			case vtinput.VK_UP: idx--
			case vtinput.VK_DOWN: idx++
			case vtinput.VK_LEFT: idx -= H
			case vtinput.VK_RIGHT: idx += H
			case vtinput.VK_PRIOR: idx -= H * 2
			case vtinput.VK_NEXT: idx += H * 2
			case vtinput.VK_HOME: idx = 0
			case vtinput.VK_END: idx = len(fp.entries) - 1
			default: return false
			}
			fp.SetCursorIndex(idx)
			return true
		} else {
			// In Detailed mode, we let the table handle navigation but sync our index back
			handled := fp.table.ProcessKey(e)
			if handled {
				fp.cursorIdx = fp.table.SelectPos
			}
			return handled
		}

	case vtinput.VK_RETURN:
		idx := fp.GetCursorIndex()
		if idx >= 0 && idx < len(fp.entries) {
			selected := fp.entries[idx]
			if selected.IsDir {
				oldPath := fp.vfs.GetPath()
				newPath := fp.vfs.Join(oldPath, selected.Name)
				vtui.DebugLog("PANEL: Navigating %q -> %q", oldPath, newPath)
				if err := fp.vfs.SetPath(newPath); err == nil {
					if selected.Name == ".." {
						fp.pendingSelection = fp.vfs.Base(oldPath)
					} else {
						// When entering a folder, always reset cursor to ".."
						fp.pendingSelection = ".."
					}
					fp.ReadDirectory()
					return true
				} else {
					vtui.DebugLog("PANEL: Navigation failed: %v", err)
				}
			}
		}
	}

	return false
}

func (fp *FileSystemPanel) ProcessMouse(e *vtinput.InputEvent) bool {
	if e.Type != vtinput.MouseEventType {
		return false
	}

	if e.ButtonState == 0 {
		fp.lastRightClickedIdx = -1
	}

	if fp.fastFindMode && e.ButtonState != 0 {
		fp.fastFindMode = false
		vtui.FrameManager.Redraw()
	}

	handled := fp.table.ProcessMouse(e)
	if handled {
		// Sync absolute index from table's visual selection
		if fp.viewMode == ViewModeDetailed {
			fp.cursorIdx = fp.table.SelectPos
		} else {
			H := fp.table.ViewHeight
			if H <= 0 { H = 1 }
			newIdx := fp.table.TopPos + fp.table.SelectPos + fp.table.SelectCol*H

			// Fix for "click in empty space": if we selected an empty slot,
			// snap to the last valid entry.
			if newIdx >= len(fp.entries) {
				fp.SetCursorIndex(len(fp.entries) - 1)
			} else {
				fp.cursorIdx = newIdx
			}
		}
	}

	if e.ButtonState != 0 && e.KeyDown {
		idx := fp.GetCursorIndex()
		if idx < len(fp.entries) {
			if e.ButtonState == vtinput.RightmostButtonPressed {
				if fp.entries[idx].Name != ".." && fp.lastRightClickedIdx != idx {
					fp.entries[idx].Selected = !fp.entries[idx].Selected
					fp.lastRightClickedIdx = idx
				}
				return true
			}
		}
	}

	return handled
}

func (fp *FileSystemPanel) GetSelectedName() string {
	idx := fp.GetCursorIndex()
	if len(fp.entries) == 0 || idx < 0 || idx >= len(fp.entries) {
		return ""
	}
	entry := fp.entries[idx]
	if entry.Name == ".." {
		return fp.vfs.Dir(fp.vfs.GetPath())
	}
	return entry.Name
}

// SelectName searches for an entry by name and moves the cursor to it.
func (fp *FileSystemPanel) SelectName(name string) {
	for i, entry := range fp.entries {
		if entry.Name == name {
			fp.SetCursorIndex(i)
			fp.Refresh()
			break
		}
	}
}

// GetSelectedNames returns a list of selected files. If none are selected, returns the focused one.
func (fp *FileSystemPanel) GetSelectedNames() []string {
	var names []string
	for _, e := range fp.entries {
		if e.Selected && e.Name != ".." {
			names = append(names, e.Name)
		}
	}
	if len(names) == 0 {
		name := fp.GetSelectedName()
		if name != "" && name != ".." {
			names = append(names, name)
		}
	}
	return names
}
// GetSuccessorName determines which file should receive focus after the current
// selection (or focused item) is deleted or moved.
func (fp *FileSystemPanel) doFastFind(dir int) {
	if fp.fastFindStr == "" {
		return
	}
	searchLower := strings.ToLower(fp.fastFindStr)
	startIdx := fp.GetCursorIndex()

	if dir == 0 {
		for i := 0; i < len(fp.entries); i++ {
			if strings.HasPrefix(strings.ToLower(fp.entries[i].Name), searchLower) {
				fp.SetCursorIndex(i)
				fp.Refresh()
				return
			}
		}
	} else if dir == 1 {
		for i := startIdx + 1; i < len(fp.entries); i++ {
			if strings.HasPrefix(strings.ToLower(fp.entries[i].Name), searchLower) {
				fp.SetCursorIndex(i)
				fp.Refresh()
				return
			}
		}
		for i := 0; i <= startIdx; i++ {
			if strings.HasPrefix(strings.ToLower(fp.entries[i].Name), searchLower) {
				fp.SetCursorIndex(i)
				fp.Refresh()
				return
			}
		}
	} else if dir == -1 {
		for i := startIdx - 1; i >= 0; i-- {
			if strings.HasPrefix(strings.ToLower(fp.entries[i].Name), searchLower) {
				fp.SetCursorIndex(i)
				fp.Refresh()
				return
			}
		}
		for i := len(fp.entries) - 1; i >= startIdx; i-- {
			if strings.HasPrefix(strings.ToLower(fp.entries[i].Name), searchLower) {
				fp.SetCursorIndex(i)
				fp.Refresh()
				return
			}
		}
	}
}
func (fp *FileSystemPanel) GetSuccessorName() string {
	if len(fp.entries) <= 1 {
		return ".."
	}

	anySelected := false
	for _, e := range fp.entries {
		if e.Selected && e.Name != ".." {
			anySelected = true
			break
		}
	}

	var firstIdx, lastIdx int

	if anySelected {
		// If something is selected, we only care about the selection range
		firstIdx = len(fp.entries)
		lastIdx = -1
		for i, e := range fp.entries {
			if e.Selected && e.Name != ".." {
				if i < firstIdx { firstIdx = i }
				if i > lastIdx { lastIdx = i }
			}
		}
	} else {
		// If nothing selected, the "range" is just the current cursor
		firstIdx = fp.cursorIdx
		lastIdx = fp.cursorIdx
	}

	// Helper to check if an item at index i is about to be removed
	isToBeRemoved := func(i int) bool {
		if anySelected {
			return fp.entries[i].Selected && fp.entries[i].Name != ".."
		}
		return i == fp.cursorIdx
	}

	// 1. Try to find the first valid item AFTER the removed block
	for i := lastIdx + 1; i < len(fp.entries); i++ {
		if !isToBeRemoved(i) {
			return fp.entries[i].Name
		}
	}

	// 2. If no "next" item, try to find the first valid item BEFORE the removed block
	for i := firstIdx - 1; i >= 0; i-- {
		if !isToBeRemoved(i) && fp.entries[i].Name != ".." {
			return fp.entries[i].Name
		}
	}

	// 3. Fallback to parent directory entry
	return ".."
}
