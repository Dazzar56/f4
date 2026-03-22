package main

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
	"github.com/mattn/go-runewidth"
)

// ViewerView is a high-performance file viewer component.
type ViewerView struct {
	vtui.ScreenObject
	topBar  *ViewerBar
	backend *ViewerBackend
	path    string

	HexMode   bool
	WrapMode  bool
	TopOffset int64 // Current byte offset of the first visible line

	// For Text mode: offsets of lines currently on screen
	lineOffsets []int64
	eofVisible  bool
	done        bool
}

func NewViewerView(path string) (*ViewerView, error) {
	backend, err := NewViewerBackend(path)
	if err != nil {
		return nil, err
	}
	vv := &ViewerView{
		backend:  backend,
		path:     path,
		WrapMode: true,
	}
	vv.topBar = &ViewerBar{vv: vv}
	vv.topBar.SetVisible(true)
	vv.SetCanFocus(true)
	vv.SetFocus(true)
	return vv, nil
}

type ViewerBar struct {
	vtui.Bar
	vv *ViewerView
}

func (vb *ViewerBar) Show(scr *vtui.ScreenBuf) {
	vb.Bar.Show(scr)
	vb.DisplayObject(scr)
}
func (vb *ViewerBar) DisplayObject(scr *vtui.ScreenBuf) {
	if !vb.IsVisible() {
		return
	}
	attr := vtui.Palette[ColViewerStatus]
	vb.DrawBackground(scr, attr)

	percent := 0
	size := vb.vv.backend.Size()
	if size > 0 {
		viewHeightBytes := int64(vb.vv.Y2 - vb.vv.Y1)
		if vb.vv.HexMode {
			viewHeightBytes *= 16
		} else {
			viewHeightBytes *= 80
		}
		if size <= viewHeightBytes {
			percent = 100
		} else {
			denominator := size - viewHeightBytes
			percent = int((vb.vv.TopOffset * 100) / denominator)
		}
		if percent < 0 { percent = 0 }
		if percent > 100 { percent = 100 }
	}

	mode := Msg("Viewer.ModeText")
	if vb.vv.HexMode { mode = Msg("Viewer.ModeHex") }
	status := fmt.Sprintf(" %s │ %s │ %d%% ", vb.vv.path, mode, percent)
	scr.Write(vb.X1, vb.Y1, vtui.StringToCharInfo(status, attr))
}

func (vv *ViewerView) SetPosition(x1, y1, x2, y2 int) {
	vv.ScreenObject.SetPosition(x1, y1, x2, y2)
	if vv.topBar != nil {
		vv.topBar.SetPosition(x1, y1, x2, y1)
	}
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
		}

		scr.Write(vv.X1, vv.Y1+1+y, vtui.StringToCharInfo(string(data[:lineLen]), attr))
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

	//height := int64(vv.Y2 - vv.Y1 + 1)
	step := int64(1)
	if vv.HexMode {
		step = 16
	}

	contentHeight := int64(vv.Y2 - vv.Y1) // height - 1 (status line)

	switch e.VirtualKeyCode {
	case vtinput.VK_ESCAPE, vtinput.VK_F10, vtinput.VK_F3:
		vv.done = true
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

func (vv *ViewerView) ProcessMouse(e *vtinput.InputEvent) bool { return false }
func (vv *ViewerView) ResizeConsole(w, h int)                 { vv.SetPosition(0, 0, w-1, h-2) }
func (vv *ViewerView) GetType() vtui.FrameType               { return vtui.TypeUser + 3 }
func (vv *ViewerView) SetExitCode(c int)                     { vv.done = true }
func (vv *ViewerView) IsDone() bool                          { return vv.done }
func (vv *ViewerView) IsBusy() bool                          { return false }
func (vv *ViewerView) IsModal() bool                         { return false }
func (vv *ViewerView) GetWindowNumber() int                  { return 0 }
func (vv *ViewerView) SetWindowNumber(n int)                 {}
func (vv *ViewerView) RequestFocus() bool                    { return true }
func (vv *ViewerView) Close()                                { vv.done = true }
func (vv *ViewerView) HasShadow() bool                       { return false }
func (vv *ViewerView) GetKeyLabels() *vtui.KeySet {
	return &vtui.KeySet{
		Normal: vtui.KeyBarLabels{
			Msg("KeyBar.ViewerF1"), Msg("KeyBar.ViewerF2"), Msg("KeyBar.ViewerF3"), Msg("KeyBar.ViewerF4"),
			"", "", Msg("KeyBar.ViewerF7"), "", "", Msg("KeyBar.ViewerF10"),
		},
	}
}