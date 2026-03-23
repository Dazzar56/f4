package main

import (
	"testing"

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

func TestTerminalView_HistoryAndReflow(t *testing.T) {
	// Создаем терминал шириной 10
	tv := NewTerminalView(10, 5)

	// Пишем длинную строку без пробелов (Hard Wrap)
	text := "1234567890ABCDE" // 15 символов
	for _, r := range text {
		tv.PutChar(r, DefaultTermAttr)
	}

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
	for _, r := range "RED" { tv.PutChar(r, red) }
	for _, r := range "BLUE" { tv.PutChar(r, blue) }

	// Проверяем атрибуты в логе через getAttrAt
	// "RED" — оффсеты 0, 1, 2
	if tv.getAttrAt(0) != red { t.Error("Style at offset 0 should be RED") }
	if tv.getAttrAt(2) != red { t.Error("Style at offset 2 should be RED") }

	// "BLUE" — оффсеты 3, 4, 5, 6
	if tv.getAttrAt(3) != blue { t.Error("Style at offset 3 should be BLUE") }
	if tv.getAttrAt(6) != blue { t.Error("Style at offset 6 should be BLUE") }
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
