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
			got := TranslateInput(tt.e, false)
			if got != tt.want {
				t.Errorf("TranslateInput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTranslateInput_Win32(t *testing.T) {
	e := &vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		VirtualKeyCode:  65,
		VirtualScanCode: 30,
		Char:            'A',
		KeyDown:         true,
		ControlKeyState: 8,
		RepeatCount:     1,
	}
	res := TranslateInput(e, true)
	expected := "\x1b[65;30;65;1;8;1_"
	if res != expected {
		t.Errorf("Expected %q, got %q", expected, res)
	}
}

func TestTranslateInput_Win32KeyUp(t *testing.T) {
	// Test release of Left Control (VK_CONTROL = 0x11 = 17)
	e := &vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		VirtualKeyCode:  17,
		VirtualScanCode: 29,
		Char:            0,
		KeyDown:         false, // KeyUp!
		ControlKeyState: 0,
		RepeatCount:     1,
	}
	res := TranslateInput(e, true)
	// CSI Vk ; Sc ; Uc ; Kd ; Cs ; Rc _
	// Kd must be 0 for release
	expected := "\x1b[17;29;0;0;0;1_"
	if res != expected {
		t.Errorf("Expected %q for Win32 KeyUp, got %q", expected, res)
	}
}
