package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
	"os"
	"bytes"
	"testing"
	"path/filepath"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
	"github.com/unxed/vtui/piecetable"
	"github.com/unxed/vtinput"
)

func TestEditorView_TypingAndBackspace(t *testing.T) {
	pt := piecetable.New([]byte("Hello"))
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 79, 24) // Устанавливаем стандартный размер 80x25
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
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 79, 24)
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
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 79, 24)
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
	// Creating text:
	// LongLine (8)
	// Short (5)
	// LongLine (8)
	pt := piecetable.New([]byte("LongLine\nShort\nLongLine"))
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 79, 24)
	ev.WordWrap = false // Для этого теста отключаем перенос, чтобы имитировать классику

	// Position at the end of the first long line
	ev.CursorLine = 0
	ev.CursorPos = 8
	ev.DesiredVisualCol = 8

	// 1. Down to short line -> visually at the end (5), но желаемая колонка остается 8
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
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
	v := vfs.NewOSVFS(t.TempDir())
	ev := NewEditorView(pt, v, tmpFile)
	// Add mock file object to editor so SaveToFile logic triggers cleanly
	f, _ := v.Open(context.Background(), tmpFile)
	ev.file = f

	// 3. Simulate typing text " + Edit" at the end
	ev.CursorPos = 8
	for _, char := range " + Edit" {
		ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: char})
	}

	// 4. Simulate pressing F2 (Save)
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf()) // Needed for PostTask to work
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_F2})

	// 5. Wait for async save to finish by processing tasks
	timeout := time.After(1 * time.Second)
	for ev.saving {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-timeout:
			t.Fatal("Timeout waiting for async save to complete")
		}
	}

	// 6. Read file from disk and check that data was written
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
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 79, 24)
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
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 79, 24)

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
	// Текст: "0123456789ABCDEFGHIJklmno" (25 символов)
	// При чистой ширине 10:
	// Ряд 0: "0123456789" (оффсеты 0-10)
	// Ряд 1: "ABCDEFGHIJ" (оффсеты 10-20)
	// Ряд 2: "klmno"      (оффсеты 20-25)
	text := "0123456789ABCDEFGHIJklmno"
	pt := piecetable.New([]byte(text))
	ev := NewEditorView(pt, nil, "")
	ev.WordWrap = true
	// Set width to 11 so that width minus scrollbar (11-1) is exactly 10.
	ev.SetPosition(0, 0, 10, 6)

	// Инициализируем DesiredVisualCol (имитируем клик или переход)
	ev.CursorLine = 0
	ev.CursorPos = 5 // Символ '5'
	ev.updateDesiredVisualCol()

	// 1. Вниз на Ряд 1. Колонка 5 должна соответствовать символу 'F' (оффсет 15)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})

	if ev.CursorPos != 15 {
		t.Errorf("WordWrap Down: expected byte pos 15, got %d", ev.CursorPos)
	}

	// 2. Вниз на Ряд 2. Колонка 5 должна соответствовать концу строки (оффсет 25),
	// так как "klmno" короче 5 колонок.
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
	if ev.CursorPos != 25 {
		t.Errorf("WordWrap Down to end: expected byte pos 25, got %d", ev.CursorPos)
	}

	// 3. Вверх обратно на Ряд 1. Должны вернуться на символ 'F' (15)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_UP})
	if ev.CursorPos != 15 {
		t.Errorf("WordWrap Up: expected byte pos 15, got %d", ev.CursorPos)
	}
}

func TestEditorView_UTF8Editing(t *testing.T) {
	// "Привет" - Russian letters occupy 2 bytes each
	pt := piecetable.New([]byte("Привет"))
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 79, 24)
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
	ev := NewEditorView(pt, nil, "")
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
	ev := NewEditorView(pt, nil, "")
	ev.WordWrap = true
	ev.SetPosition(0, 0, 4, 3) // Width 5, Text height 3

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
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 79, 24)
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
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 79, 24)
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
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 79, 24)

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
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 79, 24)
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
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 79, 24)
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
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 79, 24)

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
	// File of three empty lines (breaks only)
	pt := piecetable.New([]byte("\n\n"))
	ev := NewEditorView(pt, nil, "")
	ev.WordWrap = true
	ev.SetPosition(0, 0, 10, 11)

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
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
	if ev.CursorLine != 1 {
		t.Errorf("Down on empty lines failed: expected line 1, got %d", ev.CursorLine)
	}
}

func TestEditorView_WordWrapScrolling(t *testing.T) {
	// Текст 46 байт. Ширина 10.
	// Фрагменты: 0 (0-10), 1 (10-20), 2 (20-30), 3 (30-40), 4 (40-46)
	text := "0123456789ABCDEFGHIJklmnopqrstuvwxyz0123456789"
	pt := piecetable.New([]byte(text))
	ev := NewEditorView(pt, nil, "")
	ev.WordWrap = true
	ev.SetPosition(0, 0, 9, 2) // Высота 3, высота текста 2
	ev.engine.SetWidth(10)

	ev.ensureCursorVisible()
	if ev.ScrollTopRow != 0 {
		t.Error("Initial scroll should be 0")
	}

	// 1. Прыгаем в конец строки (оффсет 46)
	// Конец строки — это 4-й визуальный ряд (индекс 4).
	ev.CursorPos = 46
	ev.ensureCursorVisible()

	// Чтобы увидеть 4-й ряд при высоте окна 2, верхним должен быть 3-й ряд (индекс 3).
	// Тогда видны ряды 3 и 4.
	if ev.ScrollTopRow != 3 {
		t.Errorf("WordWrap scroll failed: expected ScrollTopRow 3, got %d", ev.ScrollTopRow)
	}
	
	// 2. Прыгаем в начало
	ev.CursorPos = 0
	ev.ensureCursorVisible()
	if ev.ScrollTopRow != 0 {
		t.Errorf("WordWrap scroll back failed: expected ScrollTopRow 0, got %d", ev.ScrollTopRow)
	}
}

func TestEditorView_WordWrapInfiniteLoop(t *testing.T) {
	// Text with wide character
	pt := piecetable.New([]byte("A世B"))
	ev := NewEditorView(pt, nil, "")
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
	ev := NewEditorView(pt, nil, "")
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
	ev := NewEditorView(pt, nil, "test.txt")
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
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 79, 24)
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
	// Create 20 lines of text
	var buf []byte
	for i := 0; i < 20; i++ {
		buf = append(buf, []byte("Line\n")...)
	}
	pt := piecetable.New(buf)
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 79, 5) // Text Viewport height 5
	ev.CursorLine = 0
	ev.CursorPos = 0

	// 1. PgDn
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_NEXT})
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
	min, max := ev.getSelectionRange()
	// Selection from offset 0 to start of line 5 (5 characters "Line\n" * 5 = 25)
	if min != 0 || max != 25 {
		t.Errorf("Shift+PgDn range failed: expected [0:25], got [%d:%d]", min, max)
	}
}

func TestEditorView_LongLinePerformance(t *testing.T) {
	// Removed t.Parallel() to prevent CPU starvation and deadlocks
	// when competing with other UI tests.

	// Create one very long line (100 KB) to simulate the problem.
	// Without the fix, this would cause O(N*M) reads and hanging.
	longLine := strings.Repeat("a", 100*1024)
	pt := piecetable.New([]byte(longLine))
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 79, 24) // 80x25 viewport

	// Set cursor in the middle of the line
	ev.CursorPos = 50 * 1024

	// Wrap test in timeout. If editor "hangs", test fails.
	done := make(chan struct{})
	go func() {
		// Simulate 100 "right" presses. This heavily loads ensureCursorVisible.
		for i := 0; i < 100; i++ {
			ev.ProcessKey(&vtinput.InputEvent{
				Type:           vtinput.KeyEventType,
				KeyDown:        true,
				VirtualKeyCode: vtinput.VK_RIGHT,
			})
		}
		// Moving to end of line — another expensive operation without caching
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
	ev := NewEditorView(pt, nil, "")
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
	ev := NewEditorView(pt, nil, "test.go")
	ev.SetPosition(0, 0, 40, 10)
	ev.CursorLine = 5
	ev.CursorPos = 12

	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(41, 11)

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
	ev := NewEditorView(pt, nil, "file.txt")

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
	ev1 := NewEditorView(pt, nil, "/var/log/syslog")
	if ev1.GetTitle() != "Edit: syslog" {
		t.Errorf("GetTitle failed for valid path: %s", ev1.GetTitle())
	}

	// Without path
	ev2 := NewEditorView(pt, nil, "")
	if ev2.GetTitle() != "Editor" {
		t.Errorf("GetTitle failed for empty path: %s", ev2.GetTitle())
	}
}
func TestEditorView_AsyncIndexing(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	content := "Line 1\nLine 2\nLine 3"
	tmp := t.TempDir() + "/idx_test.txt"
	os.WriteFile(tmp, []byte(content), 0644)

	v := vfs.NewOSVFS(t.TempDir())
	f, _ := v.Open(context.Background(), tmp)

	// Open editor with AsyncBuffer
	buf := NewAsyncBuffer(context.Background(), f)
	pt := piecetable.NewWithBuffer(buf)
	ev := NewEditorView(pt, v, tmp)
	ev.asyncBuf = buf
	ev.file = f

	// Initial LineCount should be 1 (empty or unindexed)
	if ev.li.LineCount() != 1 {
		t.Errorf("Expected 1 line initially, got %d", ev.li.LineCount())
	}

	// Start background indexing
	ev.StartIndexing()

	// Wait and pump tasks
	timeout := time.After(2 * time.Second)
	for ev.li.LineCount() < 3 {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-timeout:
			t.Fatal("Timeout waiting for indexer to find 3 lines")
		}
	}

	if ev.li.LineCount() != 3 {
		t.Errorf("Indexer failed: expected 3 lines, got %d", ev.li.LineCount())
	}
}
func TestEditorView_Indexer_EditInterference(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	// Create a large file with many lines
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString(fmt.Sprintf("Line %d\n", i))
	}
	tmp := t.TempDir() + "/race_test.txt"
	os.WriteFile(tmp, []byte(sb.String()), 0644)

	v := vfs.NewOSVFS(t.TempDir())
	f, _ := v.Open(context.Background(), tmp)
	buf := NewAsyncBuffer(context.Background(), f)
	pt := piecetable.NewWithBuffer(buf)
	ev := NewEditorView(pt, v, tmp)
	ev.asyncBuf = buf
	ev.file = f

	// 1. Start indexing
	ev.StartIndexing()

	// 2. Immediately delete half of the file on the UI thread
	// This should trigger the indexer cancellation via ev.edited = true
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_BACK,
	})

	if !ev.edited {
		t.Error("Editor should be marked as edited after Backspace")
	}

	// 3. Process any tasks that might have been queued by the indexer
	// before it saw the cancellation.
	timeout := time.After(200 * time.Millisecond)
Loop:
	for {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-timeout:
			break Loop
		}
	}

	// 4. Verification: The LineIndex must remain consistent with the PieceTable size
	// even if some stale background tasks were executed.
	lastOffset := ev.li.GetLineOffset(ev.li.LineCount() - 1)
	if lastOffset > pt.Size() {
		t.Errorf("LineIndex corruption: last offset %d exceeds PieceTable size %d", lastOffset, pt.Size())
	}
}
func TestEditorView_StartIndexing_RestartSafety(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	v := vfs.NewOSVFS(t.TempDir())

	// Create a dummy file
	tmp := t.TempDir() + "/restart.txt"
	os.WriteFile(tmp, []byte("line1\nline2"), 0644)
	f, _ := v.Open(context.Background(), tmp)

	buf := NewAsyncBuffer(context.Background(), f)
	pt := piecetable.NewWithBuffer(buf)
	ev := NewEditorView(pt, v, tmp)
	ev.asyncBuf = buf

	// 1. Start indexing
	ev.StartIndexing()
	oldCancel := ev.indexCancel
	if oldCancel == nil { t.Fatal("indexCancel should be set") }

	// 2. Start again immediately
	ev.StartIndexing()

	// 3. Verify it is still set and didn't panic
	if ev.indexCancel == nil {
		t.Error("indexCancel should not be nil after restart")
	}

	// Clean up
	ev.Close()
}
func TestEditorView_UnsavedChanges(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	pt := piecetable.New([]byte("line1"))
	ev := NewEditorView(pt, nil, "test.txt")

	// 1. Initially not modified
	if ev.modified {
		t.Error("Editor should not be marked as modified initially")
	}

	// 2. Modify text (typing) -> should be modified
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: '!'})
	if !ev.modified {
		t.Error("Editor should be modified after typing")
	}

	// 3. Test tryClose when NOT modified
	ev.modified = false
	ev.tryClose()
	if !ev.IsDone() {
		t.Error("Editor should close immediately if not modified")
	}

	// 4. Test tryClose when modified (should NOT close immediately)
	ev.Done = false
	ev.modified = true
	ev.tryClose()
	if ev.IsDone() {
		t.Error("Editor should NOT close immediately if modified (should show dialog)")
	}

	// 5. Verify deletion also triggers modified
	ev.modified = false
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_BACK})
	if !ev.modified {
		t.Error("Editor should be modified after deletion")
	}
}

func TestEditorView_Navigation_DocumentBoundaries(t *testing.T) {
	pt := piecetable.New([]byte("Line 1\nLine 2\nLine 3"))
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 80, 24)
	
	// 1. Ctrl+End -> End of file
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_END, ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if ev.CursorLine != 2 || ev.CursorPos != 6 {
		t.Errorf("Ctrl+End failed: expected line 2 pos 6, got %d:%d", ev.CursorLine, ev.CursorPos)
	}

	// 2. Ctrl+Home -> Start of file
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_HOME, ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if ev.CursorLine != 0 || ev.CursorPos != 0 {
		t.Errorf("Ctrl+Home failed: expected 0:0, got %d:%d", ev.CursorLine, ev.CursorPos)
	}
}

func TestEditorView_SelectAll(t *testing.T) {
	pt := piecetable.New([]byte("First\nSecond"))
	ev := NewEditorView(pt, nil, "")
	
	// Ctrl+A
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_A, ControlKeyState: vtinput.LeftCtrlPressed,
	})
	
	if !ev.selActive { t.Fatal("Selection should be active after Ctrl+A") }
	min, max := ev.getSelectionRange()
	if min != 0 || max != pt.Size() {
		t.Errorf("Ctrl+A range failed: [0:%d], got [%d:%d]", pt.Size(), min, max)
	}
	// Cursor should jump to EOF in Far
	if ev.CursorLine != 1 || ev.CursorPos != 6 {
		t.Errorf("Ctrl+A cursor pos failed, got %d:%d", ev.CursorLine, ev.CursorPos)
	}
}

func TestEditorView_ShiftAliasSelection(t *testing.T) {
	pt := piecetable.New([]byte("ABCDE"))
	ev := NewEditorView(pt, nil, "")
	ev.CursorPos = 0

	// Shift + Ctrl + D (Right alias)
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_D, ControlKeyState: vtinput.LeftCtrlPressed | vtinput.ShiftPressed,
	})

	if !ev.selActive { t.Fatal("Shift + Alias should trigger selection") }
	if ev.selAnchorOffset != 0 || ev.CursorPos != 1 {
		t.Errorf("Selection anchor or cursor wrong: anchor=%d, pos=%d", ev.selAnchorOffset, ev.CursorPos)
	}
}
func TestEditorView_FarNavigation_FullCoverage(t *testing.T) {
	pt := piecetable.New([]byte("Line 1\nLine 2\nLine 3"))
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 80, 24)

	// 1. Ctrl+End -> End of file
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_END, ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if ev.CursorLine != 2 || ev.CursorPos != 6 {
		t.Errorf("Ctrl+End failed: expected 2:6, got %d:%d", ev.CursorLine, ev.CursorPos)
	}

	// 2. Ctrl+Home -> Start of file
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_HOME, ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if ev.CursorLine != 0 || ev.CursorPos != 0 {
		t.Errorf("Ctrl+Home failed: expected 0:0, got %d:%d", ev.CursorLine, ev.CursorPos)
	}

	// 3. Shift + Ctrl + End -> Select to end of file
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_END, ControlKeyState: vtinput.LeftCtrlPressed | vtinput.ShiftPressed,
	})
	if !ev.selActive { t.Fatal("Shift+Ctrl+End should activate selection") }
	min, max := ev.getSelectionRange()
	if min != 0 || max != pt.Size() {
		t.Errorf("Shift+Ctrl+End selection range failed: [0:%d], got [%d:%d]", pt.Size(), min, max)
	}
}

func TestEditorView_FarAliases_FullCoverage(t *testing.T) {
	pt := piecetable.New([]byte("First word\nSecond line"))
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 80, 24)

	// 1. Ctrl+S should move 1 char left, NOT 1 word
	ev.CursorPos = 10
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_S, ControlKeyState: vtinput.LeftCtrlPressed})
	if ev.CursorPos != 9 { t.Errorf("Ctrl+S (alias) moved more than 1 char: pos %d", ev.CursorPos) }

	// 2. Ctrl+D should move 1 char right, NOT 1 word
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_D, ControlKeyState: vtinput.LeftCtrlPressed})
	if ev.CursorPos != 10 { t.Errorf("Ctrl+D (alias) moved more than 1 char: pos %d", ev.CursorPos) }

	// 3. Shift + Ctrl + D -> Select 1 char
	ev.selActive = false
	ev.CursorPos = 0
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_D, ControlKeyState: vtinput.LeftCtrlPressed | vtinput.ShiftPressed,
	})
	if !ev.selActive || ev.CursorPos != 1 { t.Error("Shift + Alias selection failed") }
}

func TestEditorView_FarX_SmartCut(t *testing.T) {
	pt := piecetable.New([]byte("Select me\nNext line"))
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 80, 24)

	// Scenario A: Selection active -> Ctrl+X is CUT
	ev.selActive = true
	ev.selAnchorOffset = 0
	ev.CursorPos = 6 // "Select"
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_X, ControlKeyState: vtinput.LeftCtrlPressed})
	if pt.String() != " me\nNext line" { t.Errorf("Ctrl+X Cut failed: %q", pt.String()) }

	// Scenario B: No selection -> Ctrl+X is DOWN
	ev.selActive = false
	ev.CursorLine = 0
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_X, ControlKeyState: vtinput.LeftCtrlPressed})
	if ev.CursorLine != 1 { t.Error("Ctrl+X Down failed") }
}

func TestEditorView_FarSelectAll_Behavior(t *testing.T) {
	pt := piecetable.New([]byte("All\nText"))
	ev := NewEditorView(pt, nil, "")

	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_A, ControlKeyState: vtinput.LeftCtrlPressed,
	})

	if !ev.selActive || ev.selAnchorOffset != 0 { t.Error("Ctrl+A anchor should be 0") }
	if ev.CursorLine != 1 || ev.CursorPos != 4 { t.Errorf("Ctrl+A cursor should be at EOF, got %d:%d", ev.CursorLine, ev.CursorPos) }
}
func TestEditorView_FarNavigation_Document(t *testing.T) {
	pt := piecetable.New([]byte("Line 1\nLine 2\nLine 3"))
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 80, 24)

	// 1. Ctrl+End -> В самый конец файла
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_END, ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if ev.CursorLine != 2 || ev.CursorPos != 6 {
		t.Errorf("Ctrl+End failed: expected line 2 pos 6, got %d:%d", ev.CursorLine, ev.CursorPos)
	}

	// 2. Ctrl+Home -> В самое начало файла
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_HOME, ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if ev.CursorLine != 0 || ev.CursorPos != 0 {
		t.Errorf("Ctrl+Home failed: expected line 0 pos 0, got %d:%d", ev.CursorLine, ev.CursorPos)
	}
}

func TestEditorView_FarSelectAll(t *testing.T) {
	pt := piecetable.New([]byte("Line 1\nLine 2"))
	ev := NewEditorView(pt, nil, "")

	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_A, ControlKeyState: vtinput.LeftCtrlPressed,
	})

	if !ev.selActive { t.Fatal("Selection should be active after Ctrl+A") }
	min, max := ev.getSelectionRange()
	if min != 0 || max != pt.Size() {
		t.Errorf("Ctrl+A range failed: [0:%d], got [%d:%d]", pt.Size(), min, max)
	}
	// В Far курсор прыгает в конец после выделения всего текста
	if ev.CursorLine != 1 || ev.CursorPos != 6 {
		t.Errorf("Ctrl+A cursor pos failed, got %d:%d", ev.CursorLine, ev.CursorPos)
	}
}

func TestEditorView_FarNavigationAliases(t *testing.T) {
	pt := piecetable.New([]byte("First line\nSecond line\nThird line"))
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 80, 24)
	ev.CursorLine = 1
	ev.CursorPos = 0

	// 1. Ctrl+E -> Вверх
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_E, ControlKeyState: vtinput.LeftCtrlPressed})
	if ev.CursorLine != 0 { t.Errorf("Ctrl+E (Up) failed, line: %d", ev.CursorLine) }

	// 2. Ctrl+X -> Вниз (без выделения)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_X, ControlKeyState: vtinput.LeftCtrlPressed})
	if ev.CursorLine != 1 { t.Errorf("Ctrl+X (Down) failed, line: %d", ev.CursorLine) }

	// 3. Ctrl+S -> Влево (на один символ, а не на слово!)
	ev.CursorPos = 4
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_S, ControlKeyState: vtinput.LeftCtrlPressed})
	if ev.CursorPos != 3 { t.Errorf("Ctrl+S (Left) failed: expected 3, got %d", ev.CursorPos) }

	// 4. Ctrl+D -> Вправо (на один символ)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_D, ControlKeyState: vtinput.LeftCtrlPressed})
	if ev.CursorPos != 4 { t.Errorf("Ctrl+D (Right) failed: expected 4, got %d", ev.CursorPos) }
}

func TestEditorView_FarX_CutVsDown(t *testing.T) {
	pt := piecetable.New([]byte("Some selected text\nNext line"))
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 80, 24)

	// 1. С выделением Ctrl+X должен сработать как Cut
	ev.selActive = true
	ev.selAnchorOffset = 0
	ev.CursorPos = 4 // Выделено "Some"

	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_X, ControlKeyState: vtinput.LeftCtrlPressed,
	})

	if pt.String() != " selected text\nNext line" {
		t.Errorf("Ctrl+X (Cut) failed: text is %q", pt.String())
	}

	// 2. Без выделения Ctrl+X должен сработать как Down (навигация Far)
	ev.selActive = false
	ev.CursorLine = 0
	ev.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_X, ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if ev.CursorLine != 1 {
		t.Error("Ctrl+X without selection should move cursor down")
	}
}
func TestEditorView_Search_Basic(t *testing.T) {
	vtui.SetDefaultPalette()
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	content := "The quick brown fox jumps over the lazy dog"
	pt := piecetable.New([]byte(content))
	ev := NewEditorView(pt, nil, "test.txt")
	ev.SetPosition(0, 0, 80, 24)

	// Запускаем поиск слова "fox"
	ev.Search("fox", false)

	// Прокачиваем задачи из очереди (PostTask), так как поиск асинхронный
	timeout := time.After(1 * time.Second)
Loop:
	for {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
			// Если выделение стало активным, значит поиск завершился
			if ev.selActive {
				break Loop
			}
		case <-timeout:
			t.Fatal("Search timed out")
		}
	}

	// "fox" начинается с 16-го байта
	if ev.selAnchorOffset != 16 {
		t.Errorf("Expected search anchor at 16, got %d", ev.selAnchorOffset)
	}

	// Конец совпадения — 16 + 3 = 19. Проверяем позицию курсора.
	actualOffset := ev.li.GetLineOffset(ev.CursorLine) + ev.CursorPos
	if actualOffset != 19 {
		t.Errorf("Expected cursor at 19, got %d", actualOffset)
	}
}

func TestEditorView_Search_Next(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	// Два вхождения слова "match"
	pt := piecetable.New([]byte("match one, match two"))
	ev := NewEditorView(pt, nil, "test.txt")
	ev.SetPosition(0, 0, 80, 24)

	// 1. Находим первое вхождение
	ev.Search("match", false)

	timeout := time.After(1 * time.Second)
	for !ev.selActive {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-timeout:
			t.Fatal("First search failed")
		}
	}

	if ev.selAnchorOffset != 0 {
		t.Errorf("First match should be at 0, got %d", ev.selAnchorOffset)
	}

	// 2. Ищем следующее (Find Next)
	ev.selActive = false // Сбрасываем для проверки нового результата
	ev.Search("match", true)

	timeout = time.After(1 * time.Second)
	for !ev.selActive {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-timeout:
			t.Fatal("Next search failed")
		}
	}

	// Второе "match" начинается с 11-го байта
	if ev.selAnchorOffset != 11 {
		t.Errorf("Second match should be at 11, got %d", ev.selAnchorOffset)
	}
}

func TestEditorView_Search_CaseInsensitive(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	pt := piecetable.New([]byte("ALL CAPS TEXT"))
	ev := NewEditorView(pt, nil, "test.txt")
	ev.SetPosition(0, 0, 80, 24)

	// Ищем "caps" маленькими буквами
	ev.Search("caps", false)

	timeout := time.After(1 * time.Second)
	for !ev.selActive {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-timeout:
			t.Fatal("Case-insensitive search failed")
		}
	}

	if ev.selAnchorOffset != 4 {
		t.Errorf("Should find 'CAPS' at offset 4, got %d", ev.selAnchorOffset)
	}
}
func TestEditorView_Search_NotFound(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	pt := piecetable.New([]byte("some text"))
	ev := NewEditorView(pt, nil, "test.txt")

	// Ищем то, чего нет
	ev.Search("missing", false)

	// Ждем появления сообщения об ошибке (оно создается через ShowMessage)
	timeout := time.After(1 * time.Second)
	foundMessage := false
Loop:
	for {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
			// Проверяем, не открылся ли диалог (сообщение об ошибке)
			if vtui.FrameManager.GetTopFrameType() == vtui.TypeDialog {
				foundMessage = true
				break Loop
			}
		case <-timeout:
			break Loop
		}
	}

	if !foundMessage {
		t.Error("Search should show a message box when pattern is not found")
	}
}
func TestEditorView_SaveFailure_NoDataLoss(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	tmpFile := t.TempDir() + "/important.txt"
	os.WriteFile(tmpFile, []byte("Original"), 0644)

	// Use our failing VFS
	baseVfs := vfs.NewOSVFS(filepath.Dir(tmpFile))
	failingVfs := &mockFailingVFS{VFS: baseVfs, failRename: true}

	pt := piecetable.New([]byte("Original"))
	ev := NewEditorView(pt, failingVfs, tmpFile)
	f, _ := failingVfs.Open(context.Background(), tmpFile)
	ev.file = f

	// 1. Modify the file
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: 'X'})
	if !ev.modified { t.Fatal("Editor should be modified") }

	// 2. Attempt to save (F2)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_F2})

	// Process async tasks
	timeout := time.After(2 * time.Second)
	saveFinished := false
	for !saveFinished {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
			if !ev.saving { saveFinished = true }
		case <-timeout:
			t.Fatal("Timeout waiting for save operation")
		}
	}

	// 3. Assertions
	// The modified flag MUST remain true because the save failed!
	if !ev.modified {
		t.Error("CRITICAL: Editor 'modified' flag was cleared even though save failed! Data loss risk.")
	}

	// Original file must remain untouched
	data, _ := os.ReadFile(tmpFile)
	if string(data) != "Original" {
		t.Errorf("CRITICAL: Original file was corrupted during failed save. Got %q", string(data))
	}

	// Should have popped an error dialog
	if vtui.FrameManager.GetTopFrameType() != vtui.TypeDialog {
		t.Error("Editor did not show an error dialog upon save failure")
	}
}
// mockFailingVFS wraps OSVFS but intentionally fails the Rename operation
type mockFailingVFS struct {
	vfs.VFS
	failRename bool
}

func (m *mockFailingVFS) Rename(ctx context.Context, old, new string) error {
	if m.failRename {
		return os.ErrPermission // Simulate permission denied
	}
	return m.VFS.Rename(ctx, old, new)
}
func TestEditorView_Save_DiskFullSimulation(t *testing.T) {
	// Verifies that if writing to the temp file fails (e.g. disk full),
	// the editor does not clear the modified flag and doesn't destroy memory state.
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	tmpFile := t.TempDir() + "/important.txt"
	os.WriteFile(tmpFile, []byte("Stable Content"), 0644)

	baseVfs := vfs.NewOSVFS(filepath.Dir(tmpFile))
	failingVfs := &mockFailingWriteVFS{VFS: baseVfs}

	pt := piecetable.New([]byte("Stable Content"))
	ev := NewEditorView(pt, failingVfs, tmpFile)
	f, _ := failingVfs.Open(context.Background(), tmpFile)
	ev.file = f

	// 1. Modify (at the end of the text)
	ev.CursorPos = len("Stable Content")
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: '!'})

	// 2. Attempt Save
	ev.SaveToFile(nil)

	// Pump tasks
	timeout := time.After(1 * time.Second)
	for ev.saving {
		select {
		case task := <-vtui.FrameManager.TaskChan: task()
		case <-timeout: t.Fatal("Timeout")
		}
	}

	// 3. Verify state
	if !ev.modified {
		t.Error("Editor cleared modified flag despite write failure!")
	}
	if ev.pt.String() != "Stable Content!" {
		t.Error("Editor memory state was corrupted after failed save")
	}
}
func TestEditorView_LargePaste_Consistency(t *testing.T) {
	// Tests stability and index consistency when pasting large blocks of text.
	pt := piecetable.New([]byte("Start\nEnd"))
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 80, 24)
	ev.CursorLine = 1
	ev.CursorPos = 0 // Before "End"

	// Create 1MB block with many newlines
	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		sb.WriteString("pasted line content\n")
	}
	pasteData := sb.String()

	// Simulate Bracketed Paste
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.PasteEventType, PasteStart: true})
	for _, r := range pasteData {
		ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: r})
	}
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.PasteEventType, PasteStart: false})

	// 1. Verify content
	expectedSize := 5 + 1 + len(pasteData) + 3 // "Start" + \n + paste + "End"
	if pt.Size() != expectedSize {
		t.Errorf("Size mismatch after large paste: expected %d, got %d", expectedSize, pt.Size())
	}

	// 2. Verify LineIndex integrity
	if ev.li.LineCount() != 5000+2 {
		t.Errorf("Line count mismatch: expected 5002, got %d", ev.li.LineCount())
	}

	// 3. Verify cursor is at the end of the paste
	if ev.CursorLine != 5001 || ev.CursorPos != 0 {
		t.Errorf("Cursor misplaced after large paste: %d:%d", ev.CursorLine, ev.CursorPos)
	}

	// 4. Verify no crash on re-render
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	ev.Show(scr)
}

func TestEditorView_DeleteSelection_EOFBoundaries(t *testing.T) {
	// Ensures deleting the very last character or line doesn't crash the editor.
	content := "line1\nline2"
	pt := piecetable.New([]byte(content))
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 80, 24)

	// Select last line and the newline before it
	ev.selActive = true
	ev.selAnchorOffset = 5 // after "line1"
	ev.CursorLine = 1
	ev.CursorPos = 5 // at end of "line2"

	ev.DeleteSelection()

	if pt.String() != "line1" {
		t.Errorf("EOF delete failed: expected 'line1', got %q", pt.String())
	}
	if ev.li.LineCount() != 1 {
		t.Errorf("Line count mismatch: expected 1, got %d", ev.li.LineCount())
	}
	if ev.CursorLine != 0 || ev.CursorPos != 5 {
		t.Errorf("Cursor misplaced after EOF delete: %d:%d", ev.CursorLine, ev.CursorPos)
	}

	// Test deleting the only remaining character
	ev.selActive = true
	ev.selAnchorOffset = 0
	ev.CursorPos = 5
	ev.DeleteSelection()

	if pt.Size() != 0 {
		t.Errorf("Failed to delete last line, size: %d", pt.Size())
	}
	if ev.CursorLine != 0 || ev.CursorPos != 0 {
		t.Errorf("Cursor not at 0:0 after full delete: %d:%d", ev.CursorLine, ev.CursorPos)
	}
}

type mockFailingWriteVFS struct {
	vfs.VFS
}

func (m *mockFailingWriteVFS) Create(ctx context.Context, path string) (io.WriteCloser, error) {
	return &failingWriter{}, nil
}

type failingWriter struct{}
func (f *failingWriter) Write(p []byte) (n int, err error) { return 0, fmt.Errorf("mock write failure") }
func (f *failingWriter) Close() error { return nil }
func TestEditorView_Save_IOErrorRecovery(t *testing.T) {
	// Verifies that a failure during the streaming write phase of saving
	// does not corrupt the Editor's memory state and does not clear the modified flag.
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "persist.txt")
	os.WriteFile(path, []byte("Initial Data"), 0644)

	// Mock VFS that allows Open/Stat/Rename but returns a failing writer for Create
	baseVfs := vfs.NewOSVFS(tmpDir)
	failingVfs := &mockFailingWriteVFS{VFS: baseVfs}

	pt := piecetable.New([]byte("Initial Data"))
	ev := NewEditorView(pt, failingVfs, path)
	f, _ := failingVfs.Open(context.Background(), path)
	ev.file = f

	// 1. Modify the content
	ev.CursorPos = 12
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: '!'})

	// 2. Trigger Save
	ev.SaveToFile(nil)

	// Pump tasks to process the async save
	timeout := time.After(1 * time.Second)
	for ev.saving {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-timeout:
			t.Fatal("Timeout waiting for save failure")
		}
	}

	// 3. Verify Integrity
	if !ev.modified {
		t.Error("Editor 'modified' flag was cleared despite IO failure")
	}
	if ev.pt.String() != "Initial Data!" {
		t.Errorf("Memory state corrupted. Expected 'Initial Data!', got %q", ev.pt.String())
	}

	// Ensure temp file was cleaned up (handled by vfs.Remove in save logic)
	if _, err := os.Stat(path + ".f4tmp"); !os.IsNotExist(err) {
		t.Error("Temporary file was not cleaned up after failed save")
	}
}
func TestEditorView_Save_AtomicRenameFailure(t *testing.T) {
	// Verifies that if the final Rename fails (e.g., target file is locked by another process),
	// the editor does not lose data and keeps the internal state modified.
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "locked.txt")
	os.WriteFile(path, []byte("Original Content"), 0644)

	// Mock VFS that allows everything except the final Rename
	baseVfs := vfs.NewOSVFS(tmpDir)
	failingVfs := &mockFailingVFS{VFS: baseVfs, failRename: true}

	pt := piecetable.New([]byte("Original Content"))
	ev := NewEditorView(pt, failingVfs, path)
	f, _ := failingVfs.Open(context.Background(), path)
	ev.file = f

	// 1. Modify the content
	ev.CursorPos = 0
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: '!'})

	// 2. Trigger Save
	ev.SaveToFile(nil)

	// Pump tasks to process the async save
	timeout := time.After(1 * time.Second)
	for ev.saving {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-timeout:
			t.Fatal("Timeout waiting for rename failure")
		}
	}

	// 3. Verify Integrity
	if !ev.modified {
		t.Error("Editor cleared modified flag despite Rename failure")
	}
	if ev.pt.String() != "!Original Content" {
		t.Errorf("Internal memory state corrupted after rename failure. Got %q", ev.pt.String())
	}

	// Original file MUST remain untouched
	orig, _ := os.ReadFile(path)
	if string(orig) != "Original Content" {
		t.Error("Original file was corrupted after a failed atomic rename save")
	}
}
func TestEditorView_ModificationStress(t *testing.T) {
	// Tests stability of LineIndex and navigation during randomized edits.
	content := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5"
	pt := piecetable.New([]byte(content))
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 80, 24)
	ev.WordWrap = true

	// A sequence of mixed operations
	ops := []struct {
		char uint16
		vk   uint16
		ctrl bool
	}{
		{vk: vtinput.VK_END},
		{char: 'a'}, {char: 'b'}, {char: 'c'},
		{vk: vtinput.VK_RETURN},
		{char: 'x'}, {char: 'y'}, {char: 'z'},
		{vk: vtinput.VK_UP},
		{vk: vtinput.VK_HOME},
		{vk: vtinput.VK_DELETE}, {vk: vtinput.VK_DELETE},
		{vk: vtinput.VK_BACK},
		{char: ' '},
		{vk: vtinput.VK_A, ctrl: true}, // Select all
		{vk: vtinput.VK_DELETE},        // Wipe document
		{char: 'R'}, {char: 'e'}, {char: 's'}, {char: 't'}, {char: 'a'}, {char: 'r'}, {char: 't'},
	}

	for i, op := range ops {
		ctrlFlag := uint32(0)
		if op.ctrl {
			ctrlFlag = vtinput.LeftCtrlPressed
		}
		ev.ProcessKey(&vtinput.InputEvent{
			Type:            vtinput.KeyEventType,
			KeyDown:         true,
			Char:            rune(op.char),
			VirtualKeyCode:  op.vk,
			ControlKeyState: ctrlFlag,
		})

		// After every op, verify LineIndex integrity
		expectedLi := piecetable.NewLineIndex()
		expectedLi.Rebuild(ev.pt)
		if ev.li.LineCount() != expectedLi.LineCount() {
			t.Fatalf("Step %d: LineCount mismatch. Got %d, want %d", i, ev.li.LineCount(), expectedLi.LineCount())
		}
	}

	if ev.pt.String() != "Restart" {
		t.Errorf("Stress test result mismatch: %q", ev.pt.String())
	}
}

func TestEditorView_CRLFPieceTable(t *testing.T) {
	// Verifies that \r\n line endings don't cause off-by-one errors in indices.
	content := "Line1\r\nLine2\r\nLine3"
	pt := piecetable.New([]byte(content))
	ev := NewEditorView(pt, nil, "")
	ev.SetPosition(0, 0, 80, 24)

	// 1. Check initial line count
	if ev.li.LineCount() != 3 {
		t.Errorf("Expected 3 lines for CRLF content, got %d", ev.li.LineCount())
	}

	// 2. Navigation: Down from end of line 0
	ev.CursorLine = 0
	ev.CursorPos = 5 // After '1', before '\r'
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_RIGHT})

	if ev.CursorLine != 1 || ev.CursorPos != 0 {
		t.Errorf("CRLF cross-line navigation failed. Target: 1:0, Got: %d:%d", ev.CursorLine, ev.CursorPos)
	}

	// 3. Backspace from start of line 1 (merging lines)
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_BACK})

	// Expected: "Line1" + "Line2" ( \r\n removed )
	if !strings.Contains(ev.pt.String(), "Line1Line2") {
		t.Errorf("CRLF merge failed: %q", ev.pt.String())
	}
	if ev.li.LineCount() != 2 {
		t.Errorf("LineCount after CRLF merge: expected 2, got %d", ev.li.LineCount())
	}
}
func TestEditorView_FragmentationDataIntegrity(t *testing.T) {
	// Tests that a highly fragmented file (many pieces in the table)
	// can be saved without a single byte being lost or swapped.
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "fragmented.txt")

	// 1. Initial content
	initial := []byte("Initial Content\n")
	os.WriteFile(path, initial, 0644)

	v := vfs.NewOSVFS(tmpDir)
	f, _ := v.Open(context.Background(), path)
	buf := NewAsyncBuffer(context.Background(), f)
	pt := piecetable.NewWithBuffer(buf)
	ev := NewEditorView(pt, v, path)
	ev.file = f
	ev.asyncBuf = buf

	// 2. Perform 500 deterministic "random" edits to fragment the PieceTable
	// We alternate between inserting and deleting to keep the size manageable
	// but force piece splitting and buffer interleaving.
	reference := append([]byte(nil), initial...)

	for i := 0; i < 500; i++ {
		pos := (i * 7) % (len(reference) + 1)
		data := []byte(fmt.Sprintf("[%d]", i))

		// Update reference
		newRef := make([]byte, 0, len(reference)+len(data))
		newRef = append(newRef, reference[:pos]...)
		newRef = append(newRef, data...)
		newRef = append(newRef, reference[pos:]...)
		reference = newRef

		// Update Editor
		ev.CursorLine = ev.li.GetLineAtOffset(pos)
		ev.CursorPos = pos - ev.li.GetLineOffset(ev.CursorLine)
		for _, r := range string(data) {
			ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: r})
		}

		// Occasional deletion
		if i%3 == 0 && len(reference) > 10 {
			delPos := (i * 13) % (len(reference) - 5)
			reference = append(reference[:delPos], reference[delPos+5:]...)

			ev.CursorLine = ev.li.GetLineAtOffset(delPos)
			ev.CursorPos = delPos - ev.li.GetLineOffset(ev.CursorLine)
			ev.selActive = true
			ev.selAnchorOffset = delPos
			// Move cursor by 5
			newPos := delPos + 5
			ev.CursorLine = ev.li.GetLineAtOffset(newPos)
			ev.CursorPos = newPos - ev.li.GetLineOffset(ev.CursorLine)
			ev.DeleteSelection()
		}
	}

	// 3. Save the fragmented result
	ev.SaveToFile(nil)

	// Pump tasks
	timeout := time.After(3 * time.Second)
	for ev.saving {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-timeout:
			t.Fatal("Timeout waiting for fragmented file save")
		}
	}

	// 4. Verify byte-for-byte consistency
	savedData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(savedData, reference) {
		t.Errorf("DATA CORRUPTION: Saved data does not match expected reference.\nSaved len: %d, Ref len: %d", len(savedData), len(reference))
		// Log start of difference for debugging
		for i := 0; i < len(savedData) && i < len(reference); i++ {
			if savedData[i] != reference[i] {
				t.Errorf("First mismatch at byte %d: saved 0x%02x, ref 0x%02x", i, savedData[i], reference[i])
				break
			}
		}
	}
}
func TestEditorView_Save_NoTrailingNewline_Integrity(t *testing.T) {
	// Verifies that saving a file that does NOT end with a newline
	// does not accidentally add one (a common error in text editors).
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	tmpFile := t.TempDir() + "/nonewline.txt"
	content := []byte("Line 1\nLine 2 (no newline at end)")
	os.WriteFile(tmpFile, content, 0644)

	v := vfs.NewOSVFS(filepath.Dir(tmpFile))
	pt := piecetable.New(content)
	ev := NewEditorView(pt, v, tmpFile)
	f, _ := v.Open(context.Background(), tmpFile)
	ev.file = f

	// 1. Modify in the middle
	ev.CursorLine = 0
	ev.CursorPos = 0
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: '!'})

	// 2. Save
	ev.SaveToFile(nil)
	timeout := time.After(1 * time.Second)
	for ev.saving {
		select {
		case task := <-vtui.FrameManager.TaskChan: task()
		case <-timeout: t.Fatal("Timeout")
		}
	}

	// 3. Verify exactly what's on disk
	saved, _ := os.ReadFile(tmpFile)
	expected := "!Line 1\nLine 2 (no newline at end)"
	if string(saved) != expected {
		t.Errorf("Save corrupted end-of-file. Expected %q, got %q", expected, string(saved))
	}
}

func TestEditorView_Save_RetryAfterFailure(t *testing.T) {
	// Verifies that if a save fails once, the editor state remains valid
	// and allows a retry once the external issue is resolved.
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "retry.txt")
	os.WriteFile(path, []byte("Initial"), 0644)

	baseVfs := vfs.NewOSVFS(tmpDir)
	// mockFailingVFS is defined in the same test file usually
	failingVfs := &mockFailingVFS{VFS: baseVfs, failRename: true}

	pt := piecetable.New([]byte("Initial"))
	ev := NewEditorView(pt, failingVfs, path)
	f, _ := failingVfs.Open(context.Background(), path)
	ev.file = f

	// 1. Modify
	ev.SetText("Changed")

	// 2. Save (should fail at Rename)
	ev.SaveToFile(nil)
	timeout := time.After(1 * time.Second)
	for ev.saving {
		select {
		case task := <-vtui.FrameManager.TaskChan: task()
		case <-timeout: t.Fatal("Timeout on first save")
		}
	}
	if !ev.modified { t.Error("Should still be modified after failure") }

	// 3. Fix the VFS issue
	failingVfs.failRename = false

	// 4. Retry saving
	ev.SaveToFile(nil)
	timeout = time.After(1 * time.Second)
	for ev.saving {
		select {
		case task := <-vtui.FrameManager.TaskChan: task()
		case <-timeout: t.Fatal("Timeout on retry save")
		}
	}

	// 5. Verification
	if ev.modified { t.Error("Should NOT be modified after successful retry") }

	saved, _ := os.ReadFile(path)
	if string(saved) != "Changed" {
		t.Errorf("Data not saved correctly on retry. Expected 'Changed', got %q", string(saved))
	}
}
