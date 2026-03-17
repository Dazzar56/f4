package main

import (
	"testing"
	"github.com/unxed/f4/piecetable"
	"github.com/unxed/vtinput"
)

func TestEditorView_TypingAndBackspace(t *testing.T) {
	pt := piecetable.New([]byte("Hello"))
	ev := NewEditorView(pt)
	ev.CursorPos = 5 // Конец "Hello"

	// 1. Печатаем '!'
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: '!'})
	if pt.String() != "Hello!" {
		t.Errorf("Typing failed: expected 'Hello!', got '%s'", pt.String())
	}
	if ev.CursorPos != 6 {
		t.Errorf("CursorPos after typing: expected 6, got %d", ev.CursorPos)
	}

	// 2. Стираем '!' через Backspace
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
	ev := NewEditorView(pt)
	ev.CursorLine = 0
	ev.CursorPos = 5 // Конец "Line1"

	// 1. Стрелка Вправо в конце строки -> переход на начало следующей
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_RIGHT})
	if ev.CursorLine != 1 || ev.CursorPos != 0 {
		t.Errorf("Cross-line Right failed: expected Line 1, Pos 0. Got Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}

	// 2. Стрелка Влево в начале строки -> переход в конец предыдущей
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_LEFT})
	if ev.CursorLine != 0 || ev.CursorPos != 5 {
		t.Errorf("Cross-line Left failed: expected Line 0, Pos 5. Got Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}
}

func TestEditorView_EnterAndBackspaceMerging(t *testing.T) {
	pt := piecetable.New([]byte("ABC"))
	ev := NewEditorView(pt)
	ev.CursorPos = 1 // Между A и B

	// 1. Нажимаем Enter -> разрыв строки "A" и "BC"
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_RETURN})
	if pt.String() != "A\nBC" {
		t.Errorf("Enter splitting failed: expected 'A\\nBC', got %q", pt.String())
	}
	if ev.CursorLine != 1 || ev.CursorPos != 0 {
		t.Errorf("Cursor position after Enter wrong: Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}

	// 2. Нажимаем Backspace в начале второй строки -> склейка обратно в "ABC"
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_BACK})
	if pt.String() != "ABC" {
		t.Errorf("Backspace merging failed: expected 'ABC', got %q", pt.String())
	}
	if ev.CursorLine != 0 || ev.CursorPos != 1 {
		t.Errorf("Cursor position after merge wrong: Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}
}

func TestEditorView_StickyColumn(t *testing.T) {
	// Создаем текст:
	// LongLine (8)
	// Short (5)
	// LongLine (8)
	pt := piecetable.New([]byte("LongLine\nShort\nLongLine"))
	ev := NewEditorView(pt)

	// Встаем в конец первой длинной строки
	ev.CursorLine = 0
	ev.CursorPos = 8
	ev.DesiredCursorPos = 8

	// 1. Вниз на короткую строку -> визуально в конце (5), но желаемая позиция остается 8
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
	if ev.CursorPos != 5 {
		t.Errorf("Down to short line: expected pos 5, got %d", ev.CursorPos)
	}
	if ev.DesiredCursorPos != 8 {
		t.Errorf("Desired position lost! Expected 8, got %d", ev.DesiredCursorPos)
	}

	// 2. Вниз на длинную строку -> позиция должна восстановиться до 8
	ev.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
	if ev.CursorLine != 2 || ev.CursorPos != 8 {
		t.Errorf("Sticky column failed: expected Line 2, Pos 8. Got Line %d, Pos %d", ev.CursorLine, ev.CursorPos)
	}
}