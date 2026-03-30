package main

import (
	"strings"
	"time"
	"os"
	"testing"
	"github.com/unxed/vtui"
	"github.com/unxed/vtui/piecetable"
	"github.com/unxed/vtinput"
)

func TestEditorView_TypingAndBackspace(t *testing.T) {
	pt := piecetable.New([]byte("Hello"))
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 24)
	ev.SetVisible(true)
	ev.CursorPos = 5 // End of "Hello"

	// 1. Typing '!'
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: '!'})
	if pt.String() != "Hello!" {
		t.Errorf("Typing failed: expected 'Hello!', got '%s'", pt.String())
	}
	if ev.CursorPos != 6 {
		t.Errorf("CursorPos after typing: expected 6, got %d", ev.CursorPos)
	}

	// 2. Deleting '!' via Backspace
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_BACK})
	if pt.String() != "Hello" {
		t.Errorf("Backspace failed: expected 'Hello', got '%s'", pt.String())
	}
	if ev.CursorPos != 5 {
		t.Errorf("CursorPos after backspace: expected 5, got %d", ev.CursorPos)
	}
}

func TestEditorView_LineNavigation(t *testing.T) {
	pt := piecetable.New([]byte("Line1\nLine2"))
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 24)
	ev.SetVisible(true)
	ev.CursorLine = 0
	ev.CursorPos = 5 // End of "Line1"

	// 1. Right Arrow at the end of the line -> move to the beginning of the next
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_RIGHT})
	if ev.CursorLine != 1 || ev.CursorPos != 0 {
		t.Errorf("Cross-line Right failed: expected Line 1, Pos 0. Got Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}

	// 2. Left Arrow at the start of the line -> move to the end of the previous
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_LEFT})
	if ev.CursorLine != 0 || ev.CursorPos != 5 {
		t.Errorf("Cross-line Left failed: expected Line 0, Pos 5. Got Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}
}

func TestEditorView_EnterAndBackspaceMerging(t *testing.T) {
	pt := piecetable.New([]byte("ABC"))
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 24)
	ev.SetVisible(true)
	ev.CursorPos = 1 // Between A and B

	// 1. Press Enter -> split line "A" and "BC"
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_RETURN})
	if pt.String() != "A\nBC" {
		t.Errorf("Enter splitting failed: expected 'A\\nBC', got %q", pt.String())
	}
	if ev.CursorLine != 1 || ev.CursorPos != 0 {
		t.Errorf("Cursor position after Enter wrong: Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}

	// 2. Press Backspace at the start of the second line -> merge back to "ABC"
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_BACK})
	if pt.String() != "ABC" {
		t.Errorf("Backspace merging failed: expected 'ABC', got %q", pt.String())
	}
	if ev.CursorLine != 0 || ev.CursorPos != 1 {
		t.Errorf("Cursor position after merge wrong: Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}
}

func TestEditorView_StickyColumn(t *testing.T) {
	pt := piecetable.New([]byte("LongLine\nShort\nLongLine"))
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 24)
	ev.SetVisible(true)
	ev.WordWrap = false
	ev.syncListViewer()

	ev.CursorLine = 0
	ev.CursorPos = 8
	ev.DesiredVisualCol = 8

	// 1. Down to short line
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
	ev.syncListViewer()
	if ev.CursorPos != 5 {
		t.Errorf("Down to short line: expected pos 5, got %d", ev.CursorPos)
	}
	if ev.DesiredVisualCol != 8 {
		t.Errorf("Desired position lost! Expected 8, got %d", ev.DesiredVisualCol)
	}

	// 2. Down to long line -> position should be restored to 8
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
	if ev.CursorLine != 2 || ev.CursorPos != 8 {
		t.Errorf("Sticky column failed: expected Line 2, Pos 8. Got Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}
}

func TestEditorView_SaveFile(t *testing.T) {
	// 1. Create a temporary file
	tmpFile := "test_save.txt"
	defer os.Remove(tmpFile)
	err := os.WriteFile(tmpFile, []byte("Original"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Open it in the editor
	pt := piecetable.New([]byte("Original"))
	ev := NewEditorView(pt, tmpFile)

	// 3. Simulate typing text " + Edit" at the end
	ev.CursorPos = 8
	for _, char := range " + Edit" {
		ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: char})
	}

	// 4. Simulate pressing F2 (Save)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_F2})

	// 5. Read file from disk and check that data was written
	savedData, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatal(err)
	}

	expected := "Original + Edit"
	if string(savedData) != expected {
		t.Errorf("Save failed: expected %q on disk, got %q", expected, string(savedData))
	}
}

func TestEditorView_Selection(t *testing.T) {
	pt := piecetable.New([]byte("Select Me"))
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 24)
	ev.SetVisible(true)
	ev.CursorLine = 0
	ev.CursorPos = 0

	// 1. Start selection (Shift + Right x 6)
	// Important to emulate KeyDown with Shift flag in the test
	for i := 0; i < 6; i++ {
		ev.ProcessKey(&vtinput.InputEvent{
			Type:            vtinput.KeyEventType,
			KeyDown:         true,
			VirtualKeyCode:  vtinput.VK_RIGHT,
			ControlKeyState: vtinput.ShiftPressed,
		})
	}

	if !ev.selActive {
		t.Fatal("Selection should be active")
	}
	if ev.selAnchorOffset != 0 {
		t.Errorf("Anchor should be 0, got %d", ev.selAnchorOffset)
	}

	min, max := ev.getSelectionRange()
	if min != 0 || max != 6 {
		t.Errorf("Wrong selection range: [%d:%d]", min, max)
	}

	// 2. Copying (Ctrl+C) - checking only the log or lack of panic
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_C,
		ControlKeyState: vtinput.LeftCtrlPressed,
	})

	// 3. Deleting selected (Delete)
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_DELETE,
	})

	if pt.String() != " Me" {
		t.Errorf("Delete selection failed: %q", pt.String())
	}
	if ev.selActive {
		t.Error("Selection should be cleared after delete")
	}
}

func TestEditorView_DeleteSelectionMultiline(t *testing.T) {
	// Three-line text
	pt := piecetable.New([]byte("Line1\nLine2\nLine3"))
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 24)
	ev.SetVisible(true)

	// 1. Select the end of the first line, all of the second, and the start of the third
	// "Line[1\nLine2\nLin]e3"
	ev.CursorLine = 0
	ev.CursorPos = 4
	ev.selActive = true
	ev.selAnchorOffset = ev.li.GetLineOffset(0) + ev.CursorPos // Offset 4

	// Move cursor to the end of selection
	ev.CursorLine = 2
	ev.CursorPos = 3
	// Offset of the beginning of "Line3" (12) + 3 = 15

	// 2. Delete selection
	ev.DeleteSelection()

	// Expected result: "Linee3"
	expected := "Linee3"
	if pt.String() != expected {
		t.Errorf("Multiline delete failed: expected %q, got %q", expected, pt.String())
	}

	// Check that line index updated (1 line left)
	if ev.li.LineCount() != 1 {
		t.Errorf("LineCount after multiline delete: expected 1, got %d", ev.li.LineCount())
	}

	// Check cursor position (should be at the deletion point)
	if ev.CursorLine != 0 || ev.CursorPos != 4 {
		t.Errorf("Cursor after multiline delete: expected Line 0, Pos 4. Got Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}
}

func TestEditorView_WordWrapNavigation(t *testing.T) {
	text := "0123456789ABCDEFGHIJklmno"
	pt := piecetable.New([]byte(text))
	ev := NewEditorView(pt, "")
	ev.WordWrap = true
	ev.SetPosition(0, 0, 9, 6)
	ev.SetVisible(true)
	ev.syncListViewer()

	ev.CursorLine = 0
	ev.CursorPos = 5
	ev.updateDesiredVisualCol()

	// 1. Down to Row 1
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
	ev.syncListViewer()

	if ev.CursorPos != 15 {
		t.Errorf("WordWrap Down: expected byte pos 15, got %d", ev.CursorPos)
	}

	// 2. Down to Row 2
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
	ev.syncListViewer()
	if ev.CursorPos != 25 {
		t.Errorf("WordWrap Down to end: expected byte pos 25, got %d", ev.CursorPos)
	}

	// 3. Up back to Row 1
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_UP})
	ev.syncListViewer()
	if ev.CursorPos != 15 {
		t.Errorf("WordWrap Up: expected byte pos 15, got %d", ev.CursorPos)
	}
}

func TestEditorView_UTF8Editing(t *testing.T) {
	// "Привет" - Russian letters occupy 2 bytes each
	pt := piecetable.New([]byte("Привет"))
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 24)
	ev.SetVisible(true)
	ev.CursorPos = 4 // After "Пр" (4 bytes)

	// 1. Insert another letter (2 bytes)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: 'и'})
	if ev.CursorPos != 6 {
		t.Errorf("UTF8 typing: expected pos 6, got %d", ev.CursorPos)
	}

	// 2. Backspace should remove exactly one character (2 bytes)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_BACK})
	if pt.String() != "Привет" {
		t.Errorf("UTF8 backspace failed: %q", pt.String())
	}
	if ev.CursorPos != 4 {
		t.Errorf("UTF8 backspace pos: expected 4, got %d", ev.CursorPos)
	}
}

func TestEditorView_WideCharWrap(t *testing.T) {
	// "A世B" -> A(1), 世(2), B(1).
	// Ширина 2.
	pt := piecetable.New([]byte("A世B"))
	ev := NewEditorView(pt, "")
	ev.WordWrap = true
	ev.engine.SetWidth(2)

	frags := ev.engine.GetFragments(0)
	if len(frags) < 2 {
		t.Fatalf("Expected at least 2 fragments, got %d", len(frags))
	}
	// Проверяем, что широкие символы не разрываются (это гарантирует WrapEngine)
}

func TestEditorView_SelectionWrapping(t *testing.T) {
	pt := piecetable.New([]byte("1234567890"))
	ev := NewEditorView(pt, "")
	ev.WordWrap = true
	ev.SetPosition(0, 0, 4, 3) // Width 5, Text height 3
	ev.SetVisible(true)

	// Select "456" (from 3rd to 6th position)
	// This captures the end of the first fragment "12345" and the start of the second "67890"
	ev.CursorPos = 3
	ev.selActive = true
	ev.selAnchorOffset = 3
	ev.CursorPos = 6

	min, max := ev.getSelectionRange()
	if min != 3 || max != 6 {
		t.Errorf("Wrapped selection range failed: [%d:%d]", min, max)
	}
}

func TestEditorView_WideCharNavigation(t *testing.T) {
	// "A世B" -> 世 occupies 2 columns.
	pt := piecetable.New([]byte("A世B"))
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 24)
	ev.SetVisible(true)
	ev.WordWrap = false
	ev.CursorPos = 0 // On 'A'

	// 1. Right -> should land on '世' (offset 1)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_RIGHT})
	if ev.CursorPos != 1 {
		t.Errorf("Navigate to Wide: expected pos 1, got %d", ev.CursorPos)
	}

	// 2. Right -> should SKIP OVER '世' (size 3 bytes in UTF-8) and land on 'B' (offset 1+3=4)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_RIGHT})
	if ev.CursorPos != 4 {
		t.Errorf("Navigate over Wide: expected pos 4, got %d", ev.CursorPos)
	}
}

func TestEditorView_UTF8Selection(t *testing.T) {
	// "Да" - 2 runes, 4 bytes
	pt := piecetable.New([]byte("Да"))
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 24)
	ev.SetVisible(true)
	ev.CursorPos = 0

	// Start selection: Shift + Right (one letter 'Д')
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_RIGHT,
		ControlKeyState: vtinput.ShiftPressed,
	})

	if !ev.selActive { t.Fatal("Selection should be active") }
	min, max := ev.getSelectionRange()
	if min != 0 || max != 2 {
		t.Errorf("UTF8 Selection failed: expected [0:2], got [%d:%d]", min, max)
	}
}

func TestEditorView_HomeEnd(t *testing.T) {
	pt := piecetable.New([]byte("Hello World"))
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 24)
	ev.SetVisible(true)

	// 1. End test
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_END})
	if ev.CursorPos != 11 {
		t.Errorf("End failed: expected pos 11, got %d", ev.CursorPos)
	}

	// 2. Home test
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_HOME})
	if ev.CursorPos != 0 {
		t.Errorf("Home failed: expected pos 0, got %d", ev.CursorPos)
	}
}

func TestEditorView_WideCharBackspace(t *testing.T) {
	// "A世" -> 'A' (1), '世' (3 bytes)
	pt := piecetable.New([]byte("A世"))
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 24)
	ev.SetVisible(true)
	ev.CursorPos = 4 // At the very end

	// Press Backspace (remove '世')
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_BACK})

	if pt.String() != "A" {
		t.Errorf("Wide Backspace failed: expected 'A', got %q", pt.String())
	}
	if ev.CursorPos != 1 {
		t.Errorf("Wide Backspace pos failed: expected 1, got %d", ev.CursorPos)
	}
}

func TestEditorView_BracketedPaste(t *testing.T) {
	pt := piecetable.New([]byte("Start-"))
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 24)
	ev.SetVisible(true)
	ev.CursorLine = 0
	ev.CursorPos = 6

	// 1. Paste start signal (PasteStart: true)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.PasteEventType, PasteStart: true})
	if !ev.IsBusy() {
		t.Error("Editor should be Busy during paste")
	}

	// 2. Simulate characters: "A", "B", Enter (\n), "C"
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: 'A'})
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: 'B'})
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_RETURN})
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: 'C'})

	// IMPORTANT: Model should not change until PasteStart: false
	if pt.String() != "Start-" {
		t.Errorf("Model changed prematurely during paste: %q", pt.String())
	}

	// 3. Paste end signal (PasteStart: false)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.PasteEventType, PasteStart: false})

	// Now everything should be in the model
	expected := "Start-AB\nC"
	if pt.String() != expected {
		t.Errorf("Paste commit failed: expected %q, got %q", expected, pt.String())
	}

	// Check cursor position (line 1, position 1 - after 'C')
	if ev.CursorLine != 1 || ev.CursorPos != 1 {
		t.Errorf("Post-paste cursor error: Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}
}

func TestEditorView_ExtremeBounds(t *testing.T) {
	pt := piecetable.New([]byte("A"))
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 24)
	ev.SetVisible(true)

	// 1. Backspace at file start should not break anything
	ev.CursorLine = 0
	ev.CursorPos = 0
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_BACK})
	if pt.String() != "A" {
		t.Error("Backspace at file start modified the text")
	}

	// 2. Delete at file end should not break anything
	ev.CursorPos = 1
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DELETE})
	if pt.String() != "A" {
		t.Error("Delete at file end modified the text")
	}
}

func TestEditorView_EmptyLinesWrap(t *testing.T) {
	pt := piecetable.New([]byte("\n\n"))
	ev := NewEditorView(pt, "")
	ev.WordWrap = true
	ev.SetPosition(0, 0, 10, 11)
	ev.SetVisible(true)
	ev.syncListViewer()

	if ev.li.LineCount() != 3 {
		t.Errorf("Expected 3 lines, got %d", ev.li.LineCount())
	}

	// Check that engine returns fragments even for empty lines
	ev.engine.SetWidth(10)
	frags := ev.engine.GetFragments(0)
	if len(frags) == 0 {
		t.Fatal("Empty line fragments should not be empty")
	}

	// Empty line navigation
	ev.CursorLine = 0
	ev.CursorPos = 0
	ev.syncListViewer()
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
	if ev.CursorLine != 1 {
		t.Errorf("Down on empty lines failed: expected line 1, got %d", ev.CursorLine)
	}
}

func TestEditorView_WordWrapScrolling(t *testing.T) {
	text := "0123456789ABCDEFGHIJklmnopqrstuvwxyz0123456789"
	pt := piecetable.New([]byte(text))
	ev := NewEditorView(pt, "")
	ev.WordWrap = true
	ev.SetPosition(0, 0, 9, 2)
	ev.SetVisible(true)
	ev.engine.SetWidth(10)

	ev.EnsureVisible()
	if ev.TopPos != 0 {
		t.Error("Initial scroll should be 0")
	}

	// 1. Прыгаем в конец строки (оффсет 46)
	// Конец строки — это 4-й визуальный ряд (индекс 4).
	ev.CursorPos = 46
	ev.syncListViewer()
	ev.EnsureVisible()

	if ev.TopPos != 3 {
		t.Errorf("WordWrap scroll failed: expected TopPos 3, got %d", ev.TopPos)
	}

	// 2. Прыгаем в начало
	ev.CursorPos = 0
	ev.syncListViewer()
	ev.EnsureVisible()
	if ev.TopPos != 0 {
		t.Errorf("WordWrap scroll back failed: expected TopPos 0, got %d", ev.TopPos)
	}
}

func TestEditorView_WordWrapInfiniteLoop(t *testing.T) {
	// Text with wide character
	pt := piecetable.New([]byte("A世B"))
	ev := NewEditorView(pt, "")
	ev.WordWrap = true

	// Extremely narrow window (width 1)
	ev.engine.SetWidth(1)
	frags := ev.engine.GetFragments(0)

	if len(frags) == 0 {
		t.Fatal("Should have produced fragments even for narrow window")
	}
	// Check that we didn't hang and traversed the entire line
	lastFrag := frags[len(frags)-1]
	if lastFrag.ByteOffsetEnd < 5 { // A(1) + 世(3) + B(1) = 5
		t.Errorf("Fragments didn't cover the whole line: end at %d", lastFrag.ByteOffsetEnd)
	}
}

func TestEditorView_F3_ToggleWordWrap(t *testing.T) {
	pt := piecetable.New([]byte("some text"))
	ev := NewEditorView(pt, "")
	ev.WordWrap = true

	// Press F3 (Wait, make sure your code uses VK_F3 now)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_F3})
	if ev.WordWrap {
		t.Error("F3 failed to disable WordWrap")
	}

	// Press F3 again
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_F3})
	if !ev.WordWrap {
		t.Error("F3 failed to re-enable WordWrap")
	}
}

func TestEditorView_Labels(t *testing.T) {
	pt := piecetable.New([]byte(""))
	ev := NewEditorView(pt, "test.txt")
	ks := ev.GetKeyLabels()

	if ks == nil {
		t.Fatal("EditorView.GetKeyLabels() returned nil")
	}

	if ks.Normal[1] != "Save" { // F2
		t.Errorf("Expected F2 to be 'Save', got %q", ks.Normal[1])
	}
	if ks.Normal[9] != "Quit" { // F10
		t.Errorf("Expected F10 to be 'Quit', got %q", ks.Normal[9])
	}
}

func TestEditorView_WideCharDelete(t *testing.T) {
	// "A世" -> 'A' (1), '世' (3 bytes)
	pt := piecetable.New([]byte("A世"))
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 24)
	ev.SetVisible(true)
	ev.CursorPos = 1 // Before '世'

	// Press Delete
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DELETE})

	if pt.String() != "A" {
		t.Errorf("Wide Delete failed: expected 'A', got %q", pt.String())
	}
	if ev.CursorPos != 1 {
		t.Errorf("Cursor position after Wide Delete should remain 1, got %d", ev.CursorPos)
	}
}

func TestEditorView_PageNavigation(t *testing.T) {
	var buf []byte
	for i := 0; i < 20; i++ {
		buf = append(buf, []byte("Line\n")...)
	}
	pt := piecetable.New(buf)
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 5)
	ev.syncListViewer()
	ev.CursorLine = 0
	ev.CursorPos = 0

	// 1. PgDn
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_NEXT})
	ev.syncListViewer()
	if ev.CursorLine != 5 {
		t.Errorf("PgDn failed: expected line 5, got %d", ev.CursorLine)
	}

	// 2. PgUp
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_PRIOR})
	if ev.CursorLine != 0 {
		t.Errorf("PgUp failed: expected line 0, got %d", ev.CursorLine)
	}

	// 3. Selection with PgDn (Shift + PgDn)
	ev.ProcessKey(&vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		KeyDown:         true,
		VirtualKeyCode:  vtinput.VK_NEXT,
		ControlKeyState: vtinput.ShiftPressed,
	})

	if !ev.selActive {
		t.Fatal("Shift+PgDn should activate selection")
	}
	ev.syncListViewer()
	min, max := ev.getSelectionRange()
	// Selection from offset 0 to start of line 5 (5 characters "Line\n" * 5 = 25)
	if min != 0 || max != 25 {
		t.Errorf("Shift+PgDn range failed: expected [0:25], got [%d:%d]", min, max)
	}
}

func TestEditorView_LongLinePerformance(t *testing.T) {
	longLine := strings.Repeat("a", 100*1024)
	pt := piecetable.New([]byte(longLine))
	ev := NewEditorView(pt, "")
	ev.SetPosition(0, 0, 79, 24)
	ev.SetVisible(true)

	ev.CursorPos = 50 * 1024

	done := make(chan struct{})
	go func() {
		// Simulate 100 "right" presses.
		for i := 0; i < 100; i++ {
			ev.ProcessKey(&vtinput.InputEvent{
				Type:           vtinput.KeyEventType,
				KeyDown:        true,
				VirtualKeyCode: vtinput.VK_RIGHT,
			})
		}
		ev.ProcessKey(&vtinput.InputEvent{
			Type:           vtinput.KeyEventType,
			KeyDown:        true,
			VirtualKeyCode: vtinput.VK_END,
		})
		close(done)
	}()

	select {
	case <-done:
		// Success: all operations finished in time.
	case <-time.After(200 * time.Millisecond): // 200ms — generous timeout. Hanging would last seconds.
		t.Fatal("Performance test timed out. EditorView is likely still hanging on long lines.")
	}
}

func TestEditorView_WordNavigation(t *testing.T) {
	pt := piecetable.New([]byte("hello world  test"))
	ev := NewEditorView(pt, "")
	ev.CursorPos = 0

	// 1. Ctrl + Right -> should jump to start of "world" (index 6)
	ev.ProcessKey(&vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		KeyDown:         true,
		VirtualKeyCode:  vtinput.VK_RIGHT,
		ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if ev.CursorPos != 6 {
		t.Errorf("Ctrl+Right (1) failed: expected pos 6, got %d", ev.CursorPos)
	}

	// 2. Ctrl + Right -> should jump to start of "test" (index 13)
	ev.ProcessKey(&vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		KeyDown:         true,
		VirtualKeyCode:  vtinput.VK_RIGHT,
		ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if ev.CursorPos != 13 {
		t.Errorf("Ctrl+Right (2) failed: expected pos 13, got %d", ev.CursorPos)
	}

	// 3. Ctrl + Left -> back to start of "world" (index 6)
	ev.ProcessKey(&vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		KeyDown:         true,
		VirtualKeyCode:  vtinput.VK_LEFT,
		ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if ev.CursorPos != 6 {
		t.Errorf("Ctrl+Left (1) failed: expected pos 6, got %d", ev.CursorPos)
	}

	// 4. Ctrl + Left -> back to start (index 0)
	ev.ProcessKey(&vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		KeyDown:         true,
		VirtualKeyCode:  vtinput.VK_LEFT,
		ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if ev.CursorPos != 0 {
		t.Errorf("Ctrl+Left (2) failed: expected pos 0, got %d", ev.CursorPos)
	}
}
func TestEditorBar_Content(t *testing.T) {
	vtui.SetDefaultPalette()
	SetDefaultF4Palette()
	pt := piecetable.New([]byte("abc"))
	ev := NewEditorView(pt, "test.go")
	ev.SetPosition(0, 0, 40, 10)
	ev.CursorLine = 5
	ev.CursorPos = 12

	scr := vtui.NewScreenBuf()
	scr.AllocBuf(41, 11)

	// Make sure we have enough data to calculate percentages without division by zero
	ev.ItemCount = 100
	ev.ViewHeight = 10

	ev.GetTopBar().Show(scr)

	// В статус-баре должно быть "6,12" (Line+1, Pos)
	foundLine := false
	foundPos := false
	for x := 0; x < 40; x++ {
		if scr.GetCell(x, 0).Char == '6' { foundLine = true }
		if scr.GetCell(x, 0).Char == '1' && scr.GetCell(x+1, 0).Char == '2' { foundPos = true }
	}

	if !foundLine || !foundPos {
		t.Errorf("EditorBar did not display correct cursor info (6,12). Found Line:%v, Pos:%v", foundLine, foundPos)
	}
}
func TestEditorView_HandleClose(t *testing.T) {
	pt := piecetable.New([]byte("test"))
	ev := NewEditorView(pt, "file.txt")

	if ev.IsDone() {
		t.Fatal("Editor should not be done initially")
	}

	// Send CmClose command (simulating menu "Exit" click)
	ev.HandleCommand(vtui.CmClose, nil)

	if !ev.IsDone() {
		t.Error("EditorView failed to set IsDone after receiving CmClose")
	}
}
func TestEditorView_GetTitle(t *testing.T) {
	pt := piecetable.New([]byte(""))

	// With path
	ev1 := NewEditorView(pt, "/var/log/syslog")
	if ev1.GetTitle() != "Edit: syslog" {
		t.Errorf("GetTitle failed for valid path: %s", ev1.GetTitle())
	}

	// Without path
	ev2 := NewEditorView(pt, "")
	if ev2.GetTitle() != "Editor" {
		t.Errorf("GetTitle failed for empty path: %s", ev2.GetTitle())
	}
}

func TestEditorView_ScrollBarMouse(t *testing.T) {
	pt := piecetable.New([]byte("L1\nL2\nL3\nL4\nL5\nL6\nL7\nL8\nL9\nL10"))
	ev := NewEditorView(pt, "")
	// Width 10, Height 3 (Y1=0, Y2=2 -> 2 lines of content)
	ev.SetPosition(0, 0, 9, 2)
	ev.SetVisible(true)
	ev.syncListViewer()

	if ev.TopPos != 0 {
		t.Fatal("Should start at TopPos 0")
	}

	// Important: ScrollBar.Max is updated during rendering
	ev.DisplayObject(vtui.NewScreenBuf())

	// Click on the "Down" area of the scrollbar (Y=2)
	ev.ProcessMouse(&vtinput.InputEvent{
		Type: vtinput.MouseEventType, KeyDown: true,
		ButtonState: vtinput.FromLeft1stButtonPressed,
		MouseX: 9, MouseY: 2,
	})

	if ev.TopPos == 0 {
		t.Error("Editor should have scrolled after scrollbar click")
	}
}

