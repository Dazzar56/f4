package main

import (
	"unicode"
	"unicode/utf8"
	"fmt"
	"path/filepath"
	"context"
	"time"
	"strings"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
	"github.com/unxed/vtui/piecetable"
	"github.com/unxed/vtui/textlayout"
)

type visualCell struct {
	info       vtui.CharInfo
	byteOffset int // Offset in bytes from the start of the logical line
}

type lineFragment struct {
	cells            []visualCell
	startOffset      int // Absolute offset of the fragment start
	startByteInLine  int // Byte in the logical line where the fragment starts
	endByteInLine    int // Byte where the fragment ends
}

// EditorView is a text editor component.
type EditorView struct {
	vtui.BaseFrame
	topBar  *TopBar
	menuBar *vtui.MenuBar
	pt     *piecetable.PieceTable
	li     *piecetable.LineIndex
	engine *textlayout.WrapEngine

	ScrollTopRow int // Индекс первой видимой ВИЗУАЛЬНОЙ строки
	ScrollLeft   int // Горизонтальный скролл (когда WordWrap=false)

	WordWrap         bool
	lastSearch       string
	modified         bool
	CursorLine       int // Текущая логическая строка (для плагинов)
	CursorPos        int // Позиция в байтах (для плагинов)
	DesiredVisualCol int // Колонка, в которую мы хотим попасть при навигации Up/Down

	selActive       bool
	selAnchorOffset int // Абсолютное смещение начала выделения

	pasting     bool
	saving      bool
	edited      bool
	pasteBuffer []rune
	asyncBuf    *AsyncBuffer
	indexCancel context.CancelFunc
	renderBytes []byte          // Reusable buffer for text data
	renderCells []vtui.CharInfo // Reusable buffer for row rendering

	vfs       vfs.VFS
	filePath  string
	file      vfs.ReadAtCloser
	scrollBar *vtui.ScrollBar
}

func (ev *EditorView) Close() {
	if ev.indexCancel != nil {
		ev.indexCancel()
	}
	if ev.asyncBuf != nil {
		ev.asyncBuf.Close()
	}
	if ev.file != nil {
		ev.file.Close()
	}
	ev.BaseFrame.Close()
}

func NewEditorView(pt *piecetable.PieceTable, v vfs.VFS, path string) *EditorView {
	li := piecetable.NewLineIndex()
	li.Rebuild(pt)
	ev := &EditorView{
		pt:       pt,
		li:       li,
		engine:   textlayout.NewWrapEngine(pt, li),
		vfs:      v,
		filePath: path,
		WordWrap: true,
	}
	ev.scrollBar = vtui.NewScrollBar(0, 0, 0)
	ev.scrollBar.SetOwner(ev)
	ev.scrollBar.OnScroll = func(v int) {
		ev.ScrollTopRow = v
		vtui.FrameManager.Redraw()
	}
	ev.menuBar = vtui.NewMenuBar(nil)
	ev.menuBar.Items = []vtui.MenuBarItem{
		{Label: "&File", SubItems: []vtui.MenuItem{{Text: "&Save", Command: vtui.CmDefault}, {Text: "E&xit", Command: vtui.CmClose}}},
		{Label: "&Edit", SubItems: []vtui.MenuItem{{Text: "&Copy", Command: CmCopy}, {Text: "&Paste"}}},
		{Label: "&Search", SubItems: []vtui.MenuItem{{Text: "&Find", Command: CmSearch}}},
		{Label: "&Options", SubItems: []vtui.MenuItem{{Text: "&WordWrap"}}},
	}

	ev.topBar = NewTopBar(func() string {
		base := ""
		if ev.vfs != nil {
			base = ev.vfs.Base(ev.filePath)
		} else {
			base = filepath.Base(ev.filePath)
		}
		return fmt.Sprintf(" %s │ %d,%d ", base, ev.CursorLine+1, ev.CursorPos)
	})
	ev.topBar.SetVisible(true)
	ev.SetCanFocus(true)
	ev.SetFocus(true)
	return ev
}

// GetTopBar возвращает верхнюю панель для тестов
func (ev *EditorView) GetTopBar() *TopBar {
	return ev.topBar
}

// SetText replaces the entire content of the editor.
func (ev *EditorView) SetText(text string) {
	ev.pt = piecetable.New([]byte(text))
	ev.li.Rebuild(ev.pt)
	ev.CursorLine = 0
	ev.CursorPos = 0
	ev.engine.InvalidateCache()
	ev.modified = true
}

func (ev *EditorView) clearCaches() {
	ev.engine.InvalidateCache()
}
func (ev *EditorView) ensureEngineWidth() {
	width := ev.X2 - ev.X1 + 1
	if ev.scrollBar != nil {
		width--
	}
	if width < 1 {
		width = 1
	}
	ev.engine.SetWidth(width)
	ev.engine.ToggleWrap(ev.WordWrap)
}

func (ev *EditorView) updateDesiredVisualCol() {
	curOffset := ev.li.GetLineOffset(ev.CursorLine) + ev.CursorPos
	_, vCol := ev.engine.LogicalToVisual(curOffset)
	ev.DesiredVisualCol = vCol
}

func (ev *EditorView) Show(scr *vtui.ScreenBuf) {
	ev.ScreenObject.Show(scr)
	if ev.topBar != nil {
		ev.topBar.Show(scr)
	}
	ev.DisplayObject(scr)
}

func (ev *EditorView) DisplayObject(scr *vtui.ScreenBuf) {
	if !ev.IsVisible() || ev.pasting {
		return
	}

	ev.ensureEngineWidth()
	height := ev.Y2 - ev.Y1
	width := ev.X2 - ev.X1 + 1
	if ev.scrollBar != nil {
		width--
	}

	bgAttr := vtui.Palette[ColCommandLineUserScreen]
	selAttr := vtui.Palette[vtui.ColDialogEditSelected]

	if ev.saving {
		scr.FillRect(ev.X1, ev.Y1+1, ev.X2, ev.Y2, ' ', bgAttr)
		scr.Write(ev.X1, ev.Y1+1, vtui.StringToCharInfo(" [ Saving... ] ", bgAttr))
		return
	}

	// Clear the entire editor text area
	scr.FillRect(ev.X1, ev.Y1+1, ev.X2, ev.Y2, ' ', bgAttr)
	scr.PushClipRect(ev.X1, ev.Y1+1, ev.X1+width-1, ev.Y2)

	// 1. Позиция курсора
	curOffset := ev.li.GetLineOffset(ev.CursorLine) + ev.CursorPos
	curVRow, curVCol := ev.engine.LogicalToVisual(curOffset)

	// 2. Отрисовка
	startLogLine, startFragIdx := ev.engine.GetLogLineAtVisualRow(ev.ScrollTopRow)
	rowsRendered := 0

	for logIdx := startLogLine; logIdx < ev.li.LineCount(); logIdx++ {
		frags := ev.engine.GetFragments(logIdx)
		baseVRow := ev.engine.GetRowOffset(logIdx)

		for fIdx, frag := range frags {
			if logIdx == startLogLine && fIdx < startFragIdx {
				continue
			}

			absVRow := baseVRow + fIdx
			currY := ev.Y1 + 1 + rowsRendered

			ev.renderBytes = ev.renderBytes[:0]
			var err error
			ev.renderBytes, err = ev.pt.AppendRange(ev.renderBytes, frag.ByteOffsetStart, frag.ByteOffsetEnd-frag.ByteOffsetStart)

			if err == piecetable.ErrLoading {
				scr.Write(ev.X1-ev.ScrollLeft, currY, vtui.StringToCharInfo(" [ Loading... ] ", bgAttr))
				rowsRendered++
				if rowsRendered >= height { goto DoneRendering }
				continue
			}

			if ev.selActive {
				selMin, selMax := ev.getSelectionRange()
				ev.renderCells = vtui.FillCharInfoWithSelection(ev.renderCells, ev.renderBytes, bgAttr, selAttr, frag.ByteOffsetStart, selMin, selMax)
			} else {
				ev.renderCells = vtui.FillCharInfo(ev.renderCells, ev.renderBytes, bgAttr)
			}

			scr.Write(ev.X1-ev.ScrollLeft, currY, ev.renderCells)

			if absVRow == curVRow {
				scr.SetCursorPos(ev.X1+curVCol-ev.ScrollLeft, currY)
				scr.SetCursorVisible(true)
			}

			rowsRendered++
			if rowsRendered >= height {
				goto DoneRendering
			}
		}
	}

DoneRendering:
	scr.PopClipRect()

	if ev.scrollBar != nil {
		totalRows := ev.engine.GetTotalVisualRows()
		if totalRows > height {
			ev.scrollBar.SetParams(ev.ScrollTopRow, 0, totalRows-height)
			ev.scrollBar.Show(scr)
		}
	}
}

func (ev *EditorView) ProcessKey(e *vtinput.InputEvent) bool {
	ev.ensureEngineWidth()
	if ev.saving { return true }
	// 1. Processing Bracketed Paste (events arrive outside KeyDown)
	if e.Type == vtinput.PasteEventType {
		if e.PasteStart {
			ev.pasting = true
			ev.pasteBuffer = nil
		} else {
			ev.pasting = false
			if len(ev.pasteBuffer) > 0 {
				if ev.selActive { ev.DeleteSelection() }
				offset := ev.li.GetLineOffset(ev.CursorLine) + ev.CursorPos
				data := []byte(string(ev.pasteBuffer))
				ev.pt.Insert(offset, data)
				// Incremental update instead of heavy Rebuild
				ev.li.UpdateAfterInsert(offset, data)
				ev.engine.InvalidateFrom(ev.CursorLine)

				newOffset := offset + len(data)
				ev.CursorLine = ev.li.GetLineAtOffset(newOffset)
				ev.CursorPos = newOffset - ev.li.GetLineOffset(ev.CursorLine)
				ev.modified = true
				ev.updateDesiredVisualCol()
				ev.ensureCursorVisible()
			}
		}
		return true
	}

	// 2. Accumulating characters in paste mode
	if ev.pasting {
		if e.Type == vtinput.KeyEventType && e.KeyDown {
			if e.Char != 0 {
				// Handle system line breaks inside the paste
				if e.Char == '\r' {
					// Ignore \r to prevent double line breaks
				} else if e.Char == '\n' {
					ev.pasteBuffer = append(ev.pasteBuffer, '\n')
				} else {
					ev.pasteBuffer = append(ev.pasteBuffer, e.Char)
				}
			} else if e.VirtualKeyCode == vtinput.VK_RETURN {
				ev.pasteBuffer = append(ev.pasteBuffer, '\n')
			}
		}
		return true
	}

	// 3. Regular key processing
	if !e.KeyDown { return false }

	shift := (e.ControlKeyState & vtinput.ShiftPressed) != 0
	ctrl := (e.ControlKeyState & (vtinput.LeftCtrlPressed | vtinput.RightCtrlPressed)) != 0

	// Allow FrameManager to handle Ctrl+Tab for workspace switching
	if e.VirtualKeyCode == vtinput.VK_TAB && ctrl {
		return false
	}

	handleNav := func() {
		if shift {
			if !ev.selActive {
				ev.selActive = true
				ev.selAnchorOffset = ev.li.GetLineOffset(ev.CursorLine) + ev.CursorPos
			}
		} else {
			ev.selActive = false
		}
	}

	// Any key that can reach this point and is not a pure navigation key 
	// should stop the background indexer to prevent index corruption.
	if !ev.edited {
		switch e.VirtualKeyCode {
		case vtinput.VK_UP, vtinput.VK_DOWN, vtinput.VK_LEFT, vtinput.VK_RIGHT,
			vtinput.VK_PRIOR, vtinput.VK_NEXT, vtinput.VK_HOME, vtinput.VK_END:
			// ignore navigation
		default:
			ev.edited = true
			if ev.indexCancel != nil { ev.indexCancel() }
		}
	}

	switch e.VirtualKeyCode {
	case vtinput.VK_A:
		if ctrl {
			ev.selActive = true
			ev.selAnchorOffset = 0
			lastLine := ev.li.LineCount() - 1
			ev.CursorLine = lastLine
			ev.CursorPos = ev.getLineLength(lastLine)
			ev.ensureCursorVisible()
			return true
		}

	case vtinput.VK_F2:
		ev.SaveToFile(nil)
		return true

	case vtinput.VK_F3:
		ev.WordWrap = !ev.WordWrap
		ev.ScrollLeft = 0
		ev.clearCaches()
		ev.ensureCursorVisible()
		return true

	case vtinput.VK_F7:
		if shift && ev.lastSearch != "" {
			ev.Search(ev.lastSearch, true)
		} else {
			vtui.FrameManager.EmitCommand(CmSearch, nil)
		}
		return true
	
	case vtinput.VK_ESCAPE, vtinput.VK_F10:
		ev.tryClose()
		return true

	case vtinput.VK_C, vtinput.VK_INSERT:
		if ctrl && ev.selActive {
			ev.CopySelection()
			return true
		}

	case vtinput.VK_UP, vtinput.VK_E:
		if e.VirtualKeyCode == vtinput.VK_E && !ctrl { break }
		handleNav()
		curOffset := ev.li.GetLineOffset(ev.CursorLine) + ev.CursorPos
		vRow, _ := ev.engine.LogicalToVisual(curOffset)
		if vRow > 0 {
			newOffset := ev.engine.VisualToLogical(vRow-1, ev.DesiredVisualCol)
			ev.CursorLine = ev.li.GetLineAtOffset(newOffset)
			ev.CursorPos = newOffset - ev.li.GetLineOffset(ev.CursorLine)
		}
		ev.ensureCursorVisible()
		return true

	case vtinput.VK_DOWN, vtinput.VK_X:
		if e.VirtualKeyCode == vtinput.VK_X {
			if !ctrl { break }
			if ev.selActive {
				ev.CopySelection()
				ev.DeleteSelection()
				return true
			}
		}
		handleNav()
		curOffset := ev.li.GetLineOffset(ev.CursorLine) + ev.CursorPos
		vRow, _ := ev.engine.LogicalToVisual(curOffset)
		newOffset := ev.engine.VisualToLogical(vRow+1, ev.DesiredVisualCol)
		ev.CursorLine = ev.li.GetLineAtOffset(newOffset)
		ev.CursorPos = newOffset - ev.li.GetLineOffset(ev.CursorLine)
		ev.ensureCursorVisible()
		return true

	case vtinput.VK_PRIOR: // PgUp
		handleNav()
		height := ev.Y2 - ev.Y1
		curOffset := ev.li.GetLineOffset(ev.CursorLine) + ev.CursorPos
		vRow, _ := ev.engine.LogicalToVisual(curOffset)
		newVRow := vRow - height
		if newVRow < 0 {
			newVRow = 0
		}
		newOffset := ev.engine.VisualToLogical(newVRow, ev.DesiredVisualCol)
		ev.CursorLine = ev.li.GetLineAtOffset(newOffset)
		ev.CursorPos = newOffset - ev.li.GetLineOffset(ev.CursorLine)
		ev.ensureCursorVisible()
		return true

	case vtinput.VK_NEXT: // PgDn
		handleNav()
		height := ev.Y2 - ev.Y1
		curOffset := ev.li.GetLineOffset(ev.CursorLine) + ev.CursorPos
		vRow, _ := ev.engine.LogicalToVisual(curOffset)
		newVRow := vRow + height
		totalVRows := ev.engine.GetTotalVisualRows()
		if newVRow >= totalVRows {
			newVRow = totalVRows - 1
		}
		newOffset := ev.engine.VisualToLogical(newVRow, ev.DesiredVisualCol)
		ev.CursorLine = ev.li.GetLineAtOffset(newOffset)
		ev.CursorPos = newOffset - ev.li.GetLineOffset(ev.CursorLine)
		ev.ensureCursorVisible()
		return true

	case vtinput.VK_LEFT, vtinput.VK_S:
		isAlias := e.VirtualKeyCode == vtinput.VK_S
		if isAlias && !ctrl { break }
		handleNav()
		// Jump by word only if it's the real Left arrow + Ctrl
		if ctrl && !isAlias {
			runes := ev.getLogicalLineRunes(ev.CursorLine)

			// Find current rune position
			currRuneIdx := 0
			byteAcc := 0
			for i, r := range runes {
				if byteAcc >= ev.CursorPos {
					currRuneIdx = i
					break
				}
				byteAcc += utf8.RuneLen(r)
				if i == len(runes)-1 { currRuneIdx = len(runes) }
			}

			if currRuneIdx > 0 {
				pos := currRuneIdx
				// Skip spaces
				for pos > 0 && unicode.IsSpace(runes[pos-1]) { pos-- }
				// Skip word
				for pos > 0 && !unicode.IsSpace(runes[pos-1]) { pos-- }

				// Convert rune index back to byte offset
				newBytePos := 0
				for i := 0; i < pos; i++ {
					newBytePos += utf8.RuneLen(runes[i])
				}
				ev.CursorPos = newBytePos
			} else if ev.CursorLine > 0 {
				ev.CursorLine--
				ev.CursorPos = ev.getLineLength(ev.CursorLine)
			}
		} else {
			if ev.CursorPos > 0 {
				lineStart := ev.li.GetLineOffset(ev.CursorLine)
				data, _ := ev.pt.GetRange(lineStart, ev.CursorPos)
				if data != nil && len(data) > 0 {
					_, size := utf8.DecodeLastRune(data)
					ev.CursorPos -= size
				} else {
					ev.CursorPos--
				}
			} else if ev.CursorLine > 0 {
				ev.CursorLine--
				ev.CursorPos = ev.getLineLength(ev.CursorLine)
			}
		}
		ev.updateDesiredVisualCol()
		ev.ensureCursorVisible()
		return true

	case vtinput.VK_RIGHT, vtinput.VK_D:
		isAlias := e.VirtualKeyCode == vtinput.VK_D
		if isAlias && !ctrl { break }
		handleNav()
		lineLen := ev.getLineLength(ev.CursorLine)
		// Jump by word only if it's the real Right arrow + Ctrl
		if ctrl && !isAlias {
			runes := ev.getLogicalLineRunes(ev.CursorLine)

			// Find current rune position
			currRuneIdx := len(runes)
			byteAcc := 0
			for i, r := range runes {
				if byteAcc >= ev.CursorPos {
					currRuneIdx = i
					break
				}
				byteAcc += utf8.RuneLen(r)
			}

			if currRuneIdx < len(runes) {
				pos := currRuneIdx
				// Skip word
				for pos < len(runes) && !unicode.IsSpace(runes[pos]) { pos++ }
				// Skip spaces
				for pos < len(runes) && unicode.IsSpace(runes[pos]) { pos++ }

				// Convert rune index back to byte offset
				newBytePos := 0
				for i := 0; i < pos; i++ {
					newBytePos += utf8.RuneLen(runes[i])
				}
				ev.CursorPos = newBytePos
			} else if ev.CursorLine < ev.li.LineCount()-1 {
				ev.CursorLine++
				ev.CursorPos = 0
			}
		} else {
			if ev.CursorPos < lineLen {
				lineStart := ev.li.GetLineOffset(ev.CursorLine)
				peekLen := 4
				if lineLen-ev.CursorPos < 4 { peekLen = lineLen - ev.CursorPos }
				data, _ := ev.pt.GetRange(lineStart+ev.CursorPos, peekLen)
				if data != nil && len(data) > 0 {
					_, size := utf8.DecodeRune(data)
					ev.CursorPos += size
				} else {
					ev.CursorPos++
				}
			} else if ev.CursorLine < ev.li.LineCount()-1 {
				ev.CursorLine++
				ev.CursorPos = 0
			}
		}
		ev.updateDesiredVisualCol()
		ev.ensureCursorVisible()
		return true

	case vtinput.VK_HOME:
		handleNav()
		if ctrl {
			ev.CursorLine = 0
		}
		ev.CursorPos = 0
		ev.updateDesiredVisualCol()
		ev.ensureCursorVisible()
		return true

	case vtinput.VK_END:
		handleNav()
		if ctrl {
			ev.CursorLine = ev.li.LineCount() - 1
		}
		ev.CursorPos = ev.getLineLength(ev.CursorLine)
		ev.updateDesiredVisualCol()
		ev.ensureCursorVisible()
		return true

	case vtinput.VK_BACK:
		if ev.selActive {
			ev.DeleteSelection()
		} else {
			ev.modified = true
			offset := ev.li.GetLineOffset(ev.CursorLine) + ev.CursorPos
			if offset > 0 {
				if ev.CursorPos == 0 {
					// Merge with the previous line (remove line break)
					prevLen := ev.getLineLength(ev.CursorLine - 1)
					delLen := 1
					// Check for CRLF (\r\n)
					if offset >= 2 {
						prefix, _ := ev.pt.GetRange(offset-2, 2)
						if len(prefix) == 2 && prefix[0] == '\r' && prefix[1] == '\n' {
							delLen = 2
						}
					}

					ev.pt.Delete(offset-delLen, delLen)
					ev.li.UpdateAfterDelete(offset-delLen, delLen)
					ev.engine.InvalidateFrom(ev.CursorLine - 1)
					ev.CursorLine--
					ev.CursorPos = prevLen
				} else {
					// Remove the UTF-8 character before the cursor
					lineStart := ev.li.GetLineOffset(ev.CursorLine)
					lineData, _ := ev.pt.GetRange(lineStart, ev.CursorPos)
					size := 1
					if lineData != nil {
						_, size = utf8.DecodeLastRune(lineData)
					}

					ev.pt.Delete(offset-size, size)
					ev.li.UpdateAfterDelete(offset-size, size)
					ev.engine.InvalidateFrom(ev.CursorLine)
					ev.CursorPos -= size
				}
			}
		}
		ev.updateDesiredVisualCol()
		ev.ensureCursorVisible()
		return true

	case vtinput.VK_DELETE:
		if ev.selActive {
			if shift {
				ev.CopySelection()
			}
			ev.DeleteSelection()
		} else {
			ev.modified = true
			offset := ev.li.GetLineOffset(ev.CursorLine) + ev.CursorPos
			if offset < ev.pt.Size() {
				// Remove the UTF-8 character under the cursor
				peekLen := 4
				if ev.pt.Size()-offset < 4 { peekLen = ev.pt.Size() - offset }
				data, _ := ev.pt.GetRange(offset, peekLen)
				size := 1
				if data != nil {
					_, size = utf8.DecodeRune(data)
				}

				ev.pt.Delete(offset, size)
				ev.li.UpdateAfterDelete(offset, size)
				ev.engine.InvalidateFrom(ev.CursorLine)
			}
		}
		ev.ensureCursorVisible()
		return true

	case vtinput.VK_RETURN:
		if ev.selActive {
			ev.DeleteSelection()
		}
		ev.modified = true
		offset := ev.li.GetLineOffset(ev.CursorLine) + ev.CursorPos
		ev.pt.Insert(offset, []byte("\n"))
		ev.li.UpdateAfterInsert(offset, []byte("\n"))
		ev.engine.InvalidateFrom(ev.CursorLine)
		ev.CursorLine++
		ev.CursorPos = 0
		ev.DesiredVisualCol = 0
		ev.ensureCursorVisible()
		return true
	}

	if e.Char != 0 && ctrl == false {
		if ev.selActive {
			ev.DeleteSelection()
		}
		ev.modified = true
		offset := ev.li.GetLineOffset(ev.CursorLine) + ev.CursorPos
		data := []byte(string(e.Char))
		ev.pt.Insert(offset, data)
		ev.li.UpdateAfterInsert(offset, data)
		ev.engine.InvalidateFrom(ev.CursorLine)
		ev.CursorPos += len(data)
		ev.updateDesiredVisualCol()
		ev.ensureCursorVisible()
		return true
	}

	return false
}

func (ev *EditorView) ensureCursorVisible() {
	width := ev.X2 - ev.X1 + 1
	height := ev.Y2 - ev.Y1
	if width <= 0 || height <= 0 { return }

	curOffset := ev.li.GetLineOffset(ev.CursorLine) + ev.CursorPos
	vRow, vCol := ev.engine.LogicalToVisual(curOffset)

	// 1. Вертикальный скролл
	if vRow < ev.ScrollTopRow {
		ev.ScrollTopRow = vRow
	} else if vRow >= ev.ScrollTopRow + height {
		ev.ScrollTopRow = vRow - height + 1
	}

	// 2. Горизонтальный скролл (только если WordWrap выключен)
	if !ev.WordWrap {
		if vCol < ev.ScrollLeft {
			ev.ScrollLeft = vCol
		} else if vCol >= ev.ScrollLeft+width {
			ev.ScrollLeft = vCol - width + 1
		}
	} else {
		ev.ScrollLeft = 0
	}
}

func (ev *EditorView) ProcessMouse(e *vtinput.InputEvent) bool {
	if e.Type != vtinput.MouseEventType {
		return false
	}

	if ev.scrollBar != nil && ev.scrollBar.ProcessMouse(e) {
		return true
	}

	if e.WheelDirection != 0 {
		if e.WheelDirection > 0 {
			ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_UP})
		} else {
			ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
		}
		return true
	}
	return false
}

func (ev *EditorView) SetPosition(x1, y1, x2, y2 int) {
	ev.ScreenObject.SetPosition(x1, y1, x2, y2)
	if ev.topBar != nil {
		ev.topBar.SetPosition(x1, y1, x2, y1)
	}
	if ev.menuBar != nil {
		ev.menuBar.SetPosition(x1, 0, x2, 0)
	}
	if ev.scrollBar != nil {
		ev.scrollBar.SetPosition(x2, y1+1, x2, y2)
		ev.scrollBar.PgStep = y2 - y1
	}
	ev.ensureEngineWidth()
	ev.ensureCursorVisible()
}

func (ev *EditorView) ResizeConsole(w, h int) {
	// Редактор в f4 занимает всё пространство до KeyBar (h-1)
	ev.SetPosition(0, 0, w-1, h-2)
}

func (ev *EditorView) GetMenuBar() *vtui.MenuBar { return ev.menuBar }

func (ev *EditorView) StartIndexing() {
	if ev.asyncBuf == nil { return }
	if ev.indexCancel != nil { ev.indexCancel() }

	ctx, cancel := context.WithCancel(context.Background())
	ev.indexCancel = cancel

	go func() {
		absPos := 0
		chunkSize := 64 * 1024
		buf := ev.asyncBuf
		li := ev.li

		for absPos < buf.Size() {
			select {
			case <-ctx.Done(): return
			default:
			}

			if ev.IsDone() { return }

			data, err := buf.Read(absPos, chunkSize)
			if err == piecetable.ErrLoading {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			if err != nil { break }

			var newOffsets []int
			for i, b := range data {
				if b == '\n' {
					newOffsets = append(newOffsets, absPos+i+1)
				}
			}

			if len(newOffsets) > 0 {
				vtui.FrameManager.PostTask(func() {
					if ctx.Err() != nil || ev.edited { return }
					li.AppendOffsets(newOffsets)
					ev.engine.InvalidateCache()
					vtui.FrameManager.Redraw()
				})
			}
			absPos += len(data)
		}
		vtui.DebugLog("INDEXER: Finished for %s", ev.filePath)
	}()
}

func (ev *EditorView) HandleCommand(cmd int, args any) bool {
	if cmd == vtui.CmClose {
		ev.tryClose()
		return true
	}
	if cmd == CmSearch {
		vtui.InputBox(Msg("Viewer.SearchTitle"), "Search for:", ev.lastSearch, func(p string) {
			ev.Search(p, false)
		})
		return true
	}
	return ev.BaseFrame.HandleCommand(cmd, args)
}

func (ev *EditorView) tryClose() {
	if !ev.modified {
		ev.Close()
		return
	}

	msg := "The file has been modified.\nDo you want to save it?"
	dlg := vtui.ShowMessage(" Confirm ", msg, []string{"&Save", "&Don't Save", "Cancel"})
	dlg.OnResult = func(code int) {
		switch code {
		case 0: // Save
			ev.SaveToFile(func() {
				ev.Close()
			})
		case 1: // Don't save
			ev.Close()
		}
	}
}

func (ev *EditorView) GetKeyLabels() *vtui.KeySet {
	return &vtui.KeySet{
		Normal: vtui.KeyBarLabels{
			Msg("KeyBar.EditorF1"), Msg("KeyBar.EditorF2"), Msg("KeyBar.EditorF3"),
			"", "", "", Msg("KeyBar.EditorF7"), "", "", Msg("KeyBar.EditorF10"),
		},
	}
}
func (ev *EditorView) getLogicalLineRunes(line int) []rune {
	lineStart := ev.li.GetLineOffset(line)
	lineData, _ := ev.pt.GetRange(lineStart, ev.getLineLength(line))
	return []rune(string(lineData))
}
func (ev *EditorView) getLineLength(line int) int {
	if line < 0 || line >= ev.li.LineCount() {
		return 0
	}
	start := ev.li.GetLineOffset(line)
	end := ev.pt.Size()
	if line+1 < ev.li.LineCount() {
		end = ev.li.GetLineOffset(line + 1)
	}

	totalLen := end - start
	if totalLen <= 0 {
		return 0
	}

	data, err := ev.pt.GetRange(start, totalLen)
	if err == piecetable.ErrLoading || len(data) == 0 {
		return totalLen
	}

	// Safely decrease length if there are line breaks at the end.
	// First check for \n, then (if present) check for \r before it.
	if totalLen > 0 && data[totalLen-1] == '\n' {
		totalLen--
		if totalLen > 0 && data[totalLen-1] == '\r' {
			totalLen--
		}
	}
	return totalLen
}

func (ev *EditorView) SaveToFile(afterSave func()) {
	if ev.filePath == "" || ev.vfs == nil || ev.saving {
		return
	}
	
	ev.saving = true
	ev.edited = true
	vtui.DebugLog("EDITOR: Saving %s...", ev.filePath)

	// Stop indexing to prevent async reads on closed buffers
	if ev.indexCancel != nil {
		ev.indexCancel()
		ev.indexCancel = nil
	}

	// Capture visible offset for preloading before we destroy the current engine
	visStart := ev.engine.VisualToLogical(ev.ScrollTopRow, 0)

	vtui.RunAsync(func(ctx *vtui.TaskContext) {
		// Saving PieceTable content to VFS.
		tmpPath := ev.filePath + ".f4tmp"
		f, err := ev.vfs.Create(ctx.Context, tmpPath)
		if err != nil {
			ctx.RunOnUI(func() { 
				ev.saving = false
				vtui.DebugLog("EDITOR: Failed to open temp file for saving: %v", err) 
				vtui.ShowMessage(" Error ", fmt.Sprintf("Failed to save file:\n%v", err), []string{"&Ok"})
			})
			return
		}

		saveErr := ev.pt.ForEachRange(func(data []byte) error {
			_, errWrite := f.Write(data)
			return errWrite
		})
		f.Close()
		
		if saveErr != nil {
			ev.vfs.Remove(ctx.Context, tmpPath)
			ctx.RunOnUI(func() {
				ev.saving = false
				vtui.ShowMessage(" Error ", fmt.Sprintf("Failed to save data:\n%v", saveErr), []string{"&Ok"})
			})
			return
		}

		// On Windows, we might need to close the file before renaming.
		// However, doing so before Rename succeeds is dangerous for retries.
		// We use a temporary close-and-recover approach.
		oldAsync := ev.asyncBuf
		oldFile := ev.file
		if oldAsync != nil { oldAsync.Close() }
		if oldFile != nil { oldFile.Close() }

		err = ev.vfs.Rename(ctx.Context, tmpPath, ev.filePath)
		if err != nil {
			// RECOVER: Rename failed, try to reopen the original file so editor stays functional
			reopened, reerr := ev.vfs.Open(ctx.Context, ev.filePath)
			ctx.RunOnUI(func() {
				ev.saving = false
				if reerr == nil {
					ev.file = reopened
					ev.asyncBuf = NewAsyncBuffer(ctx.Context, reopened)
					// We don't change ev.pt here, it still points to the same logic.
					// But we'd need to reconstruct the PieceTable's 'orig' buffer if it was MemoryBuffer.
					// This is a rare edge case, but for now we at least prevent ev.file from being nil.
				}
				vtui.DebugLog("EDITOR: Failed to rename temp file: %v", err)
				vtui.ShowMessage(" Error ", fmt.Sprintf("Failed to save file:\n%v", err), []string{"&Ok"})
			})
			return
		}

		newFile, err := ev.vfs.Open(ctx.Context, ev.filePath)
		var newPt *piecetable.PieceTable
		var newEngine *textlayout.WrapEngine
		var newBuf *AsyncBuffer

		if err == nil {
			newBuf = NewAsyncBuffer(ctx.Context, newFile)
			newPt = piecetable.NewWithBuffer(newBuf)
			// Reuse the existing LineIndex since the logical content is identical
			newEngine = textlayout.NewWrapEngine(newPt, ev.li)
		}

		// PRELOAD CACHE TO PREVENT SCREEN FLICKER
		// This MUST be outside RunOnUI to prevent blocking the main thread for 500ms.
		for i := 0; i < 50; i++ { // max 500ms
			if ctx.Err() != nil { break }
			_, e := newBuf.Read(visStart, 4096)
			if e != piecetable.ErrLoading { break }
			time.Sleep(10 * time.Millisecond)
		}

		ctx.RunOnUI(func() {
			ev.saving = false
			if err == nil {
				vtui.DebugLog("EDITOR: Successfully saved %s (%d bytes)", ev.filePath, ev.pt.Size())
			}
			vtui.FrameManager.Broadcast(CmFileChanged, nil)

			if err == nil {
				ev.modified = false
				if afterSave != nil {
					afterSave()
				}
				ev.file = newFile
				ev.asyncBuf = newBuf
				ev.pt = newPt
				ev.engine = newEngine
				ev.ensureEngineWidth()
				ev.edited = false
			}
		})
	})
}

func (ev *EditorView) getSelectionRange() (int, int) {
	if !ev.selActive { return 0, 0 }
	cursorOffset := ev.li.GetLineOffset(ev.CursorLine) + ev.CursorPos
	min, max := ev.selAnchorOffset, cursorOffset
	if min > max { min, max = max, min }
	return min, max
}

func (ev *EditorView) CopySelection() {
	min, max := ev.getSelectionRange()
	if max > min {
		data, _ := ev.pt.GetRange(min, max-min)
		if data != nil {
			vtui.SetClipboard(string(data))
			vtui.DebugLog("EDITOR: Copied %d bytes to clipboard", max-min)
		}
	}
}

func (ev *EditorView) DeleteSelection() {
	min, max := ev.getSelectionRange()
	if max > min {
		ev.modified = true
		ev.pt.Delete(min, max-min)
		// Incremental update
		ev.li.UpdateAfterDelete(min, max-min)
		ev.clearCaches()
		ev.selActive = false
		// Update cursor position to the start of the former selection
		ev.CursorLine = ev.li.GetLineAtOffset(min)
		ev.CursorPos = min - ev.li.GetLineOffset(ev.CursorLine)
	}
}
func (ev *EditorView) GetType() vtui.FrameType { return vtui.TypeUser + 2 }
func (ev *EditorView) IsBusy() bool { return ev.pasting || ev.saving }
func (ev *EditorView) GetTitle() string {
	if ev.filePath != "" {
		return "Edit: " + filepath.Base(ev.filePath)
	}
	return "Editor"
}
func (ev *EditorView) Search(pattern string, next bool) {
	if pattern == "" {
		return
	}
	ev.lastSearch = pattern

	title := " Searching... "
	msg := fmt.Sprintf("Looking for: %s", pattern)

	vtui.FrameManager.PostTask(func() {
		dlg := vtui.NewCenteredDialog(50, 8, title)
		lbl := vtui.NewLabel(0, 0, msg, nil)
		dlg.AddItem(lbl)
		btnCancel := vtui.NewButton(0, 0, "&Cancel")
		dlg.AddItem(btnCancel)

		vbox := vtui.NewVBoxLayout(dlg.X1+2, dlg.Y1+2, 50-4, 8-4)
		vbox.Add(lbl, vtui.Margins{}, vtui.AlignCenter)
		vbox.Add(btnCancel, vtui.Margins{Top: 1}, vtui.AlignCenter)
		vbox.Apply()

		vtui.FrameManager.AddScreenHeadless(dlg)

		_ = vtui.RunAsync(func(ctx *vtui.TaskContext) {
			btnCancel.OnClick = func() { ctx.Cancel(); dlg.Close() }

			startOff := ev.li.GetLineOffset(ev.CursorLine) + ev.CursorPos
			if next {
				startOff++
			}

			foundOffset := -1
			currOff := startOff
			totalSize := ev.pt.Size()
			patternLower := strings.ToLower(pattern)
			chunkSize := 256 * 1024

			for currOff < totalSize {
				if ctx.Err() != nil {
					return
				}
				percent := 0
				if totalSize > 0 {
					percent = int((currOff * 100) / totalSize)
				}
				ctx.RunOnUI(func() { dlg.SetProgress(percent) })

				readSize := chunkSize
				if currOff+readSize > totalSize {
					readSize = totalSize - currOff
				}

				data, err := ev.pt.GetRange(currOff, readSize)
				if err == piecetable.ErrLoading {
					time.Sleep(20 * time.Millisecond)
					continue
				}
				if len(data) == 0 {
					break
				}

				idx := strings.Index(strings.ToLower(string(data)), patternLower)
				if idx != -1 {
					foundOffset = currOff + idx
					break
				}

				advance := len(data) - len(patternLower)
				if advance <= 0 {
					advance = 1
				}
				currOff += advance
				if len(data) < chunkSize {
					break
				}
			}

			ctx.RunOnUI(func() {
				dlg.Close()
				if foundOffset != -1 {
					ev.selActive = true
					ev.selAnchorOffset = foundOffset

					endFound := foundOffset + len(pattern)
					ev.CursorLine = ev.li.GetLineAtOffset(endFound)
					ev.CursorPos = endFound - ev.li.GetLineOffset(ev.CursorLine)

					ev.updateDesiredVisualCol()
					ev.ensureCursorVisible()
					vtui.FrameManager.Redraw()
				} else if ctx.Err() == nil {
					vtui.ShowMessage(" Search ", "Pattern not found.", []string{"&Ok"})
				}
			})
		})
	})
}
