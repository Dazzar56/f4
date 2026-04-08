package main

import (
	"sort"
	"sync"
	"unicode/utf8"
	"encoding/base64"

	"github.com/mattn/go-runewidth"
	"strings"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
	"github.com/unxed/vtui/piecetable"
	"github.com/unxed/vtui/textlayout"
)

// StyleChange фиксирует момент смены атрибутов в байтовом потоке лога.
type StyleChange struct {
	Offset int
	Attr   uint64
}

// TerminalView объединяет классическую сетку CharInfo и бесконечный лог.
type TerminalView struct {
	vtui.ScreenObject
	mu sync.Mutex

	// --- Состояние для ANSI Парсера (Grid) ---
	Lines        [][]vtui.CharInfo
	AltLines     [][]vtui.CharInfo
	UseAltScreen bool

	ScrollTop    int
	ScrollBottom int

	Width   int
	Height  int
	CursorX int
	CursorY int

	// Состояние терминала (сохранение координат)
	savedX, savedY       int
	decSavedX, decSavedY int
	Palette              [256]uint32

	// --- Бесконечный лог (History & Reflow) ---
	pt              *piecetable.PieceTable
	li              *piecetable.LineIndex
	engine          *textlayout.WrapEngine
	styles          []StyleChange
	lastAttr        uint64
	lastLineOffset  int // Смещение начала текущей строки в PieceTable

	// Скроллинг истории (визуальный ряд)
	ScrollTopRow int

	Title              string
	Win32InputMode        bool
	BracketedPasteMode    bool
	ApplicationCursorKeys bool
	KittyFlags            int
	KittyFlagsStack       []int

	clipboardChunks []byte
	pty             PtyBackend
}

func NewTerminalView(w, h int) *TerminalView {
	tv := &TerminalView{
		Width:  w,
		Height: h,
	}
	tv.ResetBuffer(w, h)
	return tv
}

func (tv *TerminalView) CloneStateFrom(other *TerminalView) {
	other.mu.Lock()
	defer other.mu.Unlock()
	tv.mu.Lock()
	defer tv.mu.Unlock()

	// 1. Match dimensions and re-allocate grids
	tv.Width = other.Width
	tv.Height = other.Height

	allocGrid := func(src [][]vtui.CharInfo) [][]vtui.CharInfo {
		dst := make([][]vtui.CharInfo, len(src))
		for y := range src {
			dst[y] = make([]vtui.CharInfo, len(src[y]))
			copy(dst[y], src[y])
		}
		return dst
	}
	tv.Lines = allocGrid(other.Lines)
	tv.AltLines = allocGrid(other.AltLines)

	// 2. Deep copy the PieceTable (History)
	bytes, _ := other.pt.Bytes()
	tv.pt = piecetable.New(bytes)

	// 3. Re-initialize indices and engine to point to the NEW pt
	tv.li = piecetable.NewLineIndex()
	tv.li.Rebuild(tv.pt)
	tv.engine = textlayout.NewWrapEngine(tv.pt, tv.li)
	tv.engine.SetWidth(tv.Width)

	// 4. Copy terminal state metadata
	tv.styles = append([]StyleChange(nil), other.styles...)
	tv.lastAttr = other.lastAttr
	tv.lastLineOffset = other.lastLineOffset
	tv.Palette = other.Palette
	tv.CursorX, tv.CursorY = other.CursorX, other.CursorY
	tv.UseAltScreen = other.UseAltScreen
	tv.ScrollTop, tv.ScrollBottom = other.ScrollTop, other.ScrollBottom
	tv.KittyFlags = other.KittyFlags
	tv.KittyFlagsStack = append([]int(nil), other.KittyFlagsStack...)
	tv.pty = other.pty

	// 5. CRITICAL: Wipe the current active line to avoid duplicate prompt.
	// The parent's prompt is already in the copied PieceTable and Lines grid.
	// We remove it so the new shell's prompt has a clean space to print into.
	if tv.lastLineOffset < tv.pt.Size() {
		tv.pt.Delete(tv.lastLineOffset, tv.pt.Size()-tv.lastLineOffset)
		tv.li.Rebuild(tv.pt)
		tv.engine.InvalidateFrom(tv.li.LineCount() - 1)
	}

	// Clear the active visual row and reset horizontal cursor
	if tv.CursorY >= 0 && tv.CursorY < len(tv.Lines) {
		for x := range tv.Lines[tv.CursorY] {
			tv.Lines[tv.CursorY][x] = vtui.CharInfo{Char: ' ', Attributes: DefaultTermAttr}
		}
	}
	tv.CursorX = 0
}

func (tv *TerminalView) ResetBuffer(w, h int) {
	vtui.DebugLog("TERM: ResetBuffer to %dx%d", w, h)
	tv.mu.Lock()
	defer tv.mu.Unlock()

	// Инициализация PieceTable (только один раз)
	if tv.pt == nil {
		tv.pt = piecetable.New([]byte{})
		tv.li = piecetable.NewLineIndex()
		tv.engine = textlayout.NewWrapEngine(tv.pt, tv.li)
		tv.styles = []StyleChange{{0, DefaultTermAttr}}
		tv.lastAttr = DefaultTermAttr
		tv.lastLineOffset = 0
	}
	tv.engine.SetWidth(w)

	// Создание сеток (Grid)
	makeBuf := func() [][]vtui.CharInfo {
		b := make([][]vtui.CharInfo, h)
		for i := range b {
			b[i] = make([]vtui.CharInfo, w)
			for j := range b[i] {
				b[i][j] = vtui.CharInfo{Char: ' ', Attributes: DefaultTermAttr}
			}
		}
		return b
	}

	tv.Lines = makeBuf()
	tv.AltLines = makeBuf()

	// Сброс параметров прокрутки и курсора
	tv.Width, tv.Height = w, h
	tv.ScrollTop = 0
	tv.ScrollBottom = h - 1
	tv.CursorX = 0
	tv.CursorY = h - 1

	// Палитра по умолчанию (ANSI order)
	copy(tv.Palette[:], vtui.XTerm256Palette[:])
	tv.Palette[0] = far2lPalette[0] // Black
	tv.Palette[1] = far2lPalette[4] // Red
	tv.Palette[2] = far2lPalette[2] // Green
	tv.Palette[3] = far2lPalette[6] // Yellow
	tv.Palette[4] = far2lPalette[1] // Blue
	tv.Palette[5] = far2lPalette[5] // Magenta
	tv.Palette[6] = far2lPalette[3] // Cyan
	tv.Palette[7] = far2lPalette[7] // White
	for i := 0; i < 8; i++ {
		winIdx := []int{0, 4, 2, 6, 1, 5, 3, 7}[i]
		tv.Palette[i+8] = far2lPalette[winIdx+8]
	}
}

func (tv *TerminalView) getBuffer() [][]vtui.CharInfo {
	if tv.UseAltScreen {
		return tv.AltLines
	}
	return tv.Lines
}

func (tv *TerminalView) PutChar(r rune, attr uint64) {
	// vtui.DebugLog("TERM: PutChar %q (U+%04X) at (%d,%d)", r, r, tv.CursorX, tv.CursorY)
	tv.mu.Lock()
	defer tv.mu.Unlock()

	// 1. Запись в бесконечный лог (если не AltScreen)
	if !tv.UseAltScreen {
		if r == '\n' {
			offset := tv.pt.Size()
			tv.pt.Insert(offset, []byte("\n"))
			tv.li.UpdateAfterInsert(offset, []byte("\n"))
			tv.lastLineOffset = tv.pt.Size()
			tv.engine.InvalidateFrom(tv.li.LineCount() - 2)
		} else if r >= 0x20 {
			// Если мы в начале строки и в логе уже что-то есть для этой строки —
			// вероятно, это перерисовка промпта оболочкой. Удаляем старое.
			if tv.CursorX == 0 && tv.pt.Size() > tv.lastLineOffset {
				tv.pt.Delete(tv.lastLineOffset, tv.pt.Size()-tv.lastLineOffset)
				tv.li.UpdateAfterDelete(tv.lastLineOffset, tv.pt.Size()-tv.lastLineOffset)
				// Откатываем стили
				for i := len(tv.styles) - 1; i >= 0; i-- {
					if tv.styles[i].Offset > tv.lastLineOffset {
						tv.styles = tv.styles[:i]
					} else {
						tv.lastAttr = tv.styles[i].Attr
						break
					}
				}
			}

			offset := tv.pt.Size()
			if attr != tv.lastAttr {
				tv.styles = append(tv.styles, StyleChange{offset, attr})
				tv.lastAttr = attr
			}
			var buf [4]byte
			n := utf8.EncodeRune(buf[:], r)
			tv.pt.Insert(offset, buf[:n])
			tv.li.UpdateAfterInsert(offset, buf[:n])
			tv.engine.InvalidateFrom(tv.li.LineCount() - 1)
		}
	}

	// 2. Обработка в текущей сетке (Grid)
	if r == '\r' {
		tv.CursorX = 0
		return
	}
	if r == '\n' {
		tv.newline()
		return
	}
	if r == '\b' {
		if tv.CursorX > 0 {
			tv.CursorX--
		}
		return
	}
	if r == '\t' {
		tv.CursorX = (tv.CursorX + 8) & ^7
		return
	}
	if r < 0x20 {
		return
	}

	w := runewidth.RuneWidth(r)
	if w <= 0 {
		w = 1
	}

	buf := tv.getBuffer()
	// If the character is too wide to fit in the current line, wrap first
	if tv.CursorX+w > tv.Width {
		tv.newline()
		buf = tv.getBuffer()
	}

	if tv.CursorY >= 0 && tv.CursorY < len(buf) && tv.CursorX >= 0 && tv.CursorX+w <= tv.Width {
		// Write the actual character
		buf[tv.CursorY][tv.CursorX] = vtui.CharInfo{Char: uint64(r), Attributes: attr}
		// Fill subsequent cells for wide characters
		for i := 1; i < w; i++ {
			buf[tv.CursorY][tv.CursorX+i] = vtui.CharInfo{Char: vtui.WideCharFiller, Attributes: attr}
		}
		tv.CursorX += w
	}
}

func (tv *TerminalView) newline() {
	// vtui.DebugLog("TERM: newline at Y=%d (ScrollBottom=%d)", tv.CursorY, tv.ScrollBottom)
	tv.CursorX = 0
	tv.CursorY++
	if tv.CursorY > tv.ScrollBottom {
		tv.scrollUp(tv.ScrollTop, tv.ScrollBottom, 1)
		tv.CursorY = tv.ScrollBottom
	}
}

func (tv *TerminalView) scrollUp(top, bottom, n int) {
	vtui.DebugLog("TERM: scrollUp [Top:%d Bottom:%d N:%d]", top, bottom, n)
	buf := tv.getBuffer()
	if top < 0 { top = 0 }
	if bottom >= len(buf) { bottom = len(buf) - 1 }
	if top >= bottom { return }

	for i := 0; i < n; i++ {
		copy(buf[top:bottom], buf[top+1:bottom+1])
		buf[bottom] = make([]vtui.CharInfo, tv.Width)
		for j := range buf[bottom] {
			buf[bottom][j] = vtui.CharInfo{Char: ' ', Attributes: DefaultTermAttr}
		}
	}
}
func (tv *TerminalView) scrollDown(top, bottom, n int) {
	vtui.DebugLog("TERM: scrollDown [Top:%d Bottom:%d N:%d]", top, bottom, n)
	buf := tv.getBuffer()
	if top < 0 { top = 0 }
	if bottom >= len(buf) { bottom = len(buf) - 1 }
	if top >= bottom { return }

	for i := 0; i < n; i++ {
		// To move lines DOWN, we must copy from bottom to top to avoid overwriting
		for y := bottom; y > top; y-- {
			buf[y] = buf[y-1]
		}
		// Clear the newly inserted top line
		buf[top] = make([]vtui.CharInfo, tv.Width)
		for j := range buf[top] {
			buf[top][j] = vtui.CharInfo{Char: ' ', Attributes: DefaultTermAttr}
		}
	}
}

func (tv *TerminalView) DeleteCharacters(n int, attr uint64) {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	buf := tv.getBuffer()
	if tv.CursorY < 0 || tv.CursorY >= len(buf) { return }
	line := buf[tv.CursorY]
	if tv.CursorX < 0 || tv.CursorX >= tv.Width { return }

	if tv.CursorX+n < tv.Width {
		copy(line[tv.CursorX:], line[tv.CursorX+n:])
	}

	clearStart := tv.Width - n
	if clearStart < tv.CursorX { clearStart = tv.CursorX }
	for i := clearStart; i < tv.Width; i++ {
		line[i] = vtui.CharInfo{Char: ' ', Attributes: attr}
	}
}

func (tv *TerminalView) InsertBlankCharacters(n int, attr uint64) {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	buf := tv.getBuffer()
	if tv.CursorY < 0 || tv.CursorY >= len(buf) { return }
	line := buf[tv.CursorY]
	if tv.CursorX < 0 || tv.CursorX >= tv.Width { return }

	if tv.CursorX+n < tv.Width {
		copy(line[tv.CursorX+n:], line[tv.CursorX:])
	}

	end := tv.CursorX + n
	if end > tv.Width { end = tv.Width }
	for i := tv.CursorX; i < end; i++ {
		line[i] = vtui.CharInfo{Char: ' ', Attributes: attr}
	}
}

func (tv *TerminalView) SetCursor(x, y int) {
	// vtui.DebugLog("TERM: SetCursor to (%d,%d)", x, y)
	tv.mu.Lock()
	defer tv.mu.Unlock()
	if x < 0 { x = 0 }
	if x >= tv.Width { x = tv.Width - 1 }
	if y < 0 { y = 0 }
	if y >= tv.Height { y = tv.Height - 1 }
	tv.CursorX, tv.CursorY = x, y
}

func (tv *TerminalView) SaveCursor() {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	tv.decSavedX, tv.decSavedY = tv.CursorX, tv.CursorY
}

func (tv *TerminalView) RestoreCursor() {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	tv.CursorX, tv.CursorY = tv.decSavedX, tv.decSavedY
}

func (tv *TerminalView) RepeatLastChar(n int, r rune, attr uint64) {
	for i := 0; i < n; i++ {
		tv.PutChar(r, attr)
	}
}

func (tv *TerminalView) EraseCharacter(n int, attr uint64) {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	buf := tv.getBuffer()
	if tv.CursorY < 0 || tv.CursorY >= len(buf) { return }
	line := buf[tv.CursorY]
	for i := 0; i < n && (tv.CursorX+i) < tv.Width; i++ {
		line[tv.CursorX+i] = vtui.CharInfo{Char: ' ', Attributes: attr}
	}
}

func (tv *TerminalView) EraseDisplay(mode int, attr uint64) {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	buf := tv.getBuffer()
	if mode == 2 {
		for i := range buf {
			for j := range buf[i] {
				buf[i][j] = vtui.CharInfo{Char: ' ', Attributes: attr}
			}
		}
	} else if mode == 0 {
		if tv.CursorY >= 0 && tv.CursorY < tv.Height {
			line := buf[tv.CursorY]
			for j := (tv.CursorX); j < tv.Width; j++ {
				if j >= 0 { line[j] = vtui.CharInfo{Char: ' ', Attributes: attr} }
			}
		}
		for i := tv.CursorY + 1; i < tv.Height; i++ {
			if i >= 0 {
				for j := range buf[i] {
					buf[i][j] = vtui.CharInfo{Char: ' ', Attributes: attr}
				}
			}
		}
	}
}

func (tv *TerminalView) EraseLine(mode int, attr uint64) {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	buf := tv.getBuffer()
	if tv.CursorY < 0 || tv.CursorY >= tv.Height { return }
	line := buf[tv.CursorY]
	start, end := 0, tv.Width
	if mode == 0 {
		start = tv.CursorX
	} else if mode == 1 {
		end = tv.CursorX + 1
	}
	for j := start; j < end; j++ {
		if j >= 0 && j < tv.Width {
			line[j] = vtui.CharInfo{Char: ' ', Attributes: attr}
		}
	}
}

func (tv *TerminalView) SetAltScreen(enable bool) {
	vtui.DebugLog("TERM: SetAltScreen %v", enable)
	tv.mu.Lock()
	defer tv.mu.Unlock()
	if tv.UseAltScreen == enable { return }
	if enable {
		tv.savedX, tv.savedY = tv.CursorX, tv.CursorY
		tv.CursorX, tv.CursorY = 0, 0
	} else {
		tv.CursorX, tv.CursorY = tv.savedX, tv.savedY
	}
	tv.UseAltScreen = enable
}

func (tv *TerminalView) getAttrAt(offset int) uint64 {
	idx := sort.Search(len(tv.styles), func(i int) bool {
		return tv.styles[i].Offset > offset
	})
	if idx > 0 {
		return tv.styles[idx-1].Attr
	}
	return DefaultTermAttr
}

func (tv *TerminalView) Show(scr *vtui.ScreenBuf) {
	tv.ScreenObject.Show(scr)

	scr.ActivePalette = &tv.Palette
	// Terminal content must always be rendered without Early Binding
	// to allow the host terminal to use its native indexed palette.
	prevOverlay := scr.OverlayMode
	scr.SetOverlayMode(false)
	defer func() { scr.SetOverlayMode(prevOverlay) }()

	tv.mu.Lock()
	defer tv.mu.Unlock()

	// Очищаем всю область терминала черным цветом
	scr.FillRect(tv.X1, tv.Y1, tv.X1+tv.Width-1, tv.Y1+tv.Height-1, ' ', DefaultTermAttr)

	buf := tv.Lines
	if tv.UseAltScreen {
		buf = tv.AltLines
	}

	for y, line := range buf {
		if y >= tv.Height { break }
		scr.Write(tv.X1, tv.Y1+y, line)
	}

	if tv.IsVisible() {
		scr.SetCursorPos(tv.X1+tv.CursorX, tv.Y1+tv.CursorY)
		scr.SetCursorVisible(true)
	}
}

func (tv *TerminalView) Resize(w, h int) {
	if tv.Width == w && tv.Height == h {
		return
	}
	tv.ResetBuffer(w, h)
}
func (tv *TerminalView) IsModal() bool        { return false }
func (tv *TerminalView) RequestFocus() bool   { return true }
func (tv *TerminalView) Close()               {}
func (tv *TerminalView) GetWindowNumber() int { return 0 }
func (tv *TerminalView) SetWindowNumber(n int) {}

func (tv *TerminalView) HandleFar2lAPC(s string) {
	vtui.DebugLog("TERM_APC: Incoming Far2l sequence: %q", s)
	if s == "far2l1" {
		if tv.pty != nil { tv.pty.Write([]byte("\x1b_far2lok\x07")) }
	} else if s == "far2l0" {
		// Disable
	} else if s == "far2lok" {
		// Acknowledgement from the host terminal. This is not for the internal shell to process visually.
		// Consume and do nothing.
	} else if strings.HasPrefix(s, "far2l:") {
		b64 := s[6:]
		if m := len(b64) % 4; m != 0 {
			b64 += strings.Repeat("=", 4-m)
		}
		decoded, _ := base64.StdEncoding.DecodeString(b64)
		tv.ProcessFar2lInteract(decoded)
	}
}

func (tv *TerminalView) ProcessFar2lInteract(data []byte) {
	stk := (*vtinput.Far2lStack)(&data)
	id := stk.PopU8()
	cmd := stk.PopU8()
	vtui.DebugLog("TERM_APC: ProcessFar2lInteract: cmd=%c, id=%d", cmd, id)

	reply := vtinput.Far2lStack{}

	switch cmd {
	case 'c': // Clipboard
		sub := stk.PopU8()
		vtui.DebugLog("TERM_APC: Clipboard sub-command: %c", sub)
		switch sub {
		case 'o':
			clientID := stk.PopString()
			auth := 0
			if vtui.GlobalClipboardAccessManager != nil {
				auth = vtui.GlobalClipboardAccessManager.Authorize(clientID)
			}
			reply.PushU64(2) // FARTTY_FEATCLIP_CHUNKED_SET
			reply.PushU8(uint8(auth))
		case 'c':
			tv.clipboardChunks = nil
			reply.PushU8(1)
		case 'e':
			vtui.SetClipboard("")
			tv.clipboardChunks = nil
			reply.PushU8(1)
		case 'a':
			_ = stk.PopU32() // fmt
			reply.PushU8(1)
		case 'S':
			size := stk.PopU16()
			if size == 0 {
				tv.clipboardChunks = nil
			} else {
				chunk := stk.PopBytes(int(size) << 8)
				tv.clipboardChunks = append(tv.clipboardChunks, chunk...)
			}
		case 's':
			_ = stk.PopU32() // fmt
			len := stk.PopU32()
			textBytes := stk.PopBytes(int(len))
			fullData := append(tv.clipboardChunks, textBytes...)
			tv.clipboardChunks = nil
			vtui.SetClipboard(string(fullData))
			reply.PushU8(1)
		case 'g':
			_ = stk.PopU32() // fmt
			clipData := vtui.GetClipboard()
			reply.PushU32(uint32(len(clipData)))
			reply.PushBytes([]byte(clipData))
			reply.PushU32(uint32(len(clipData)))
		case 'i':
			_ = stk.PopU32()
			reply.PushU64(0)
		case 'r':
			_ = stk.PopString()
			reply.PushU32(0xC000)
		}
	case 'w': // Window size
		reply.PushU16(uint16(tv.Height))
		reply.PushU16(uint16(tv.Width))
	case 'h': // Cursor height
		_ = stk.PopU8()
	case 'n': // Desktop notification
		text := stk.PopString()
		title := stk.PopString()
		vtui.FrameManager.PostTask(func() {
			vtui.ShowMessage(" "+title+" ", text, []string{"&Ok"})
		})
	case 'f': // FKey titles
		for i := 0; i < 12; i++ {
			state := stk.PopU8()
			if state != 0 {
				_ = stk.PopString() // Just pop, we can ignore it for now or implement KeyBar update
			}
		}
		reply.PushU8(1)
	case 'x': // Extra features
		feats := stk.PopU64()
		if feats&2 != 0 { // FARTTY_FEAT_TERMINAL_SIZE
			tv.SendFar2lTerminalSize()
		}
	case 'p': // Palette info
		reply.PushU8(0)  // reserved
		reply.PushU8(24) // bits
	case 'i': // Image operations (stub)
		_ = stk.PopU8() // subcmd
		reply.PushU8(0)
	}

	if len(reply) > 0 || id != 0 {
		reply.PushU8(id)
		b64 := base64.StdEncoding.EncodeToString(reply)
		if tv.pty != nil {
			tv.pty.Write([]byte("\x1b_far2l" + b64 + "\x07"))
		}
	}
}

func (tv *TerminalView) SendFar2lTerminalSize() {
	stk := vtinput.Far2lStack{}
	stk.PushU16(uint16(tv.Height))
	stk.PushU16(uint16(tv.Width))
	stk.PushU8('S')
	b64 := base64.StdEncoding.EncodeToString(stk)
	if tv.pty != nil {
		tv.pty.Write([]byte("\x1b_f2l:" + b64 + "\x07"))
	}
}
