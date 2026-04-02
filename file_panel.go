package main

import (
	"context"
	"fmt"
	"sort"

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
}

func NewFileSystemPanel(x, y, w, h int, vfs vfs.VFS) *FileSystemPanel {
	path := vfs.GetPath()

	fp := &FileSystemPanel{
		vfs:                 vfs,
		frame:               vtui.NewBorderedFrame(x, y, x+w-1, y+h-1, vtui.SingleBox, path),
		table:               vtui.NewTable(x+1, y+1, w-2, h-2, nil),
		viewMode:            ViewModeMedium,
		lastRightClickedIdx: -1,
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

func (fp *FileSystemPanel) ReadDirectory() {
	path := fp.vfs.GetPath()
	fp.frame.SetTitle(path)
	// TODO: Make this truly asynchronous in Phase 2
	items, err := fp.vfs.ReadDir(context.Background(), path)
	if err != nil {
		return
	}

	fp.entries = make([]*fileEntry, 0, len(items)+1)
	fp.entries = append(fp.entries, &fileEntry{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}})
	for _, item := range items {
		fp.entries = append(fp.entries, &fileEntry{VFSItem: item})
	}

	sort.Slice(fp.entries, func(i, j int) bool {
		if fp.entries[i].Name == ".." { return true }
		if fp.entries[j].Name == ".." { return false }
		if fp.entries[i].IsDir != fp.entries[j].IsDir { return fp.entries[i].IsDir }
		return fp.entries[i].Name < fp.entries[j].Name
	})
	fp.Refresh()
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
				if err := fp.vfs.SetPath(newPath); err == nil {
					fp.ReadDirectory()
					fp.SetCursorIndex(0)
					if selected.Name == ".." {
						dirToSelect := fp.vfs.Base(oldPath)
						fp.SelectName(dirToSelect)
					}
					return true
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
