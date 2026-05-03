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

	// Пишем длинную строку без пробелов (Hard Wrap)
	text := "1234567890ABCDE" // 15 символов
	for _, r := range text {
		tv.PutChar(r, DefaultTermAttr)
	}
	tv.FlushLog()

	// Проверяем PieceTable
	if tv.pt.String() != text {
		t.Errorf("History mismatch: expected %q, got %q", text, tv.pt.String())
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

	red := vtui.SetIndexFore(0, 1)
	blue := vtui.SetIndexFore(0, 4)

	// Пишем "RED" красным и "BLUE" синим
	for _, r := range "RED" {
		tv.PutChar(r, red)
	}
	for _, r := range "BLUE" {
		tv.PutChar(r, blue)
	}
	tv.FlushLog()

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
func TestTerminalView_PromptRewriteHeuristic(t *testing.T) {
	// Tests the heuristic that prevents duplicate prompts in scrollback
	// when a shell re-renders the same line.
	tv := NewTerminalView(80, 24)
	tv.UseAltScreen = false

	// 1. Shell prints a prompt "$ "
	tv.PutChar('$', 0)
	tv.PutChar(' ', 0)
	tv.FlushLog()
	if tv.pt.Size() != 2 {
		t.Errorf("Initial history size mismatch, got %d", tv.pt.Size())
	}

	// 2. Shell moves cursor back to 0 (X=0) and prints a DIFFERENT prompt "> "
	tv.PutChar('\r', 0)
	tv.PutChar('>', 0)
	tv.FlushLog()

	// Heuristic should have wiped the previous '$ ' from history
	if tv.pt.Size() != 1 || tv.pt.String() != ">" {
		t.Errorf("Prompt rewrite heuristic failed. History: %q, Size: %d",
			tv.pt.String(), tv.pt.Size())
	}
}
func TestTerminalView_PromptRewriteDetection(t *testing.T) {
	// Tests the logic that prevents duplicate prompts in history
	// when a shell re-renders the same line.
	tv := NewTerminalView(80, 24)
	tv.UseAltScreen = false // Logic only applies to scrollback history

	// 1. Simulate shell printing a prompt "$ "
	tv.PutChar('$', 0)
	tv.PutChar(' ', 0)
	tv.FlushLog()
	initialSize := tv.pt.Size()
	if initialSize != 2 {
		t.Errorf("Expected history size 2, got %d", initialSize)
	}

	// 2. Shell moves cursor back to 0 and prints a DIFFERENT prompt "> "
	// This happens in some advanced shells or during resize.
	tv.PutChar('\r', 0)
	tv.PutChar('>', 0)
	tv.FlushLog()

	// HEURISTIC: if CursorX is 0 and history for the current line exists, it should be wiped.
	if tv.pt.Size() != 1 {
		t.Errorf("Prompt rewrite detection failed: expected history size 1 ('>'), got %d (%q)",
			tv.pt.Size(), tv.pt.String())
	}
	if tv.pt.String() != ">" {
		t.Errorf("History corrupted: expected '>', got %q", tv.pt.String())
	}
}
func TestTerminalView_AutoWrap_NoHistoryLoss(t *testing.T) {
	// Verifies that auto-wrapping does NOT trigger prompt-rewrite heuristic.
	tv := NewTerminalView(10, 5)
	tv.UseAltScreen = false

	// Write 10 chars to fill the first line
	for i := 0; i < 10; i++ {
		tv.PutChar('A', 0)
	}
	tv.FlushLog()

	// Write 11th char - triggers auto-wrap. Cursor becomes (1, 1). lastCharWasCR is FALSE.
	tv.PutChar('B', 0)
	tv.FlushLog()

	// Heuristic should NOT trigger. History should contain all 11 chars.
	expected := "AAAAAAAAAAB"
	if tv.pt.String() != expected {
		t.Errorf("Auto-wrap caused data loss. Expected %q, got %q", expected, tv.pt.String())
	}
}

func TestTerminalView_EraseDisplay_LogSync(t *testing.T) {
	tv := NewTerminalView(80, 24)
	tv.UseAltScreen = false

	// 1. Write some content
	for _, r := range "content" {
		tv.PutChar(r, 0)
	}
	tv.FlushLog()

	// 2. Execute 'clear' (mode 2)
	tv.EraseDisplay(2, 0)

	// Cursor must be homed
	if tv.CursorX != 0 || tv.CursorY != 0 {
		t.Errorf("EraseDisplay(2) failed to home cursor: (%d,%d)", tv.CursorX, tv.CursorY)
	}

	// lastLineOffset must point to the end of the newly added vertical padding
	if tv.lastLineOffset != tv.pt.Size() {
		t.Errorf("lastLineOffset mismatch after clear: %d vs %d", tv.lastLineOffset, tv.pt.Size())
	}

	// 3. Write new content after clear
	tv.PutChar('X', 0)
	tv.FlushLog()

	// Verify 'X' is preserved and didn't trigger prompt-rewrite on the empty padding
	if !strings.HasSuffix(tv.pt.String(), "X") {
		t.Errorf("Content after clear was lost. Log: %q", tv.pt.String())
	}
}
