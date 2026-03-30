package main

import (
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
type multiFileRow struct {
	entries []*fileEntry
}

func (m *multiFileRow) GetCellText(col int) string {
	if col >= len(m.entries) { return "" }
	e := m.entries[col]
	if e.IsDir {
		if e.Name == ".." { return ".." }
		return "/" + e.Name
	}
	return e.Name
}

func (m *multiFileRow) IsColSelected(col int) bool {
	if col >= len(m.entries) { return false }
	return m.entries[col].Selected
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
	viewMode  ViewMode
	cursorIdx int
	topIdx    int
}

func NewFileSystemPanel(x, y, w, h int, vfs vfs.VFS) *FileSystemPanel {
	path := vfs.GetPath()

	fp := &FileSystemPanel{
		vfs:      vfs,
		frame:    vtui.NewBorderedFrame(x, y, x+w-1, y+h-1, vtui.SingleBox, path),
		table:    vtui.NewTable(x+1, y+1, w-2, h-2, nil),
		viewMode: ViewModeMedium,
	}
	fp.frame.ColorBoxIdx = ColPanelBox
	fp.frame.ColorTitleIdx = ColPanelTitle
	fp.table.ColorTextIdx = ColPanelText
	fp.table.ColorSelectedTextIdx = ColPanelCursor
	fp.table.ColorItemSelectTextIdx = ColPanelSelectedText
	fp.table.ColorItemSelectCursorIdx = ColPanelSelectedCursor
	fp.table.ColorTitleIdx = ColPanelColumnTitle
	fp.table.ColorBoxIdx = ColPanelBox
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

func (fp *FileSystemPanel) getVisualHeight() int {
	h := fp.table.Y2 - fp.table.Y1 + 1
	if fp.table.ShowHeader {
		h--
	}
	if h < 1 {
		return 1
	}
	return h
}

func (fp *FileSystemPanel) GetCursorIndex() int {
	return fp.cursorIdx
}

func (fp *FileSystemPanel) SetCursorIndex(idx int) {
	if idx < 0 {
		idx = 0
	}
	if idx >= len(fp.entries) {
		idx = len(fp.entries) - 1
	}
	fp.cursorIdx = idx
	fp.ensureCursorVisible()
}

func (fp *FileSystemPanel) ensureCursorVisible() {
	if len(fp.entries) == 0 {
		return
	}
	height := fp.getVisualHeight()

	if fp.viewMode == ViewModeDetailed {
		if fp.cursorIdx < fp.topIdx {
			fp.topIdx = fp.cursorIdx
		} else if fp.cursorIdx >= fp.topIdx+height {
			fp.topIdx = fp.cursorIdx - height + 1
		}
		fp.table.SelectPos = fp.cursorIdx - fp.topIdx
		fp.table.SelectCol = 0
	} else {
		// Medium mode: scroll by columns
		if fp.cursorIdx < fp.topIdx {
			for fp.cursorIdx < fp.topIdx {
				fp.topIdx -= height
			}
		} else if fp.cursorIdx >= fp.topIdx+2*height {
			for fp.cursorIdx >= fp.topIdx+2*height {
				fp.topIdx += height
			}
		}
		if fp.topIdx < 0 {
			fp.topIdx = 0
		}
		relIdx := fp.cursorIdx - fp.topIdx
		fp.table.SelectPos = relIdx % height
		fp.table.SelectCol = relIdx / height
	}
	fp.table.TopPos = 0
}

func (fp *FileSystemPanel) ReadDirectory() {
	path := fp.vfs.GetPath()
	fp.frame.SetTitle(path)
	items, err := fp.vfs.ReadDir(path)
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
	height := fp.getVisualHeight()
	fp.ensureCursorVisible()

	if fp.viewMode == ViewModeDetailed {
		displayCount := height
		if displayCount > len(fp.entries)-fp.topIdx {
			displayCount = len(fp.entries) - fp.topIdx
		}
		if displayCount < 0 { displayCount = 0 }

		rows := make([]vtui.TableRow, displayCount)
		for i := 0; i < displayCount; i++ {
			rows[i] = fp.entries[fp.topIdx+i]
		}
		fp.table.SetRows(rows)
	} else {
		rows := make([]vtui.TableRow, height)
		for i := 0; i < height; i++ {
			mRow := &multiFileRow{entries: make([]*fileEntry, 0, 2)}
			if fp.topIdx+i < len(fp.entries) {
				mRow.entries = append(mRow.entries, fp.entries[fp.topIdx+i])
			}
			if fp.topIdx+height+i < len(fp.entries) {
				mRow.entries = append(mRow.entries, fp.entries[fp.topIdx+height+i])
			}
			rows[i] = mRow
		}
		fp.table.SetRows(rows)
	}
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

	height := fp.getVisualHeight()
	shift := (e.ControlKeyState & vtinput.ShiftPressed) != 0

	switch e.VirtualKeyCode {
	case vtinput.VK_INSERT:
		if fp.cursorIdx >= 0 && fp.cursorIdx < len(fp.entries) {
			if fp.entries[fp.cursorIdx].Name != ".." {
				fp.entries[fp.cursorIdx].Selected = !fp.entries[fp.cursorIdx].Selected
			}
			fp.SetCursorIndex(fp.cursorIdx + 1)
			fp.Refresh()
		}
		return true

	case vtinput.VK_UP:
		if shift && fp.cursorIdx >= 0 && fp.cursorIdx < len(fp.entries) && fp.entries[fp.cursorIdx].Name != ".." {
			fp.entries[fp.cursorIdx].Selected = !fp.entries[fp.cursorIdx].Selected
		}
		fp.SetCursorIndex(fp.cursorIdx - 1)
		fp.Refresh()
		return true

	case vtinput.VK_DOWN:
		if shift && fp.cursorIdx >= 0 && fp.cursorIdx < len(fp.entries) && fp.entries[fp.cursorIdx].Name != ".." {
			fp.entries[fp.cursorIdx].Selected = !fp.entries[fp.cursorIdx].Selected
		}
		fp.SetCursorIndex(fp.cursorIdx + 1)
		fp.Refresh()
		return true

	case vtinput.VK_LEFT:
		if fp.viewMode == ViewModeMedium {
			if shift && fp.cursorIdx >= 0 && fp.cursorIdx < len(fp.entries) && fp.entries[fp.cursorIdx].Name != ".." {
				fp.entries[fp.cursorIdx].Selected = !fp.entries[fp.cursorIdx].Selected
			}
			fp.SetCursorIndex(fp.cursorIdx - height)
			fp.Refresh()
			return true
		}

	case vtinput.VK_RIGHT:
		if fp.viewMode == ViewModeMedium {
			if shift && fp.cursorIdx >= 0 && fp.cursorIdx < len(fp.entries) && fp.entries[fp.cursorIdx].Name != ".." {
				fp.entries[fp.cursorIdx].Selected = !fp.entries[fp.cursorIdx].Selected
			}
			fp.SetCursorIndex(fp.cursorIdx + height)
			fp.Refresh()
			return true
		}

	case vtinput.VK_PRIOR: // PgUp
		fp.SetCursorIndex(fp.cursorIdx - height)
		if fp.viewMode == ViewModeMedium {
			fp.SetCursorIndex(fp.cursorIdx - height) // PgUp in Medium moves 2 columns
		}
		fp.Refresh()
		return true

	case vtinput.VK_NEXT: // PgDn
		fp.SetCursorIndex(fp.cursorIdx + height)
		if fp.viewMode == ViewModeMedium {
			fp.SetCursorIndex(fp.cursorIdx + height)
		}
		fp.Refresh()
		return true

	case vtinput.VK_HOME:
		fp.SetCursorIndex(0)
		fp.Refresh()
		return true

	case vtinput.VK_END:
		fp.SetCursorIndex(len(fp.entries) - 1)
		fp.Refresh()
		return true

	case vtinput.VK_RETURN:
		if fp.cursorIdx >= 0 && fp.cursorIdx < len(fp.entries) {
			selected := fp.entries[fp.cursorIdx]
			if selected.IsDir {
				oldPath := fp.vfs.GetPath()
				newPath := fp.vfs.Join(oldPath, selected.Name)
				if err := fp.vfs.SetPath(newPath); err == nil {
					fp.topIdx = 0
					fp.cursorIdx = 0
					fp.ReadDirectory()
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

	// 1. Handle Wheel
	if e.WheelDirection != 0 {
		height := fp.getVisualHeight()
		step := 1
		if fp.viewMode == ViewModeMedium { step = height }
		if e.WheelDirection > 0 {
			fp.SetCursorIndex(fp.cursorIdx - step)
		} else {
			fp.SetCursorIndex(fp.cursorIdx + step)
		}
		fp.Refresh()
		return true
	}

	// 2. Handle Clicks
	if e.ButtonState != 0 {
		mx, my := int(e.MouseX), int(e.MouseY)
		tx1, ty1, tx2, ty2 := fp.table.GetPosition()

		headerOffset := 0
		if fp.table.ShowHeader { headerOffset = 1 }

		if my >= ty1+headerOffset && my <= ty2 && mx >= tx1 && mx <= tx2 {
			row := my - (ty1 + headerOffset)
			col := 0

			if fp.viewMode == ViewModeMedium {
				colWidth := (tx2 - tx1 + 1 - 1) / 2
				if mx >= tx1+colWidth+1 { col = 1 }
			}

			height := fp.getVisualHeight()
			var clickedIdx int
			if fp.viewMode == ViewModeDetailed {
				clickedIdx = fp.topIdx + row
			} else {
				clickedIdx = fp.topIdx + (col * height) + row
			}

			if clickedIdx >= 0 && clickedIdx < len(fp.entries) {
				vtui.DebugLog("MOUSE: Panel click at (%d,%d) index:%d entry:%s mode:%v", mx, my, clickedIdx, fp.entries[clickedIdx].Name, fp.viewMode)
				fp.SetCursorIndex(clickedIdx)

				if e.ButtonState == vtinput.RightmostButtonPressed && e.KeyDown {
					if fp.entries[clickedIdx].Name != ".." {
						fp.entries[clickedIdx].Selected = !fp.entries[clickedIdx].Selected
					}
				}
				fp.Refresh()
				return true
			}
		} else {
			vtui.DebugLog("MOUSE: Panel click OUTSIDE data area. Table: (%d,%d)-(%d,%d) Mouse: (%d,%d)", tx1, ty1, tx2, ty2, mx, my)
		}
	}

	return false
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
