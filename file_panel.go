package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"
	"path/filepath"

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
		if AppConfig.HighlightDir { return e.Name }
		return string(os.PathSeparator) + e.Name
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
func (m *mediumRow) GetCellAttr(col int, defaultAttr uint64) uint64 {
	H := m.fp.table.ViewHeight
	if H <= 0 { H = 1 }
	idx := m.r
	if col == 1 {
		idx += H
	}
	if idx >= len(m.fp.entries) {
		return defaultAttr
	}
	e := m.fp.entries[idx]
	if AppConfig.HighlightDir && e.IsDir && e.Name != ".." {
		return vtui.Palette[ColPanelDir]
	}
	return defaultAttr
}

type ViewMode int
const (
	ViewModeMedium ViewMode = iota
	ViewModeDetailed
)
type SortMode int

const (
	SortName SortMode = iota
	SortExt
	SortTime
	SortSize
	SortUnsorted
)

func (f *fileEntry) IsSelected() bool {
	return f.Selected
}

func (f *fileEntry) GetCellText(col int) string {
	switch col {
	case 0:
		if f.IsDir {
			if f.Name == ".." { return ".." }
			if AppConfig.HighlightDir { return f.Name }
			return string(os.PathSeparator) + f.Name
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
func (f *fileEntry) GetCellAttr(col int, defaultAttr uint64) uint64 {
	if AppConfig.HighlightDir && f.IsDir && f.Name != ".." {
		return vtui.Palette[ColPanelDir]
	}
	return defaultAttr
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

	sortMode    SortMode
	sortReverse bool
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

func (fp *FileSystemPanel) SetSortMode(mode SortMode) {
	if fp.sortMode == mode {
		fp.sortReverse = !fp.sortReverse
	} else {
		fp.sortMode = mode
		// Far по умолчанию сортирует время и размер по убыванию
		if mode == SortTime || mode == SortSize {
			fp.sortReverse = true
		} else {
			fp.sortReverse = false
		}
	}
	fp.ReadDirectory()
}

func (fp *FileSystemPanel) sortEntries() {
	if fp.sortMode == SortUnsorted || len(fp.entries) <= 1 {
		return
	}

	sort.Slice(fp.entries, func(i, j int) bool {
		ei, ej := fp.entries[i], fp.entries[j]

		// ".." всегда сверху
		if ei.Name == ".." { return true }
		if ej.Name == ".." { return false }

		// Папки всегда сверху
		if ei.IsDir != ej.IsDir {
			return ei.IsDir
		}

		res := false
		switch fp.sortMode {
		case SortName:
			res = strings.ToLower(ei.Name) < strings.ToLower(ej.Name)
		case SortExt:
			extI := strings.ToLower(filepath.Ext(ei.Name))
			extJ := strings.ToLower(filepath.Ext(ej.Name))
			if extI != extJ {
				res = extI < extJ
			} else {
				res = strings.ToLower(ei.Name) < strings.ToLower(ej.Name)
			}
		case SortTime:
			res = ei.MTime.After(ej.MTime)
		case SortSize:
			res = ei.Size > ej.Size
		default:
			res = strings.ToLower(ei.Name) < strings.ToLower(ej.Name)
		}

		if fp.sortReverse {
			return !res
		}
		return res
	})
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
		if fp.fastFindMode {
			H := fp.table.ViewHeight
			if H > 2 && fp.cursorIdx >= fp.table.TopPos + H - 2 {
				fp.table.TopPos = fp.cursorIdx - H + 3
				if fp.table.TopPos < 0 { fp.table.TopPos = 0 }
			}
		}
	} else {
		H := fp.table.ViewHeight
		if H <= 0 { H = 1 }

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

		if fp.fastFindMode && H > 2 {
			rel := fp.cursorIdx - fp.table.TopPos
			row := rel % H
			if row >= H - 2 {
				shift := row - (H - 3)
				fp.table.TopPos += shift
			}
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
	fp.readDirectoryEx(false)
}

func (fp *FileSystemPanel) readDirectoryEx(keepEntries bool) {
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

	// 1. Устанавливаем флаг, но НЕ обновляем UI немедленно, чтобы избежать мерцания
	path := fp.vfs.GetPath()
	isFirstChunk := true

	// 2. Таймер для индикатора "Loading..." (появится через 200мс если VFS тормозит)
	fp.loadingTimer = time.AfterFunc(200*time.Millisecond, func() {
		vtui.FrameManager.PostTask(func() {
			// Если данные всё еще не пришли (isFirstChunk == true), только тогда "портим" UI
			if fp.isLoading && isFirstChunk {
				if !keepEntries {
					fp.entries = nil
					if !fp.vfs.IsAtRoot() || fp.vfs.ParentVFS() != nil {
						fp.entries = []*fileEntry{{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}}}
					}
					fp.SetCursorIndex(0)
					fp.Refresh()
				}
				// Теперь, когда таймаут вышел, показываем [Loading...] в заголовке
				fp.updateTitle(nil)
				vtui.FrameManager.Redraw()
			}
		})
	})

	if fp.pendingSelection == "" {
		oldName := fp.GetSelectedName()
		if oldName != "" && oldName != ".." {
			fp.pendingSelection = oldName
		}
	}

	// Capture currently selected names to restore them after the async reload
	selectedNames := make(map[string]bool)
	for _, e := range fp.entries {
		if e.Selected && e.Name != ".." {
			selectedNames[e.Name] = true
		}
	}

	go func() {
		isFirstChunk := true
		err := fp.vfs.ReadDir(ctx, path, func(chunk []vfs.VFSItem) {
			if ctx.Err() != nil { return }

			newEntries := make([]*fileEntry, 0, len(chunk))
			for _, item := range chunk {
				// Hide hidden files if configured, but never hide '..'
				if !AppConfig.ShowHiddenFiles && item.Name != ".." && item.IsHidden {
					continue
				}
				entry := &fileEntry{VFSItem: item}
				if selectedNames[item.Name] {
					entry.Selected = true
				}
				newEntries = append(newEntries, entry)
			}

			vtui.FrameManager.PostTask(func() {
				if ctx.Err() != nil { return }

                if isFirstChunk {
                    fp.entries = nil
                    if !fp.vfs.IsAtRoot() || fp.vfs.ParentVFS() != nil {
                        fp.entries = []*fileEntry{{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}}}
                    }
                    isFirstChunk = false
                }

				currentSelected := fp.GetSelectedName()
				fp.entries = append(fp.entries, newEntries...)
				fp.sortEntries()

				// Фокусировка на нужном файле
				snapped := false
				target := fp.pendingSelection
				if target == "" { target = currentSelected }

				if target != "" {
					for i, entry := range fp.entries {
						if entry.Name == target {
							fp.SetCursorIndex(i)
							if entry.Name == fp.pendingSelection { fp.pendingSelection = "" }
							snapped = true
							break
						}
					}
				}

				if !snapped && (fp.cursorIdx >= len(fp.entries) || fp.cursorIdx < 0) {
					fp.SetCursorIndex(0)
				}

				fp.Refresh()
				vtui.FrameManager.Redraw() // Рисуем каждый чанк!
			})
		})

			vtui.FrameManager.PostTask(func() {
				if ctx.Err() != nil { return }
				// Останавливаем таймер. Если он не успел сработать — заголовок так и не моргнул.
				if fp.loadingTimer != nil { fp.loadingTimer.Stop() }

				fp.isLoading = false
				if err != nil && err != context.Canceled {
					// Баг-фикс: если директория исчезла (например, удалена из другой панели),
					// пытаемся подняться на уровень выше вместо показа ошибки.
					if !fp.vfs.IsAtRoot() {
						vtui.DebugLog("PANEL: Directory inaccessible, attempting to go up: %v", err)
						fp.vfs.SetPath("..")
						fp.ReadDirectory()
						return
					}

					fp.updateTitle(err)
					vtui.ShowMessage(" Error ", fmt.Sprintf("Failed to read directory:\n%v", err), []string{"&Ok"})
				} else {
					fp.updateTitle(nil)
				}

        if isFirstChunk {
            fp.entries = nil
            if !fp.vfs.IsAtRoot() || fp.vfs.ParentVFS() != nil {
                fp.entries = []*fileEntry{{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}}}
            }
            fp.SetCursorIndex(0)
        }

				if fp.pendingSelection != "" {
					fp.SelectName(fp.pendingSelection)
					fp.pendingSelection = ""
				}

				fp.Refresh()
				vtui.FrameManager.Redraw()
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
	// Sort indicator in top-left
	sortChar := "n"
	switch fp.sortMode {
	case SortExt: sortChar = "x"
	case SortTime: sortChar = "t"
	case SortSize: sortChar = "s"
	case SortUnsorted: sortChar = "u"
	}
	if fp.sortReverse {
		sortChar = strings.ToUpper(sortChar)
	}
	scr.Write(fp.X1+2, fp.Y1, vtui.StringToCharInfo(sortChar, vtui.Palette[ColPanelTitle]))
	fp.table.SetFocus(fp.IsFocused())
	fp.table.Show(scr)
	if fp.fastFindMode {
		if vtui.ManageCursorStyle {
			os.Stdout.WriteString("\x1b[3 q") // Blinking underline
		}
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
		switch e.VirtualKeyCode {
		case vtinput.VK_LEFT, vtinput.VK_RIGHT, vtinput.VK_PRIOR, vtinput.VK_NEXT, vtinput.VK_HOME, vtinput.VK_END:
			fp.fastFindMode = false
			fp.fastFindStr = ""
			vtui.FrameManager.Redraw()
			// Проваливаемся дальше, чтобы обработать саму навигацию
		case vtinput.VK_ESCAPE:
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
			fp.fastFindStr += string(unicode.ToLower(e.Char))
			fp.doFastFind(0)
			vtui.FrameManager.Redraw()
			return true
		}
	} else {
		if e.Char != 0 && alt && !ctrl && unicode.IsPrint(e.Char) {
			fp.fastFindMode = true
			fp.fastFindStr = string(unicode.ToLower(e.Char))
			fp.doFastFind(0)
			vtui.FrameManager.Redraw()
			return true
		}
	}


	switch e.VirtualKeyCode {
	case vtinput.VK_INSERT:
		if shift || ctrl || alt {
			return false
		}
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

			// Logic for leaving a virtual VFS (like an archive)
			if selected.Name == ".." {

				parent := fp.vfs.ParentVFS()
				isRoot := fp.vfs.IsAtRoot()

				if parent != nil && isRoot {
					oldPath := fp.vfs.GetPath()

					// Закрываем текущую систему (удаляем временные файлы)
					fp.vfs.Close()

					fp.vfs = parent
					fp.pendingSelection = fp.vfs.Base(oldPath)
					fp.ReadDirectory()
					return true
				}
			}

			if selected.IsDir {
				oldPath := fp.vfs.GetPath()
				newPath := fp.vfs.Join(oldPath, selected.Name)
				vtui.DebugLog("PANEL: Navigating %q -> %q", oldPath, newPath)
				if err := fp.vfs.SetPath(newPath); err == nil {
					if selected.Name == ".." {
						fp.pendingSelection = fp.vfs.Base(oldPath)
					} else {
						fp.pendingSelection = ".."
					}
					fp.ReadDirectory()
					return true
				}
			} else {
				// Просим VFS реестр подобрать провайдера для этого файла
				fullPath := fp.vfs.Join(fp.vfs.GetPath(), selected.Name)
				if provider := vfs.FindProvider(context.Background(), fp.vfs, fullPath); provider != nil {
					// Мгновенная реакция UI: показываем ".." для возможности отмены
					fp.entries = []*fileEntry{{VFSItem: vfs.VFSItem{Name: "..", IsDir: true}}}
					fp.isLoading = true
					fp.updateTitle(nil)
					fp.Refresh()
					fp.SetCursorIndex(0)
					vtui.FrameManager.Redraw()

					vtui.RunAsync(func(ctx *vtui.TaskContext) {
						newVfs, err := provider.Open(ctx.Context, fp.vfs, fullPath)
						ctx.RunOnUI(func() {
							if err != nil {
								fp.isLoading = false
								fp.updateTitle(err)
								fp.ReadDirectory() // Возвращаемся к списку соединений
								vtui.ShowMessage(" Connection Error ", fmt.Sprintf("Failed to connect to %s:\n%v", selected.Name, err), []string{"&Ok"})
								return
							}
							fp.vfs = newVfs
							fp.pendingSelection = ".."
							fp.ReadDirectory()
						})
					})
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
			// SelectPos is already absolute (TopPos + row) in Medium mode,
			// so we just add the column offset.
			newIdx := fp.table.SelectPos + fp.table.SelectCol*H

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
	// 1. Collect explicitly selected items (ins/shift+arrows)
	for _, e := range fp.entries {
		if e.Selected && e.Name != ".." {
			names = append(names, e.Name)
		}
	}
	// 2. If nothing is selected, fallback to the item under cursor
	if len(names) == 0 {
		idx := fp.GetCursorIndex()
		if idx >= 0 && idx < len(fp.entries) {
			entry := fp.entries[idx]
			// CRITICAL: Prevent actions on parent directory ".."
			if entry.Name != ".." {
				names = append(names, entry.Name)
			}
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
	searchXlat := strings.ToLower(vtui.GlobalXlator.TranscodeString(fp.fastFindStr))
	startIdx := fp.GetCursorIndex()

	checkMatch := func(i int) bool {
		nameLower := strings.ToLower(fp.entries[i].Name)
		return strings.HasPrefix(nameLower, searchLower) || strings.HasPrefix(nameLower, searchXlat)
	}

	if dir == 0 {
		for i := 0; i < len(fp.entries); i++ {
			if checkMatch(i) {
				fp.SetCursorIndex(i)
				fp.Refresh()
				return
			}
		}
	} else if dir == 1 {
		for i := startIdx + 1; i < len(fp.entries); i++ {
			if checkMatch(i) {
				fp.SetCursorIndex(i)
				fp.Refresh()
				return
			}
		}
		for i := 0; i <= startIdx; i++ {
			if checkMatch(i) {
				fp.SetCursorIndex(i)
				fp.Refresh()
				return
			}
		}
	} else if dir == -1 {
		for i := startIdx - 1; i >= 0; i-- {
			if checkMatch(i) {
				fp.SetCursorIndex(i)
				fp.Refresh()
				return
			}
		}
		for i := len(fp.entries) - 1; i >= startIdx; i-- {
			if checkMatch(i) {
				fp.SetCursorIndex(i)
				fp.Refresh()
				return
			}
		}
	}
}

func (fp *FileSystemPanel) InvertSelection() {
	for _, e := range fp.entries {
		if e.Name != ".." {
			e.Selected = !e.Selected
		}
	}
	vtui.FrameManager.Redraw()
}

func (fp *FileSystemPanel) ApplyMaskSelection(mask string, state bool) {
	if mask == "" {
		return
	}
	// Far style: *.* matches everything
	if mask == "*.*" {
		mask = "*"
	}
	maskLower := strings.ToLower(mask)

	for _, e := range fp.entries {
		if e.Name == ".." {
			continue
		}
		nameLower := strings.ToLower(e.Name)
		matched, _ := filepath.Match(maskLower, nameLower)
		if matched {
			e.Selected = state
		}
	}
	vtui.FrameManager.Redraw()
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
