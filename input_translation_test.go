package main

import (
	"testing"

	"github.com/unxed/vtinput"
)

func TestTranslateInput(t *testing.T) {
	tests := []struct {
		name string
		e    *vtinput.InputEvent
		want string
	}{
		{"Char 'a'", &vtinput.InputEvent{Char: 'a'}, "a"},
		{"Ctrl+a", &vtinput.InputEvent{Char: 'a', ControlKeyState: vtinput.LeftCtrlPressed}, string(rune(1))},
		{"Up Arrow", &vtinput.InputEvent{VirtualKeyCode: vtinput.VK_UP}, "\x1b[A"},
		{"Shift+Up Arrow", &vtinput.InputEvent{VirtualKeyCode: vtinput.VK_UP, ControlKeyState: vtinput.ShiftPressed}, "\x1b[1;2A"},
		{"F1", &vtinput.InputEvent{VirtualKeyCode: vtinput.VK_F1}, "\x1bOP"},
		{"F5", &vtinput.InputEvent{VirtualKeyCode: vtinput.VK_F5}, "\x1b[15~"},
		{"Alt+a", &vtinput.InputEvent{Char: 'a', ControlKeyState: vtinput.LeftAltPressed}, "\x1ba"},
		{"Alt+Enter", &vtinput.InputEvent{VirtualKeyCode: vtinput.VK_RETURN, ControlKeyState: vtinput.LeftAltPressed}, "\x1b\r"},
		{"Standalone Modifier", &vtinput.InputEvent{VirtualKeyCode: vtinput.VK_SHIFT}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TranslateInput(tt.e)
			if got != tt.want {
				t.Errorf("TranslateInput() = %q, want %q", got, tt.want)
			}
		})
	}
}