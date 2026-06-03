package main

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

func init() {
	vtui.SetDefaultPalette()
	SetDefaultF4Palette()
}

func TestTerminalView_SaveRestoreCursor(t *testing.T) {
	tv := NewTerminalView(80, 24)
	defer tv.Close()

	// Set a specific cursor position
	tv.SetCursor(42, 12)

	// Save it
	tv.SaveCursor()

	// Move cursor somewhere else
	tv.SetCursor(0, 0)
	if tv.CursorX != 0 || tv.CursorY != 0 {
		t.Fatal("Failed to move cursor")
	}

	// Restore and verify
	tv.RestoreCursor()
	if tv.CursorX != 42 || tv.CursorY != 12 {
		t.Errorf("Expected restored cursor at (42, 12), got (%d, %d)", tv.CursorX, tv.CursorY)
	}
}
func TestTerminalView_HandleFar2lAPC(t *testing.T) {
	tv := NewTerminalView(80, 24)
	defer tv.Close()
	pty := &mockPty{}
	tv.pty = pty

	// Simulate far2l1 (enable)
	tv.HandleFar2lAPC("far2l1")
	if string(pty.written) != "\x1b_far2lok\x07" {
		t.Errorf("Expected far2lok response, got %q", string(pty.written))
	}

	// Simulate far2l0 (disable)
	tv.HandleFar2lAPC("far2l0") // Should not panic or write anything

	// Simulate window size request via f2l
	stk := vtinput.Far2lStack{}
	stk.PushU8('w') // window size
	stk.PushU8(0)   // id
	b64 := base64.StdEncoding.EncodeToString(stk)

	pty.written = nil // reset
	tv.HandleFar2lAPC("far2l:" + b64)

	if len(pty.written) == 0 || !strings.HasPrefix(string(pty.written), "\x1b_far2l") {
		t.Errorf("Expected window size reply, got %q", string(pty.written))
	}
}
func TestTerminalView_HandleFar2lAPC_Garbage(t *testing.T) {
	tv := NewTerminalView(80, 24)
	defer tv.Close()
	pty := &mockPty{}
	tv.pty = pty

	// Test robustness: skip garbage before "far2l"
	tv.HandleFar2lAPC("some-random-garbage-far2l1")
	if string(pty.written) != "\x1b_far2lok\x07" {
		t.Errorf("HandleFar2lAPC failed to skip garbage. Got %q", string(pty.written))
	}
}

func TestTerminalView_ProcessFar2lInteract_Clipboard(t *testing.T) {
	tv := NewTerminalView(80, 24)
	defer tv.Close()
	pty := &mockPty{}
	tv.pty = pty

	// 1. Test Clipboard Open (Handshake)
	stk := vtinput.Far2lStack{}
	stk.PushString("client-handshake-id-32-chars-minimum")
	stk.PushU8('o') // open
	stk.PushU8('c') // clipboard
	stk.PushU8(1)   // request id

	tv.ProcessFar2lInteract(stk)
	// Should write something back to PTY (B64 of reply stack)
	if len(pty.written) == 0 {
		t.Fatal("No reply for clipboard open")
	}
	pty.written = nil

	// 2. Test Chunked SetData
	// First chunk: command 'S' expects size * 256 bytes.
	stk = vtinput.Far2lStack{}
	chunkData := make([]byte, 256)
	copy(chunkData, "Part1-")
	stk.PushBytes(chunkData)
	stk.PushU16(1)  // size = 1 block (256 bytes)
	stk.PushU8('S') // Sub-command: Set chunk
	stk.PushU8('c') // Category: Clipboard
	stk.PushU8(2)   // ID
	tv.ProcessFar2lInteract(stk)

	if len(tv.clipboardChunks) != 256 {
		t.Errorf("Clipboard chunk not accumulated. Size: %d", len(tv.clipboardChunks))
	}

	// Finalize set: command 's' expects: data (bytes), len (U32), format (U32)
	stk = vtinput.Far2lStack{}
	stk.PushBytes([]byte("Part2"))
	stk.PushU32(5)  // len
	stk.PushU32(1)  // format (CF_TEXT)
	stk.PushU8('s') // Sub-command: Finalize
	stk.PushU8('c') // Category: Clipboard
	stk.PushU8(3)   // ID
	tv.ProcessFar2lInteract(stk)

	got := vtui.GetClipboard()
	if !strings.HasPrefix(got, "Part1-") || !strings.Contains(got, "Part2") {
		t.Errorf("Chunked clipboard transfer failed. Got %q", got)
	}
	if len(tv.clipboardChunks) != 0 {
		t.Error("Chunk buffer not cleared after finalization")
	}
}

type mockAuth struct {
	val   int
	calls int
}

func (m *mockAuth) Authorize(id string) int {
	m.calls++
	return m.val
}

func TestTerminalView_ProcessFar2lInteract_AuthCaching(t *testing.T) {
	tv := NewTerminalView(80, 24)
	defer tv.Close()
	pty := &mockPty{}
	tv.pty = pty

	m := &mockAuth{val: 1} // Allow Once
	oldAuth := vtui.GlobalClipboardAccessManager
	vtui.GlobalClipboardAccessManager = m
	defer func() { vtui.GlobalClipboardAccessManager = oldAuth }()

	clientID := "id-for-caching-test"

	// Call Authorize twice
	for i := 0; i < 2; i++ {
		stk := vtinput.Far2lStack{}
		stk.PushString(clientID)
		stk.PushU8('o')
		stk.PushU8('c')
		stk.PushU8(uint8(i))
		tv.ProcessFar2lInteract(stk)
	}

	if m.calls != 1 {
		t.Errorf("Authorize called %d times, expected 1 (caching should prevent duplicate prompts)", m.calls)
	}
}

type mockLocalAuth struct {
	*F4ClipboardAuth
}

func (m *mockLocalAuth) Authorize(id string) int { return -1 }

func TestTerminalView_ProcessFar2lInteract_LocalAuth(t *testing.T) {
	tv := NewTerminalView(80, 24)
	defer tv.Close()
	pty := &mockPty{}
	tv.pty = pty

	// Swap real auth manager with one that forces Local mode (-1)
	oldAuth := vtui.GlobalClipboardAccessManager
	vtui.GlobalClipboardAccessManager = &mockLocalAuth{}
	defer func() { vtui.GlobalClipboardAccessManager = oldAuth }()

	stk := vtinput.Far2lStack{}
	stk.PushString("test-client")
	stk.PushU8('o') // subcommand: open
	stk.PushU8('c') // category: clipboard
	stk.PushU8(42)  // request id

	tv.ProcessFar2lInteract(stk)

	rawResp := string(pty.written)
	// Prefix is \x1b_far2l (7 bytes)
	if !strings.HasPrefix(rawResp, "\x1b_far2l") || !strings.HasSuffix(rawResp, "\x07") {
		t.Fatalf("Malformed response: %q", rawResp)
	}

	// Strip prefix and suffix to get base64 payload
	b64 := rawResp[7 : len(rawResp)-1]
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("Failed to decode base64: %v. Raw string: %q", err, b64)
	}
	if len(decoded) < 2 {
		t.Fatalf("Decoded response too short")
	}

	// decoded format: [payload bytes...] + [1 byte id]
	// for clipboard open: [8 bytes flags] + [1 byte respAuth] + [1 byte id]
	respAuth := decoded[len(decoded)-2]
	if respAuth != 1 {
		t.Errorf("Expected respAuth=1 for local fallback (-1), got %d", respAuth)
	}

	id := decoded[len(decoded)-1]
	if id != 42 {
		t.Errorf("Expected response ID=42, got %d", id)
	}
}

func TestTerminalView_ProcessFar2lInteract_Notification(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	tv := NewTerminalView(80, 24)
	defer tv.Close()

	stk := vtinput.Far2lStack{}
	stk.PushString("Alert Body")
	stk.PushString("Title")
	stk.PushU8('n') // Notification
	stk.PushU8(1)   // ID

	tv.ProcessFar2lInteract(stk)

	// Pump task queue
	foundDialog := false
	timeout := time.After(500 * time.Millisecond)
Loop:
	for {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
			if vtui.FrameManager.GetTopFrameType() == vtui.TypeDialog {
				foundDialog = true
				break Loop
			}
		case <-timeout:
			break Loop
		}
	}

	if !foundDialog {
		t.Error("Notification APC did not result in a Message Box")
	}
}

func TestTerminalView_ProcessFar2lInteract_FKeys(t *testing.T) {
	tv := NewTerminalView(80, 24)
	defer tv.Close()
	pty := &mockPty{}
	tv.pty = pty

	stk := vtinput.Far2lStack{}
	// Push 12 pairs of (exists, string) for F1-F12
	for i := 0; i < 12; i++ {
		stk.PushString(fmt.Sprintf("F%d-Custom", i+1))
		stk.PushU8(1)
	}
	stk.PushU8('f') // FKey titles
	stk.PushU8(1)   // ID

	// Should not panic and should send '1' as success status
	tv.ProcessFar2lInteract(stk)

	if len(pty.written) == 0 {
		t.Error("No reply for FKey titles update")
	}
}

func TestTerminalView_HistoryAndReflow(t *testing.T) {
	// Создаем терминал шириной 10
	tv := NewTerminalView(10, 5)
	defer tv.Close()

	// Пишем длинную строку без пробелов (Hard Wrap)
	text := "1234567890ABCDE" // 15 символов
	for _, r := range text {
		tv.PutChar(r, DefaultTermAttr)
	}

	// В VTE Mirror мы проверяем историю через GetAllLogBytes()
	logBytes := tv.GetAllLogBytes()
	result := strings.TrimSpace(string(logBytes))

	if result != text {
		t.Errorf("History mismatch: expected %q, got %q", text, result)
	}

	// Выдавливаем строки в лог, чтобы проверить Reflow в PieceTable
	tv.scrollUp(0, 4, 5)

	for len(tv.GridHistory) > 0 {
		tv.extrudeGridHistoryRow(0)
		tv.GridHistory = tv.GridHistory[1:]
		tv.GridHistoryWrap = tv.GridHistoryWrap[1:]
	}

	// Проверяем фрагментацию при ширине 10
	// Должно быть 2 фрагмента: "1234567890" и "ABCDE"
	frags := tv.engine.GetFragments(0)
	if len(frags) != 2 {
		t.Errorf("Expected 2 fragments at width 10, got %d", len(frags))
	}

	// Ресайзим до 5
	tv.Resize(5, 5)
	// Теперь должно быть 3 фрагмента по 5 символов
	frags = tv.engine.GetFragments(0)
	if len(frags) != 3 {
		t.Errorf("Reflow failed: expected 3 fragments at width 5, got %d", len(frags))
	}
}

func TestTerminalView_StylesPreservation(t *testing.T) {
	tv := NewTerminalView(80, 5)
	defer tv.Close()

	red := vtui.SetIndexFore(0, 1)
	blue := vtui.SetIndexFore(0, 4)

	tv.SetCursor(0, 0)
	// Пишем "RED" красным и "BLUE" синим
	for _, r := range "RED" {
		tv.PutChar(r, red)
	}
	for _, r := range "BLUE" {
		tv.PutChar(r, blue)
	}

	tv.pushRowToGridHistory(0)
	tv.extrudeGridHistoryRow(0)
	tv.GridHistory = tv.GridHistory[1:]
	tv.GridHistoryWrap = tv.GridHistoryWrap[1:]

	// Проверяем атрибуты в логе через getAttrAt
	// "RED" — оффсеты 0, 1, 2
	if tv.getAttrAt(0) != red {
		t.Error("Style at offset 0 should be RED")
	}
	if tv.getAttrAt(2) != red {
		t.Error("Style at offset 2 should be RED")
	}

	// "BLUE" — оффсеты 3, 4, 5, 6
	if tv.getAttrAt(3) != blue {
		t.Error("Style at offset 3 should be BLUE")
	}
	if tv.getAttrAt(6) != blue {
		t.Error("Style at offset 6 should be BLUE")
	}
}
func TestTerminalView_ScrollModes(t *testing.T) {
	tv := NewTerminalView(10, 5)
	defer tv.Close()

	// Setup: fill with 0..4
	for i := 0; i < 5; i++ {
		tv.SetCursor(0, i)
		tv.PutChar(rune('0'+i), DefaultTermAttr)
	}

	// 1. Scroll Up (Text moves up, deletion at top, insertion at bottom)
	tv.scrollUp(1, 3, 1) // Lines 1,2,3 affected
	if tv.Lines[1][0].Char != '2' || tv.Lines[2][0].Char != '3' || tv.Lines[3][0].Char != ' ' {
		t.Errorf("Scroll Up failed. Row 1: %c, Row 3: %c", tv.Lines[1][0].Char, tv.Lines[3][0].Char)
	}

	// 2. Scroll Down (Text moves down, deletion at bottom, insertion at top)
	tv.scrollDown(0, 4, 2)
	if tv.Lines[2][0].Char != '0' || tv.Lines[0][0].Char != ' ' || tv.Lines[1][0].Char != ' ' {
		t.Errorf("Scroll Down failed. Row 2: %c, Row 0: %c", tv.Lines[2][0].Char, tv.Lines[0][0].Char)
	}
}
func TestTerminalView_WideCharAlignment(t *testing.T) {
	tv := NewTerminalView(10, 2)
	defer tv.Close()
	tv.SetCursor(0, 0)

	// '世' is a wide character (2 columns)
	tv.PutChar('世', DefaultTermAttr)

	// HYPOTHESIS: If wide characters aren't handled, cursor only moves by 1
	if tv.CursorX != 2 {
		t.Errorf("Wide char positioning failed: expected CursorX=2, got %d. This will cause attribute shift!", tv.CursorX)
	}

	// Check if the second cell is marked as a filler to prevent overdrawing
	if tv.Lines[0][1].Char != vtui.WideCharFiller {
		t.Errorf("Wide char filler missing at index 1. Got U+%04X", tv.Lines[0][1].Char)
	}

	// Write 'A' after '世'
	tv.PutChar('A', DefaultTermAttr)
	if tv.Lines[0][2].Char != 'A' {
		t.Errorf("Character after wide char misaligned: expected 'A' at index 2, got %c", rune(tv.Lines[0][2].Char))
	}
}
func TestTerminalView_AutoWrap(t *testing.T) {
	width := 10
	tv := NewTerminalView(width, 5)
	defer tv.Close()
	tv.SetCursor(0, 0)

	// Write 10 characters (fill line)
	for i := 0; i < 10; i++ {
		tv.PutChar('X', 0)
	}

	if tv.CursorX != 10 { // On the edge
		t.Errorf("CursorX should be 10, got %d", tv.CursorX)
	}

	// Write 11th character. Auto-wrap should occur.
	tv.PutChar('Y', 0)

	if tv.CursorY != 1 {
		t.Errorf("Auto-wrap failed: CursorY should be 1, got %d", tv.CursorY)
	}
	if tv.CursorX != 1 {
		t.Errorf("Auto-wrap failed: CursorX should be 1, got %d", tv.CursorX)
	}
	if tv.Lines[1][0].Char != 'Y' {
		t.Errorf("Auto-wrap failed: 'Y' should be at (0, 1), got %c", rune(tv.Lines[1][0].Char))
	}
}
func TestTerminalView_VTEMirror_PromptOverwrite(t *testing.T) {
	// Тестируем архитектурное поведение VTE Mirror:
	// При перерисовке промпта (например, после ресайза ConPTY)
	// данные просто перезаписываются в активной сетке, избегая дублирования в истории.
	tv := NewTerminalView(80, 24)
	defer tv.Close()
	tv.UseAltScreen = false

	// 1. Shell prints a prompt "$ "
	tv.PutChar('$', 0)
	tv.PutChar(' ', 0)

	// 2. Shell moves cursor back to 0 (X=0) and prints a DIFFERENT prompt "> "
	tv.PutChar('\r', 0)
	tv.PutChar('>', 0)

	// Убеждаемся, что бесконечный лог чист (данные туда не попадают до скроллинга)
	if tv.pt.Size() != 0 {
		t.Errorf("PieceTable should be empty in VTE Mirror before scroll. Size: %d", tv.pt.Size())
	}

	// Активная сетка должна содержать новый промпт
	if tv.Lines[tv.CursorY][0].Char != '>' || tv.Lines[tv.CursorY][1].Char != ' ' {
		t.Errorf("Active grid should contain the rewritten prompt")
	}
}
func TestTerminalView_AutoWrap_NoHistoryLoss(t *testing.T) {
	// Verifies that auto-wrapping operates correctly within VTE Mirror limits.
	tv := NewTerminalView(10, 5)
	defer tv.Close()
	tv.UseAltScreen = false

	// Write 10 chars to fill the first line
	for i := 0; i < 10; i++ {
		tv.PutChar('A', 0)
	}

	// Write 11th char - triggers auto-wrap. Cursor becomes (1, 1). lastCharWasCR is FALSE.
	tv.PutChar('B', 0)

	// Heuristic should NOT trigger. History representation should contain all 11 chars.
	expected := "AAAAAAAAAAB"
	logBytes := tv.GetAllLogBytes()
	result := strings.TrimSpace(string(logBytes))

	if result != expected {
		t.Errorf("Auto-wrap caused data loss. Expected %q, got %q", expected, result)
	}
}
func TestTerminalView_BottomAlignmentAndExtrusionGuard(t *testing.T) {
	tv := NewTerminalView(80, 24)
	defer tv.Close()

	// 1. Убеждаемся, что инициализация происходит снизу (прилипание к низу)
	if tv.CursorY != 23 {
		t.Errorf("Terminal must initialize at bottom (Y=23), got %d", tv.CursorY)
	}

	// 2. Пишем текст, чтобы спровоцировать скролл
	tv.PutChar('A', 0)
	tv.PutChar('\n', 0)
	tv.PutChar('B', 0)

	// 3. Проверяем лог. Extrusion Guard должен предотвратить
	// попадание изначальных пустых строк (эхо старта) в историю.
	logBytes := tv.GetAllLogBytes()
	logStr := strings.TrimRight(string(logBytes), "\n ") // убираем пустые места активной сетки
	if logStr != "A\nB" {
		t.Errorf("Extrusion guard failed or bottom alignment lost. Log looks like: %q", logStr)
	}
}

func TestTerminalView_MutedStateAndOSC133(t *testing.T) {
	tv := NewTerminalView(80, 24)
	defer tv.Close()

	// 1. Оболочка печатает первичный промпт (терминал не замьючен)
	for _, r := range "prompt> " {
		tv.PutChar(r, 0)
	}

	// 2. f4 печатает команду пользователя
	tv.PrintCleanCommand("ls")

	// 3. f4 мьютит терминал перед отправкой технической wire-команды
	tv.SetMuted(true)

	// 4. Оболочка делает эхо технической команды (оно должно быть проигнорировано)
	for _, r := range "set +H; ugly command" {
		tv.PutChar(r, 0)
	}

	// 5. Оболочка доходит до OSC 133;C (начало вывода)
	tv.HandleOSC133("C")
	if tv.Muted {
		t.Error("OSC 133;C did not unmute the terminal")
	}

	// 6. Вывод самой команды
	for _, r := range "output\n" {
		tv.PutChar(r, 0)
	}

	// 7. Завершение выполнения команды (OSC 133;D).
	// Терминал НЕ должен мьютиться, чтобы принять следующий промпт.
	tv.HandleOSC133("D")
	if tv.Muted {
		t.Error("OSC 133;D erroneously muted the terminal")
	}

	// 8. Оболочка печатает следующий промпт
	for _, r := range "prompt> " {
		tv.PutChar(r, 0)
	}

	// 9. Проверяем лог. Должно быть: prompt> ls\noutput\nprompt>
	logStr := strings.TrimRight(string(tv.GetAllLogBytes()), " \n\r")
	expected := "prompt> ls\noutput\nprompt>"
	if !strings.Contains(logStr, expected) {
		t.Errorf("Log stitching with Mute/OSC failed.\nExpected: %q\nGot: %q", expected, logStr)
	}
}

func TestTerminalView_Resize_VerticalPreservation(t *testing.T) {
	tv := NewTerminalView(80, 5) // Маленький терминал
	defer tv.Close()

	// Заполняем все 5 строк текстом
	for i := 0; i < 5; i++ {
		tv.SetCursor(0, i)
		tv.PutChar(rune('0'+i), 0)
	}

	// Уменьшаем высоту до 3
	tv.Resize(80, 3)

	// Строки '0' и '1' должны быть вытеснены в GridHistory (без потерь)
	if len(tv.GridHistory) != 2 {
		t.Errorf("Expected 2 rows in GridHistory, got %d", len(tv.GridHistory))
	}
	if tv.Lines[0][0].Char != '2' {
		t.Errorf("Top visible row should be '2', got '%c'", tv.Lines[0][0].Char)
	}

	// Увеличиваем высоту обратно до 5
	tv.Resize(80, 5)

	// Строки '0' и '1' должны вернуться из GridHistory на экран (VTE Mirror)
	if len(tv.GridHistory) != 0 {
		t.Errorf("Expected GridHistory to be empty after restoring size, got %d", len(tv.GridHistory))
	}
	if tv.Lines[0][0].Char != '0' {
		t.Errorf("Top visible row should be restored to '0', got '%c'", tv.Lines[0][0].Char)
	}
}

func TestTerminalView_Resize_HorizontalPreservation(t *testing.T) {
	tv := NewTerminalView(10, 5)
	defer tv.Close()
	tv.SetCursor(0, 0)

	// Пишем строку, которая при ресайзе выйдет за пределы
	text := "12345678"
	for _, r := range text {
		tv.PutChar(r, 0)
	}

	// Сужаем окно до 5 колонок
	tv.Resize(5, 5)

	// VTE Mirror реализует Horizontal Preservation: визуальный массив сузился логически,
	// но физически данные в слайсе сохраняются `copyLen = max(w, len(srcLine))`
	if len(tv.Lines[0]) < 8 {
		t.Errorf("Horizontal preservation failed: line length shrank to %d", len(tv.Lines[0]))
	}
	if tv.Lines[0][7].Char != '8' {
		t.Errorf("Hidden data lost: expected '8' at index 7, got '%c'", tv.Lines[0][7].Char)
	}

	// Расширяем обратно до 10 колонок
	tv.Resize(10, 5)
	if tv.Lines[0][7].Char != '8' {
		t.Errorf("Restored data lost: expected '8' at index 7, got '%c'", tv.Lines[0][7].Char)
	}
}

func TestTerminalView_PrintCleanCommandBehavior(t *testing.T) {
	tv := NewTerminalView(80, 24) // Курсор изначально на Y=23
	defer tv.Close()

	// Первая команда
	tv.PrintCleanCommand("ls -la")
	// После печати команды и \r\n экран прокручивается, но курсор остается прилепленным к низу
	if tv.CursorY != 23 {
		t.Errorf("CursorY moved unexpectedly: %d", tv.CursorY)
	}

	// "ls -la" должно оказаться на Y=22 из-за скролла
	if tv.Lines[22][0].Char != 'l' || tv.Lines[22][1].Char != 's' {
		t.Errorf("First clean command not printed correctly")
	}

	// Вторая (последующая) команда. Поведение должно быть абсолютно идентичным
	// (сверху не должно появляться пустых отступов, курсор не должен прыгать).
	tv.PrintCleanCommand("echo 1")
	if tv.CursorY != 23 {
		t.Errorf("CursorY moved unexpectedly on subsequent command: %d", tv.CursorY)
	}
	if tv.Lines[22][0].Char != 'e' || tv.Lines[22][1].Char != 'c' {
		t.Errorf("Subsequent clean command not printed correctly")
	}
}

func TestTerminalView_CompleteLogStitching(t *testing.T) {
	tv := NewTerminalView(10, 5)
	defer tv.Close()

	// Пишем более 2000 строк для срабатывания экструзии (Extrusion) из GridHistory в PieceTable
	for i := 0; i < 2010; i++ {
		tv.PutChar('A', 0)
		tv.PutChar('\n', 0)
	}

	// Теперь лог состоит из 3-х слоев архитектуры:
	// - PieceTable (вытесненные строки)
	// - GridHistory (2000 строк - лимит)
	// - Active Grid (5 строк окна)
	logBytes := tv.GetAllLogBytes()
	logStr := string(logBytes)

	// Разбиваем по \n и убираем последнюю пустую строку
	lines := strings.Split(strings.TrimRight(logStr, "\n "), "\n")

	if len(lines) != 2010 {
		t.Errorf("Log stitching failed. Expected 2010 lines, got %d", len(lines))
	}
}

func TestTerminalView_EraseDisplay_EmptyScreenGuard(t *testing.T) {
	tv := NewTerminalView(80, 24) // Пустой экран сразу после старта
	defer tv.Close()

	// Симулируем bash clear (часто приходит при старте сессии)
	tv.EraseDisplay(2, 0)

	// Поскольку экран пуст, в GridHistory не должно попасть ни одной строки
	// (защита от замусоривания лога 24-мя пустыми строками при clear).
	if len(tv.GridHistory) != 0 {
		t.Errorf("EraseDisplay on empty screen pushed garbage to GridHistory. Count: %d", len(tv.GridHistory))
	}
}
func TestTerminalView_WindowsConPTY_VisualGravity(t *testing.T) {
	// Имитируем типичное поведение ConPTY: окно 24 строки,
	// но shell упрямо рисует в самом верху (строки 0 и 1).
	height := 24
	tv := NewTerminalView(80, height)
	defer tv.Close()
	tv.SetFocus(true)
	tv.SetVisible(true)

	// 1. Shell принудительно прыгает в (0,0) - Home.
	// Используем DefaultTermAttr, чтобы пустые строки не считались "текстом" из-за цвета.
	tv.EraseDisplay(2, DefaultTermAttr)
	tv.SetCursor(0, 0)
	for _, r := range "Microsoft Windows" {
		tv.PutChar(r, 0)
	}
	tv.SetCursor(0, 1)
	for _, r := range "C:\\>" {
		tv.PutChar(r, 0)
	}

	// 2. Проверяем рендеринг через ScreenBuf
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, height)
	tv.SetPosition(0, 0, 79, height-1)
	tv.Show(scr)

	// Ожидаемое поведение: Visual Gravity должна вычислить, что последняя
	// заполненная строка — это 1. Значит offset = (23 - 1) = 22.
	// "Microsoft Windows" (бывшая строка 0) должна оказаться на Y = 22.
	// "C:\>" (бывшая строка 1) должна оказаться на Y = 23 (самый низ).

	cell0 := scr.GetCell(0, 22)
	if cell0.Char != 'M' {
		t.Errorf("Visual Gravity failed: expected 'M' at bottom-1 (Y=22), got '%c'", cell0.Char)
	}

	cell1 := scr.GetCell(0, 23)
	if cell1.Char != 'C' {
		t.Errorf("Visual Gravity failed: expected 'C' at bottom (Y=23), got '%c'", cell1.Char)
	}

	_, curY := scr.GetCursorPos()

	// Проверяем положение курсора - он тоже должен упасть вниз
	if curY != 23+tv.Y1 {
		t.Errorf("Visual Gravity: cursor did not follow the text. Expected Y=23, got %d", curY-tv.Y1)
	}
}

func TestTerminalView_WindowsConPTY_LogIntegrity(t *testing.T) {
	// Проверяем, что абсолютные прыжки курсора ConPTY не создают
	// "дырок" или дублей в текстовом логе GetAllLogBytes()
	tv := NewTerminalView(80, 10)
	defer tv.Close()

	// Пишем в строку 5
	tv.SetCursor(0, 5)
	for _, r := range "Middle" {
		tv.PutChar(r, 0)
	}

	// Прыгаем назад в строку 2
	tv.SetCursor(0, 2)
	for _, r := range "Top" {
		tv.PutChar(r, 0)
	}

	logStr := strings.TrimSpace(string(tv.GetAllLogBytes()))

	// GetAllLogBytes должен собрать строки в их логическом порядке (сверху вниз),
	// пропуская пустые строки сверху, если лог был пуст.
	expected := "Top\n\n\nMiddle"
	if !strings.Contains(logStr, expected) {
		t.Errorf("Windows Log stitching failed.\nExpected to contain: %q\nGot: %q", expected, logStr)
	}
}

func TestTerminalView_WindowsConPTY_ResizePreservation(t *testing.T) {
	// Тест на "эффект гармошки": сжимаем по вертикали, вытесняя ConPTY-данные
	// в историю, и расширяем обратно.
	tv := NewTerminalView(80, 10)
	defer tv.Close()

	// Рисуем текст в строке 0 (верх)
	tv.SetCursor(0, 0)
	for _, r := range "C:\\>" {
		tv.PutChar(r, 0)
	}

	// Сжимаем до 5 строк. Верхняя строка должна быть вытеснена в GridHistory.
	tv.Resize(80, 5)
	if len(tv.GridHistory) == 0 {
		t.Fatal("Resize shoud have pushed bottom row to GridHistory")
	}

	// Расширяем обратно до 10. VTE Mirror должен вернуть строку из истории на экран.
	tv.Resize(80, 10)

	// Из-за Extrusion Guard пустые строки (1-4) при сжатии были проигнорированы.
	// Поэтому единственная вытесненная строка вернется не на 0, а на самый низ
	// восстанавливаемого пространства (на индекс 4). Главное — данные не потеряны.
	if tv.Lines[4][0].Char != 'C' {
		t.Errorf("Data lost after accordion resize. Expected 'C' at row 4, got '%c'", tv.Lines[4][0].Char)
	}
}

func TestAnsiParser_WindowsSmartPrompt(t *testing.T) {
	// Проверяем, как парсер реагирует на новую переменную PROMPT=$E]133;D$E\$P$G
	tv := NewTerminalView(80, 24)
	defer tv.Close()
	p := NewAnsiParser(tv, nil)

	tv.SetMuted(false)

	// Имитируем вывод cmd.exe с нашим новым промптом
	// \x1b]133;D\x1b\ (сигнал завершения) + D:\> (текст промпта)
	promptPayload := "\x1b]133;D\x1b\\D:\\>"

	// В Windows мы решили не мьютить терминал.
	p.Process([]byte(promptPayload))

	// OSC 133;D должен был сработать.
	// В Windows мы решили не мьютить, но если сигнал пришел - он должен обрабатываться.
	// Проверяем, что текст "D:\>" попал в лог (значит парсер не "съел" лишнего)
	logStr := string(tv.GetAllLogBytes())
	if !strings.Contains(logStr, "D:\\>") {
		t.Errorf("Smart PROMPT text was lost during OSC parsing. Log: %q", logStr)
	}
}

func TestTerminalView_WindowsConPTY_NoDoubleEcho(t *testing.T) {
	// Проверка того, что мы не печатаем команду дважды в Windows.
	// f4 НЕ должен вызывать PrintCleanCommand на Windows.
	tv := NewTerminalView(80, 24)
	defer tv.Close()

	// Эмулируем нативный эхо-ответ от ConPTY
	// Пользователь набрал 'dir', ConPTY вернул 'dir\r\n'
	tv.PutChar('d', 0)
	tv.PutChar('i', 0)
	tv.PutChar('r', 0)
	tv.PutChar('\r', 0)
	tv.PutChar('\n', 0)

	// Если бы мы еще раз вызвали PrintCleanCommand("dir") - было бы дублирование.
	// Проверяем, что в логе только один экземпляр 'dir'
	logStr := string(tv.GetAllLogBytes())
	if strings.Count(logStr, "dir") > 1 {
		t.Error("Double echo detected! Windows execution path must not call PrintCleanCommand.")
	}
}

func TestTerminalView_EraseDisplay_LogSync(t *testing.T) {
	tv := NewTerminalView(80, 24)
	defer tv.Close()
	tv.UseAltScreen = false

	// 1. Write some content
	for _, r := range "content" {
		tv.PutChar(r, 0)
	}

	// 2. Execute 'clear' (mode 2)
	tv.EraseDisplay(2, 0)

	// Cursor must be homed
	if tv.CursorX != 0 || tv.CursorY != 0 {
		t.Errorf("EraseDisplay(2) failed to home cursor: (%d,%d)", tv.CursorX, tv.CursorY)
	}

	// 3. Write new content after clear
	tv.PutChar('X', 0)

	// After clear, "content" must be pushed to GridHistory
	foundInGridHistory := false
	for _, row := range tv.GridHistory {
		var sb strings.Builder
		for _, ci := range row {
			sb.WriteRune(rune(ci.Char))
		}
		if strings.Contains(sb.String(), "content") {
			foundInGridHistory = true
			break
		}
	}
	if !foundInGridHistory {
		t.Errorf("Content was not pushed to GridHistory on EraseDisplay(2)")
	}

	// Verify 'X' is present in the final combined log
	logBytes := tv.GetAllLogBytes()
	if !strings.HasSuffix(strings.TrimSpace(string(logBytes)), "X") {
		t.Errorf("Content after clear was lost. Log: %q", string(logBytes))
	}
}
