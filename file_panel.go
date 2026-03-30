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
	table    *vtui.Table
	frame    *vtui.BorderedFrame
	vfs      vfs.VFS
	entries  []*fileEntry
	viewMode ViewMode
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
	fp.Refresh()
}

func (fp *FileSystemPanel) GetCursorIndex() int {
	if fp.viewMode == ViewModeMedium {
		idx := fp.table.SelectPos*2 + fp.table.SelectCol
		if idx >= len(fp.entries) {
			return len(fp.entries) - 1
		}
		return idx
	}
	return fp.table.SelectPos
}

func (fp *FileSystemPanel) SetCursorIndex(idx int) {
	if fp.viewMode == ViewModeMedium {
		fp.table.SelectPos = idx / 2
		fp.table.SelectCol = idx % 2
	} else {
		fp.table.SelectPos = idx
	}
	fp.table.EnsureVisible()
}

func (fp *FileSystemPanel) Refresh() {
	path := fp.vfs.GetPath()
	fp.frame.SetTitle(path)
	items, err := fp.vfs.ReadDir(path)
	if err != nil {
		return
	}

	fp.entries = make([]*fileEntry, 0, len(items)+1)

	// Add ".." to go up
	fp.entries = append(fp.entries, &fileEntry{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}})

	for _, item := range items {
		fp.entries = append(fp.entries, &fileEntry{VFSItem: item})
	}

	// Sort: directories first, then files
	sort.Slice(fp.entries, func(i, j int) bool {
		if fp.entries[i].IsDir != fp.entries[j].IsDir {
			return fp.entries[i].IsDir
		}
		return fp.entries[i].Name < fp.entries[j].Name
	})

	if fp.viewMode == ViewModeDetailed {
		rows := make([]vtui.TableRow, len(fp.entries))
		for i, e := range fp.entries {
			rows[i] = e
		}
		fp.table.SetRows(rows)
	} else {
		rowCount := (len(fp.entries) + 1) / 2
		rows := make([]vtui.TableRow, rowCount)
		for i := 0; i < rowCount; i++ {
			mRow := &multiFileRow{
				entries: make([]*fileEntry, 0, 2),
			}
			mRow.entries = append(mRow.entries, fp.entries[i*2])
			if i*2+1 < len(fp.entries) {
				mRow.entries = append(mRow.entries, fp.entries[i*2+1])
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
}

func (fp *FileSystemPanel) ProcessKey(e *vtinput.InputEvent) bool {
	if !e.KeyDown { return false }

	shift := (e.ControlKeyState & vtinput.ShiftPressed) != 0

	// Handle Insert
	if e.VirtualKeyCode == vtinput.VK_INSERT {
		idx := fp.GetCursorIndex()
		if len(fp.entries) > 0 && idx >= 0 && idx < len(fp.entries) {
			if fp.entries[idx].Name != ".." {
				fp.entries[idx].Selected = !fp.entries[idx].Selected
			}
			if idx < len(fp.entries)-1 {
				fp.SetCursorIndex(idx + 1)
			}
			return true
		}
	}

	// Handle Shift+Arrows
	if shift && (e.VirtualKeyCode == vtinput.VK_UP || e.VirtualKeyCode == vtinput.VK_DOWN || e.VirtualKeyCode == vtinput.VK_LEFT || e.VirtualKeyCode == vtinput.VK_RIGHT) {
		idx := fp.GetCursorIndex()
		if len(fp.entries) > 0 && idx >= 0 && idx < len(fp.entries) {
			if fp.entries[idx].Name != ".." {
				fp.entries[idx].Selected = !fp.entries[idx].Selected
			}
			handled := fp.table.ProcessKey(e)
			if handled && fp.viewMode == ViewModeMedium {
				if fp.GetCursorIndex() >= len(fp.entries) {
					fp.table.SelectCol--
				}
			}
			return true
		}
	}

	// Handle directory navigation
	if e.VirtualKeyCode == vtinput.VK_RETURN {
		idx := fp.GetCursorIndex()
		if len(fp.entries) == 0 || idx < 0 || idx >= len(fp.entries) {
			return false
		}
		selected := fp.entries[idx]
		if selected.IsDir {
			oldPath := fp.vfs.GetPath()
			newPath := fp.vfs.Join(oldPath, selected.Name)

			if err := fp.vfs.SetPath(newPath); err != nil {
				return false
			}
			fp.Refresh()

			if selected.Name == ".." {
				// We went up. Find the directory we came from.
				dirToSelect := fp.vfs.Base(oldPath)
				for i, entry := range fp.entries {
					if entry.Name == dirToSelect {
						fp.SetCursorIndex(i)
						return true
					}
				}
			}

			fp.SetCursorIndex(0)
			return true
		}
	}

	handled := fp.table.ProcessKey(e)
	if handled && fp.viewMode == ViewModeMedium {
		if fp.GetCursorIndex() >= len(fp.entries) {
			fp.table.SelectCol--
		}
	}
	return handled
}

func (fp *FileSystemPanel) ProcessMouse(e *vtinput.InputEvent) bool {
	return fp.table.ProcessMouse(e)
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
