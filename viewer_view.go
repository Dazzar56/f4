package main

import (
	"bytes"
	"fmt"
	"unicode/utf8"
	"path/filepath"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
	"github.com/mattn/go-runewidth"
)

// ViewerView is a high-performance file viewer component.
type ViewerView struct {
	vtui.BaseFrame
	topBar  *TopBar
	menuBar *vtui.MenuBar
	backend *ViewerBackend
	vfs     vfs.VFS
	path    string

	HexMode   bool
	WrapMode  bool
	TopOffset int64 // Current byte offset of the first visible line

	// For Text mode: offsets of lines currently on screen
	lineOffsets []int64
	eofVisible  bool

	scrollBar *vtui.ScrollBar
}

func NewViewerView(v vfs.VFS, path string) (*ViewerView, error) {
	backend, err := NewViewerBackend(v, path)
	if err != nil {
		return nil, err
	}
	vv := &ViewerView{
		backend:  backend,
		vfs:      v,
		path:     path,
		WrapMode: true,
	}
	vv.scrollBar = vtui.NewScrollBar(0, 0, 0)
	vv.scrollBar.SetOwner(vv)
	vv.scrollBar.ScrollCommand = vv.AddCallback(func(args any) {
		if v, ok := args.(int); ok {
			// Used during dragging: snap to line start
			vv.TopOffset = vv.backend.FindLineStart(int64(v))
			vtui.FrameManager.Redraw()
		}
	})
	vv.scrollBar.StepCommand = vv.AddCallback(func(args any) {
		if step, ok := args.(int); ok {
			// Used for arrows and track clicks: perform logical steps
			switch step {
			case -1: vv.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_UP})
			case 1:  vv.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
			case -2: vv.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_PRIOR})
			case 2:  vv.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_NEXT})
			}
			vtui.FrameManager.Redraw()
		}
	})
	vv.menuBar = vtui.NewMenuBar(nil)
	vv.menuBar.Items = []vtui.MenuBarItem{
		{Label: "&File", SubItems: []vtui.MenuItem{{Text: "E&xit", Command: vtui.CmClose}}},
		{Label: "&View", SubItems: []vtui.MenuItem{{Text: "&Hex", Command: vtui.CmDefault}, {Text: "&Wrap"}}},
		{Label: "&Options", SubItems: []vtui.MenuItem{{Text: "&Settings"}}},
	}
	vv.topBar = NewTopBar(func() string {
		percent := 0
		size := vv.backend.Size()
		if size > 0 {
			viewHeightBytes := int64(vv.Y2 - vv.Y1)
			if vv.HexMode {
				viewHeightBytes *= 16
			} else {
				viewHeightBytes *= 80
			}
			if size <= viewHeightBytes {
				percent = 100
			} else {
				denominator := size - viewHeightBytes
				percent = int((vv.TopOffset * 100) / denominator)
			}
			if percent < 0 { percent = 0 }
			if percent > 100 { percent = 100 }
		}
		mode := Msg("Viewer.ModeText")
		if vv.HexMode { mode = Msg("Viewer.ModeHex") }
		base := ""
		if vv.vfs != nil {
			base = vv.vfs.Base(vv.path)
		} else {
			base = filepath.Base(vv.path)
		}
		return fmt.Sprintf(" %s │ %s │ %d%% ", base, mode, percent)
	})
	vv.topBar.SetVisible(true)
	vv.SetCanFocus(true)
	vv.SetFocus(true)
	return vv, nil
}


func (vv *ViewerView) SetPosition(x1, y1, x2, y2 int) {
	vv.ScreenObject.SetPosition(x1, y1, x2, y2)
	if vv.topBar != nil {
		vv.topBar.SetPosition(x1, y1, x2, y1)
	}
	if vv.menuBar != nil {
		vv.menuBar.SetPosition(x1, 0, x2, 0)
	}
	if vv.scrollBar != nil {
		vv.scrollBar.SetPosition(x2, y1+1, x2, y2)
	}
}

func (vv *ViewerView) GetMenuBar() *vtui.MenuBar {
	return vv.menuBar
}

func (vv *ViewerView) HandleCommand(cmd int, args any) bool {
	if cmd == vtui.CmClose {
		vv.SetExitCode(-1)
		return true
	}
	return vv.BaseFrame.HandleCommand(cmd, args)
}

func (vv *ViewerView) Show(scr *vtui.ScreenBuf) {
	vv.ScreenObject.Show(scr)
	if vv.topBar != nil {
		vv.topBar.Show(scr)
	}
	vv.DisplayObject(scr)
}

func (vv *ViewerView) DisplayObject(scr *vtui.ScreenBuf) {
	if !vv.IsVisible() {
		return
	}

	width := vv.X2 - vv.X1 + 1
	if vv.scrollBar != nil {
		width-- // Не рисуем текст поверх скроллбара
	}
	height := vv.Y2 - vv.Y1 + 1
	contentHeight := height - 1

	bgAttr := vtui.Palette[ColViewerText]

	// 1. Draw Background
	scr.FillRect(vv.X1, vv.Y1+1, vv.X2, vv.Y2, ' ', bgAttr)

	if contentHeight > 0 {
		if vv.HexMode {
			vv.renderHex(scr, width, contentHeight)
		} else {
			vv.renderText(scr, width, contentHeight)
		}
	}

	if vv.scrollBar != nil && vv.backend.Size() > 0 {
		vv.scrollBar.SetParams(int(vv.TopOffset), 0, int(vv.backend.Size()))
		vv.scrollBar.Show(scr)
	}
}

func (vv *ViewerView) renderHex(scr *vtui.ScreenBuf, width, contentHeight int) {
	attr := vtui.Palette[ColViewerText]
	offAttr := vtui.Palette[ColViewerArrows]

	currOffset := vv.TopOffset &^ 0xF // Align to 16 bytes

	for y := 0; y < contentHeight; y++ {
		if currOffset >= vv.backend.Size() {
			break
		}

		data, _ := vv.backend.ReadAt(currOffset, 16)
		line := fmt.Sprintf("%010X: ", currOffset)
		scr.Write(vv.X1, vv.Y1+1+y, vtui.StringToCharInfo(line, offAttr))

		// Hex part
		hexStr := ""
		for i := 0; i < 16; i++ {
			if i < len(data) {
				hexStr += fmt.Sprintf("%02X ", data[i])
			} else {
				hexStr += "   "
			}
			if i == 7 {
				hexStr += " "
			}
		}
		scr.Write(vv.X1+12, vv.Y1+1+y, vtui.StringToCharInfo(hexStr, attr))

		// ASCII part
		asciiStr := "│ "
		for i := 0; i < len(data); i++ {
			r := rune(data[i])
			if r < 32 || r > 126 {
				r = '.'
			}
			asciiStr += string(r)
		}
		scr.Write(vv.X1+12+50, vv.Y1+1+y, vtui.StringToCharInfo(asciiStr, attr))

		currOffset += 16
	}
	vv.eofVisible = currOffset >= vv.backend.Size()
}

func (vv *ViewerView) renderText(scr *vtui.ScreenBuf, width, contentHeight int) {
	attr := vtui.Palette[ColViewerText]
	currOffset := vv.TopOffset
	vv.lineOffsets = vv.lineOffsets[:0]

	for y := 0; y < contentHeight; y++ {
		vv.lineOffsets = append(vv.lineOffsets, currOffset)
		if currOffset >= vv.backend.Size() {
			break
		}

		// Read a generous chunk to handle wrapping
		data, _ := vv.backend.ReadAt(currOffset, width*4)
		if len(data) == 0 {
			break
		}

		lineLen := 0
		textLen := 0
		visualWidth := 0
		foundNewline := false

		for lineLen < len(data) {
			r, size := utf8.DecodeRune(data[lineLen:])
			if r == '\n' {
				lineLen += size
				foundNewline = true
				break
			}
			if r == '\r' {
				lineLen += size
				continue
			}

			rw := runewidth.RuneWidth(r)
			if vv.WrapMode && visualWidth+rw > width {
				// Wrap occurred
				break
			}
			visualWidth += rw
			lineLen += size
			textLen = lineLen
		}

		scr.Write(vv.X1, vv.Y1+1+y, vtui.StringToCharInfo(string(data[:textLen]), attr))
		currOffset += int64(lineLen)

		if !foundNewline && !vv.WrapMode {
			// In no-wrap mode, we must consume until the actual newline
			tempOff := currOffset
			for {
				b, err := vv.backend.ReadAt(tempOff, 1024)
				if err != nil || len(b) == 0 { break }
				found := false
				for i, char := range b {
					if char == '\n' {
						tempOff += int64(i + 1)
						found = true
						break
					}
				}
				if found { break }
				tempOff += int64(len(b))
			}
			currOffset = tempOff
		}
	}
	vv.eofVisible = currOffset >= vv.backend.Size()
}
func (vv *ViewerView) ProcessKey(e *vtinput.InputEvent) bool {
	if !e.KeyDown {
		return false
	}

	ctrl := (e.ControlKeyState & (vtinput.LeftCtrlPressed | vtinput.RightCtrlPressed)) != 0
	if e.VirtualKeyCode == vtinput.VK_TAB && ctrl {
		return false
	}

	//height := int64(vv.Y2 - vv.Y1 + 1)
	step := int64(1)
	if vv.HexMode {
		step = 16
	}

	contentHeight := int64(vv.Y2 - vv.Y1) // height - 1 (status line)

	switch e.VirtualKeyCode {
	case vtinput.VK_ESCAPE, vtinput.VK_F10, vtinput.VK_F3:
		vv.SetExitCode(-1)
		return true

	case vtinput.VK_F2:
		vv.WrapMode = !vv.WrapMode
		return true

	case vtinput.VK_F4:
		vv.HexMode = !vv.HexMode
		if vv.HexMode {
			vv.TopOffset &= ^0xF
		}
		return true

	case vtinput.VK_DOWN:
		if vv.eofVisible {
			return true // Prevent scrolling past End of File
		}
		if vv.HexMode {
			vv.TopOffset += step
		} else if len(vv.lineOffsets) > 1 {
			vv.TopOffset = vv.lineOffsets[1]
		}
		return true

	case vtinput.VK_UP:
		if vv.HexMode {
			vv.TopOffset -= step
		} else {
			vv.TopOffset = vv.backend.FindLineStart(vv.TopOffset - 1)
		}
		if vv.TopOffset < 0 {
			vv.TopOffset = 0
		}
		return true

	case vtinput.VK_NEXT: // PgDn
		if vv.eofVisible {
			return true // Prevent paging past End of File
		}
		if vv.HexMode {
			vv.TopOffset += step * contentHeight
		} else if len(vv.lineOffsets) > 0 {
			vv.TopOffset = vv.lineOffsets[len(vv.lineOffsets)-1]
		}
		return true

	case vtinput.VK_PRIOR: // PgUp
		if vv.HexMode {
			vv.TopOffset -= step * contentHeight
		} else {
			for i := 0; i < int(contentHeight); i++ {
				vv.TopOffset = vv.backend.FindLineStart(vv.TopOffset - 1)
			}
		}
		if vv.TopOffset < 0 {
			vv.TopOffset = 0
		}
		return true

	case vtinput.VK_HOME:
		vv.TopOffset = 0
		return true

	case vtinput.VK_END:
		if vv.HexMode {
			if vv.backend.Size() == 0 {
				vv.TopOffset = 0
			} else {
				lastLineOffset := (vv.backend.Size() - 1) &^ 0xF
				vv.TopOffset = lastLineOffset - (contentHeight-1)*16
				if vv.TopOffset < 0 {
					vv.TopOffset = 0
				}
			}
		} else {
			if vv.backend.Size() == 0 {
				vv.TopOffset = 0
			} else {
				// Estimate a safe starting point a few kilobytes back
				width := vv.X2 - vv.X1 + 1
				chunkSize := contentHeight * int64(width) * 4
				if chunkSize < 4096 { chunkSize = 4096 }

				startOff := vv.backend.Size() - chunkSize
				if startOff < 0 { startOff = 0 }
				startOff = vv.backend.FindLineStart(startOff)

				// Simulate rendering forward to find exact visual line offsets
				data, _ := vv.backend.ReadAt(startOff, int(vv.backend.Size()-startOff))
				var offsets []int64
				currOff := startOff

				for len(data) > 0 {
					offsets = append(offsets, currOff)

					lineLen := 0
					visualWidth := 0
					foundNewline := false

					for lineLen < len(data) {
						r, size := utf8.DecodeRune(data[lineLen:])
						if r == '\n' {
							lineLen += size
							foundNewline = true
							break
						}
						if r == '\r' {
							lineLen += size
							continue
						}

						rw := runewidth.RuneWidth(r)
						if vv.WrapMode && visualWidth+rw > width {
							break
						}
						visualWidth += rw
						lineLen += size
					}

					currOff += int64(lineLen)
					data = data[lineLen:]

					if !foundNewline && !vv.WrapMode {
						// Skip to the next newline in no-wrap mode
						idx := bytes.IndexByte(data, '\n')
						if idx >= 0 {
							currOff += int64(idx + 1)
							data = data[idx+1:]
						} else {
							currOff += int64(len(data))
							data = nil
						}
					}
				}

				if int64(len(offsets)) <= contentHeight {
					vv.TopOffset = startOff
				} else {
					vv.TopOffset = offsets[len(offsets)-int(contentHeight)]
				}
			}
		}
		return true
	}

	return false
}

func (vv *ViewerView) ProcessMouse(e *vtinput.InputEvent) bool {
	if e.Type != vtinput.MouseEventType {
		return false
	}
	if vv.scrollBar != nil && vv.scrollBar.ProcessMouse(e) {
		return true
	}
	if e.WheelDirection != 0 {
		if e.WheelDirection > 0 {
			vv.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_UP})
		} else {
			vv.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
		}
		return true
	}
	return false
}
func (vv *ViewerView) ResizeConsole(w, h int)                 { vv.SetPosition(0, 0, w-1, h-2) }
func (vv *ViewerView) GetKeyLabels() *vtui.KeySet {
	return &vtui.KeySet{
		Normal: vtui.KeyBarLabels{
			Msg("KeyBar.ViewerF1"), Msg("KeyBar.ViewerF2"), Msg("KeyBar.ViewerF3"), Msg("KeyBar.ViewerF4"),
			"", "", Msg("KeyBar.ViewerF7"), "", "", Msg("KeyBar.ViewerF10"),
		},
	}
}

func (vv *ViewerView) GetType() vtui.FrameType { return vtui.TypeUser + 3 }
func (vv *ViewerView) GetTitle() string {
	if vv.path != "" {
		return "View: " + filepath.Base(vv.path)
	}
	return "Viewer"
}
