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
	"github.com/unxed/f4/piecetable"
	"github.com/unxed/f4/textlayout"
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
	pendingLog      []byte
	pendingAttr     uint64

	Muted           bool
	lastCharWasCR   bool
	authCache       map[string]int

	OnTitleChange func(string)
}

func NewTerminalView(w, h int) *TerminalView {
	tv := &TerminalView{
		Width:     w,
		Height:    h,
		authCache: make(map[string]int),
	}
	tv.ResetBuffer(w, h)
	return tv
}

func (tv *TerminalView) CloneStateFrom(other *TerminalView) {
	other.FlushLog()
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
	tv.authCache = make(map[string]int)
	for k, v := range other.authCache {
		tv.authCache[k] = v
	}
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
	tv.lastCharWasCR = true
	tv.pendingLog = tv.pendingLog[:0]
	tv.pendingAttr = DefaultTermAttr

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


func (tv *TerminalView) SetMuted(muted bool) {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	tv.Muted = muted
}
func (tv *TerminalView) PrintCleanCommand(cleanCmd string) {
	for _, r := range cleanCmd {
		tv.PutChar(r, DefaultTermAttr)
	}
	tv.PutChar('\r', DefaultTermAttr)
	tv.PutChar('\n', DefaultTermAttr)
	tv.FlushLog()
}
func (tv *TerminalView) FlushLog() {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	tv.flushLogUnsafe()
}

func (tv *TerminalView) flushLogUnsafe() {
	if len(tv.pendingLog) == 0 {
		return
	}
	offset := tv.pt.Size()
	if tv.pendingAttr != tv.lastAttr {
		tv.styles = append(tv.styles, StyleChange{Offset: offset, Attr: tv.pendingAttr})
		tv.lastAttr = tv.pendingAttr
	}
	tv.pt.Insert(offset, tv.pendingLog)
	tv.li.UpdateAfterInsert(offset, tv.pendingLog)
	tv.engine.InvalidateFrom(tv.li.LineCount() - 2)

	tv.pendingLog = tv.pendingLog[:0]
}

func (tv *TerminalView) PutChar(r rune, attr uint64) {
	tv.mu.Lock()
	defer tv.mu.Unlock()

	if tv.Muted { return }

	// 1. Запись в бесконечный лог (если не AltScreen)
	if !tv.UseAltScreen {
		if r == '\n' {
			tv.pendingLog = append(tv.pendingLog, '\n')
			tv.lastLineOffset = tv.pt.Size() + len(tv.pendingLog)
			tv.lastCharWasCR = false
		} else if r >= 0x20 {
			if tv.lastCharWasCR && tv.CursorX == 0 && (tv.pt.Size()+len(tv.pendingLog)) > tv.lastLineOffset {
				tv.flushLogUnsafe()
				tv.pt.Delete(tv.lastLineOffset, tv.pt.Size()-tv.lastLineOffset)
				tv.li.UpdateAfterDelete(tv.lastLineOffset, tv.pt.Size()-tv.lastLineOffset)
				for i := len(tv.styles) - 1; i >= 0; i-- {
					if tv.styles[i].Offset > tv.lastLineOffset {
						tv.styles = tv.styles[:i]
					} else {
						tv.lastAttr = tv.styles[i].Attr
						tv.pendingAttr = tv.lastAttr
						break
					}
				}
			}
			tv.lastCharWasCR = false

			if attr != tv.pendingAttr {
				tv.flushLogUnsafe()
				tv.pendingAttr = attr
			}
			var buf [4]byte
			n := utf8.EncodeRune(buf[:], r)
			tv.pendingLog = append(tv.pendingLog, buf[:n]...)

			if len(tv.pendingLog) > 4096 {
				tv.flushLogUnsafe()
			}
		}
	}

	// 2. Обработка в текущей сетке (Grid)
	if r == '\r' {
		tv.CursorX = 0
		tv.lastCharWasCR = true
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
	if tv.CursorX+w > tv.Width {
		tv.newline()
		buf = tv.getBuffer()
	}

	if tv.CursorY >= 0 && tv.CursorY < len(buf) && tv.CursorX >= 0 && tv.CursorX+w <= tv.Width {
		buf[tv.CursorY][tv.CursorX] = vtui.CharInfo{Char: uint64(r), Attributes: attr}
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

func (tv *TerminalView) ScrollUp(top, bottom, n int) {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	if tv.Muted { return }
	tv.scrollUp(top, bottom, n)
}

func (tv *TerminalView) scrollUp(top, bottom, n int) {
	// Muted to avoid log flood during heavy outputs
	// vtui.DebugLog("TERM: scrollUp [Top:%d Bottom:%d N:%d]", top, bottom, n)
	buf := tv.getBuffer()
	if top < 0 { top = 0 }
	if bottom >= len(buf) { bottom = len(buf) - 1 }
	if top >= bottom { return }

	for i := 0; i < n; i++ {
		recycledLine := buf[top]
		copy(buf[top:bottom], buf[top+1:bottom+1])
		buf[bottom] = recycledLine
		for j := range buf[bottom] {
			buf[bottom][j] = vtui.CharInfo{Char: ' ', Attributes: DefaultTermAttr}
		}
	}
}
func (tv *TerminalView) ScrollDown(top, bottom, n int) {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	if tv.Muted { return }
	tv.scrollDown(top, bottom, n)
}

func (tv *TerminalView) scrollDown(top, bottom, n int) {
	// Muted to avoid log flood during heavy outputs
	// vtui.DebugLog("TERM: scrollDown [Top:%d Bottom:%d N:%d]", top, bottom, n)
	buf := tv.getBuffer()
	if top < 0 { top = 0 }
	if bottom >= len(buf) { bottom = len(buf) - 1 }
	if top >= bottom { return }

	for i := 0; i < n; i++ {
		recycledLine := buf[bottom]
		// To move lines DOWN, we must copy from bottom to top to avoid overwriting
		for y := bottom; y > top; y-- {
			buf[y] = buf[y-1]
		}
		// Clear the newly inserted top line
		buf[top] = recycledLine
		for j := range buf[top] {
			buf[top][j] = vtui.CharInfo{Char: ' ', Attributes: DefaultTermAttr}
		}
	}
}

func (tv *TerminalView) DeleteCharacters(n int, attr uint64) {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	if tv.Muted { return }
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
	if tv.Muted { return }
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
	if tv.Muted { return }
	if x < 0 { x = 0 }
	if x >= tv.Width { x = tv.Width - 1 }
	if y < 0 { y = 0 }
	if y >= tv.Height { y = tv.Height - 1 }
	tv.CursorX, tv.CursorY = x, y
	if x == 0 {
		tv.lastCharWasCR = true
	}
}

func (tv *TerminalView) SaveCursor() {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	if tv.Muted { return }
	tv.decSavedX, tv.decSavedY = tv.CursorX, tv.CursorY
}

func (tv *TerminalView) RestoreCursor() {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	if tv.Muted { return }
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
	if tv.Muted { return }
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
	if tv.Muted { return }
	buf := tv.getBuffer()

	// При очистке экрана утилитами вроде `clear` мы пушим пустые строки в лог,
	// чтобы старый текст ушел в "scrollback" историю и не всплыл на экране при ресайзе.
	if (mode == 2 || mode == 3) && !tv.UseAltScreen {
		for i := 0; i < tv.Height; i++ {
			tv.pendingLog = append(tv.pendingLog, '\n')
		}
		tv.flushLogUnsafe()
		tv.lastLineOffset = tv.pt.Size()
	}

	if mode == 2 {
		tv.CursorX = 0
		tv.CursorY = 0
		tv.lastCharWasCR = true
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
	if tv.Muted { return }
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

	if tv.IsVisible() && tv.IsFocused() {
		scr.SetCursorPos(tv.X1+tv.CursorX, tv.Y1+tv.CursorY)
		scr.SetCursorVisible(true)
	}
}

func (tv *TerminalView) Resize(w, h int) {
	if tv.Width == w && tv.Height == h {
		return
	}

	tv.mu.Lock()
	defer tv.mu.Unlock()

	// Скидываем недописанный лог, чтобы получить финальную структуру
	tv.flushLogUnsafe()

	// Обновляем ширину движка для пересчета визуальных фрагментов (Reflow)
	tv.engine.SetWidth(w)

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

	newLines := makeBuf()
	newAltLines := makeBuf()

	// Сохраняем содержимое AltScreen (для TUI приложений типа nano/mc).
	minH := h
	if tv.Height < minH { minH = tv.Height }
	minW := w
	if tv.Width < minW { minW = tv.Width }

	for y := 0; y < minH; y++ {
		copy(newAltLines[y][:minW], tv.AltLines[y][:minW])
	}

	// Восстанавливаем Primary Screen из бесконечного лога (PieceTable)
	totalRows := tv.engine.GetTotalVisualRows()
	
	rowsToDraw := totalRows
	if rowsToDraw > h {
		rowsToDraw = h
	}

	startRow := totalRows - rowsToDraw
	startY := h - rowsToDraw // Всегда прижимаем текст к низу экрана, чтобы prompt шелла совпадал с cmdLine

	gridY := startY
	for vRow := startRow; vRow < totalRows; vRow++ {
		logIdx, fragIdx := tv.engine.GetLogLineAtVisualRow(vRow)
		frags := tv.engine.GetFragments(logIdx)
		if fragIdx >= len(frags) { continue }
		frag := frags[fragIdx]

		data, _ := tv.pt.GetRange(frag.ByteOffsetStart, frag.ByteOffsetEnd-frag.ByteOffsetStart)

		gridX := 0
		currByte := frag.ByteOffsetStart

		for len(data) > 0 {
			r, size := utf8.DecodeRune(data)
			data = data[size:]

			attr := DefaultTermAttr
			idx := sort.Search(len(tv.styles), func(i int) bool {
				return tv.styles[i].Offset > currByte
			})
			if idx > 0 {
				attr = tv.styles[idx-1].Attr
			}

			currByte += size

			// Пропускаем символы перевода строк, так как сетка двумерная
			if r == '\n' || r == '\r' {
				continue
			}

			rw := 1
			if r >= 0x7F {
				rw = runewidth.RuneWidth(r)
			}
			if rw <= 0 { rw = 1 }

			// Защита от переполнения новой ширины
			if gridX+rw > w {
				break
			}

			newLines[gridY][gridX] = vtui.CharInfo{Char: uint64(r), Attributes: attr}
			for i := 1; i < rw; i++ {
				newLines[gridY][gridX+i] = vtui.CharInfo{Char: vtui.WideCharFiller, Attributes: attr}
			}
			gridX += rw
		}
		gridY++
	}

	// Вычисляем новую позицию курсора. В f4 терминал всегда прижат к низу.
	newCursorY := h - 1
	if newCursorY < 0 { newCursorY = 0 }
	
	newCursorX := 0
	if totalRows > 0 {
		lastLogIdx, lastFragIdx := tv.engine.GetLogLineAtVisualRow(totalRows - 1)
		lastFrags := tv.engine.GetFragments(lastLogIdx)
		if lastFragIdx < len(lastFrags) {
			lastFrag := lastFrags[lastFragIdx]
			newCursorX = lastFrag.VisualWidth
		}
	}

	tv.Lines = newLines
	tv.AltLines = newAltLines
	tv.Width = w
	tv.Height = h
	tv.ScrollTop = 0
	tv.ScrollBottom = h - 1

	if !tv.UseAltScreen {
		tv.CursorX = newCursorX
		if tv.CursorX >= w { tv.CursorX = w - 1 }
		tv.CursorY = newCursorY
	} else {
		if tv.CursorX >= w { tv.CursorX = w - 1 }
		if tv.CursorY >= h { tv.CursorY = h - 1 }
	}
	tv.lastCharWasCR = (tv.CursorX == 0)
}
func (tv *TerminalView) IsModal() bool        { return false }
func (tv *TerminalView) RequestFocus() bool   { return true }
func (tv *TerminalView) Close()               {}
func (tv *TerminalView) GetWindowNumber() int { return 0 }
func (tv *TerminalView) SetWindowNumber(n int) {}

func (tv *TerminalView) HandleFar2lAPC(s string) {
	vtui.DebugLog("TERM_APC: Incoming Far2l sequence: %q", s)
	// Robustness: skip any garbage before the actual marker
	idx := strings.Index(s, "far2l")
	if idx == -1 {
		return
	}
	s = s[idx:]

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
		if len(decoded) > 0 {
			tv.ProcessFar2lInteract(decoded)
		}
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
			auth, cached := tv.authCache[clientID]
			if !cached {
				if vtui.GlobalClipboardAccessManager != nil {
					auth = vtui.GlobalClipboardAccessManager.Authorize(clientID)
					// Only cache if the user didn't explicitly "Reject" (0)
					// to allow them to change their mind on the next attempt.
					if auth != 0 {
						tv.authCache[clientID] = auth
					}
				}
			}
			respAuth := auth
			if auth == -1 {
				respAuth = 1 // Tell child success, we'll handle it locally
			}
			reply.PushU64(2) // FARTTY_FEATCLIP_CHUNKED_SET
			reply.PushU8(uint8(respAuth))
		case 'c':
			tv.clipboardChunks = nil
			reply.PushU8(1)
		case 'e':
			if !vtui.SetOSClipboard("") {
				vtui.SetClipboard("")
			}
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
			if !vtui.SetOSClipboard(string(fullData)) {
				vtui.SetClipboard(string(fullData))
			}
			reply.PushU8(1)
		case 'g':
			_ = stk.PopU32() // fmt
			clipData := vtui.GetOSClipboard()
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
