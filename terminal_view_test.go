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

func TestTerminalView_ScrollingRegion(t *testing.T) {
	tv := NewTerminalView(80, 10)
	// Устанавливаем регион прокрутки: строки со 2 по 5 (1-based: 3;6)
	tv.ScrollTop = 2
	tv.ScrollBottom = 5

	// Заполняем строку 5
	tv.SetCursor(0, 5)
	tv.PutChar('X', 0)

	// Вызываем прокрутку в регионе (перенос строки на последней строке региона)
	tv.SetCursor(0, 5)
	tv.PutChar('\n', 0)

	// Проверяем: строка 5 должна стать пустой, а 'X' должен уехать на строку 4
	if tv.Lines[4][0].Char != 'X' {
		t.Errorf("Scroll region failed: 'X' should be at line 4, got %c at line 5", rune(tv.Lines[5][0].Char))
	}
	if tv.Lines[5][0].Char != ' ' {
		t.Error("Scroll region failed: line 5 should be cleared")
	}
}
func TestTerminalView_AutoWrap(t *testing.T) {
	width := 10
	tv := NewTerminalView(width, 5)
	tv.SetCursor(0, 0)

	// Пишем 10 символов (заполняем строку)
	for i := 0; i < 10; i++ {
		tv.PutChar('X', 0)
	}

	if tv.CursorX != 10 { // На грани
		t.Errorf("CursorX should be 10, got %d", tv.CursorX)
	}

	// Пишем 11-й символ. Должен произойти автоперенос.
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
