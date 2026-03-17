package main

import (
	"os"
	"strings"
	"testing"

	"github.com/unxed/vtinput"
)

func TestMacroRecordingAndPlayback(t *testing.T) {
	tmpFile := "test_macros.ini"
	defer os.Remove(tmpFile)

	mgr := NewMacroManager(tmpFile)

	// Trigger recording start (Ctrl+.)
	ctrlDot := &vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		KeyDown:         true,
		VirtualKeyCode:  vtinput.VK_OEM_PERIOD,
		ControlKeyState: vtinput.LeftCtrlPressed,
	}

	if !mgr.Filter(ctrlDot) {
		t.Fatal("Ctrl+. should be filtered and start recording")
	}
	if !mgr.Recording {
		t.Fatal("Manager should be in recording state")
	}

	// Send a normal key 'A'
	keyA := &vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_A,
		Char:           'a',
	}
	mgr.Filter(keyA)

	if len(mgr.Buffer) != 1 {
		t.Fatalf("Expected 1 event in buffer, got %d", len(mgr.Buffer))
	}

	// Stop recording
	mgr.Filter(ctrlDot)
	if mgr.Recording {
		t.Fatal("Manager should stop recording")
	}

	// Simulate Assign Frame capturing Ctrl+F1
	ctrlF1 := &vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		KeyDown:         true,
		VirtualKeyCode:  vtinput.VK_F1,
		ControlKeyState: vtinput.LeftCtrlPressed,
	}

	assignFrame := &MacroAssignFrame{mgr: mgr}
	assignFrame.ProcessKey(ctrlF1)

	if _, ok := mgr.Macros[KeyStr(vtinput.VK_F1, vtinput.LeftCtrlPressed)]; !ok {
		t.Fatal("Macro should be saved with Ctrl+F1 key")
	}

	// Test reloading from file
	mgr2 := NewMacroManager(tmpFile)
	if _, ok := mgr2.Macros[KeyStr(vtinput.VK_F1, vtinput.LeftCtrlPressed)]; !ok {
		t.Fatal("Macro was not correctly loaded from INI file")
	}
}

func TestKeyNormalization(t *testing.T) {
	// Проверяем, что Left и Right Ctrl дают одинаковый ключ
	k1 := KeyStr(vtinput.VK_A, vtinput.LeftCtrlPressed)
	k2 := KeyStr(vtinput.VK_A, vtinput.RightCtrlPressed)
	if k1 != k2 {
		t.Errorf("Normalization failed: %s != %s", k1, k2)
	}

	// Проверяем комбинацию Ctrl+Shift
	k3 := KeyStr(vtinput.VK_B, vtinput.LeftCtrlPressed|vtinput.ShiftPressed)
	if !strings.Contains(k3, ":18") { // 0x08 (Ctrl) | 0x10 (Shift) = 0x18
		t.Errorf("Complex normalization failed: %s", k3)
	}
}

func TestMacroPlaybackLogic(t *testing.T) {
	mgr := NewMacroManager("unused.ini")

	// Создаем макрос: по нажатию F2 печатаем "hi"
	f2Key := KeyStr(vtinput.VK_F2, 0)
	macroSeq := []*vtinput.InputEvent{
		{Type: vtinput.KeyEventType, KeyDown: true, Char: 'h', VirtualKeyCode: vtinput.VK_H},
		{Type: vtinput.KeyEventType, KeyDown: true, Char: 'i', VirtualKeyCode: vtinput.VK_I},
	}
	mgr.Macros[f2Key] = macroSeq

	// Имитируем нажатие F2
	pressF2 := &vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_F2,
	}

	// Костыль для теста: перехватим InjectEvents через замену глобального FrameManager не получится легко,
	// но мы можем проверить, что Filter вернул true (событие поглощено для замены макросом)
	if !mgr.Filter(pressF2) {
		t.Error("Filter should return true when triggering a macro")
	}
}
