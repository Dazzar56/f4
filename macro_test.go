package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
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

	assignFrame := NewMacroAssignFrame(mgr)
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
	// Check that Left and Right Ctrl give same key
	k1 := KeyStr(vtinput.VK_A, vtinput.LeftCtrlPressed)
	k2 := KeyStr(vtinput.VK_A, vtinput.RightCtrlPressed)
	if k1 != k2 {
		t.Errorf("Normalization failed: %s != %s", k1, k2)
	}

	// Check Ctrl+Shift combination
	k3 := KeyStr(vtinput.VK_B, vtinput.LeftCtrlPressed|vtinput.ShiftPressed)
	if !strings.Contains(k3, ":18") { // 0x08 (Ctrl) | 0x10 (Shift) = 0x18
		t.Errorf("Complex normalization failed: %s", k3)
	}
}

func TestMacroPlaybackLogic(t *testing.T) {
	mgr := NewMacroManager("unused.ini")

	// Create macro: print "hi" on F2 press
	f2Key := KeyStr(vtinput.VK_F2, 0)
	macroSeq := []*vtinput.InputEvent{
		{Type: vtinput.KeyEventType, KeyDown: true, Char: 'h', VirtualKeyCode: vtinput.VK_H},
		{Type: vtinput.KeyEventType, KeyDown: true, Char: 'i', VirtualKeyCode: vtinput.VK_I},
	}
	mgr.Macros[f2Key] = macroSeq

	// Simulate F2 press
	pressF2 := &vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_F2,
	}

	// Hack for test: intercepting InjectEvents by replacing global FrameManager is not easy,
	// but we can check that Filter returned true (event consumed to be replaced by macro)
	if !mgr.Filter(pressF2) {
		t.Error("Filter should return true when triggering a macro")
	}
}
func TestMacro_FilterTriggerSwallowing_Order(t *testing.T) {
	mgr := NewMacroManager("unused.ini")
	mgr.Recording = true
	mgr.Buffer = make([]*vtinput.InputEvent, 0)

	// Trigger stop recording via Ctrl+.
	stopEvent := &vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		Char: '.', ControlKeyState: vtinput.LeftCtrlPressed,
	}

	res := mgr.Filter(stopEvent)

	if !res {
		t.Error("Filter should swallow the stop trigger even if recording is active")
	}
	if len(mgr.Buffer) != 0 {
		t.Errorf("Stop trigger should NOT be added to macro buffer, but buffer size is %d", len(mgr.Buffer))
	}
}
func TestMacro_TriggerSwallowing(t *testing.T) {
	mgr := NewMacroManager("unused.ini")

	// 1. Start recording via Ctrl+. (using Char for compatibility)
	startEvent := &vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		Char: '.', ControlKeyState: vtinput.LeftCtrlPressed,
	}
	mgr.Filter(startEvent)
	if !mgr.Recording {
		t.Fatal("Should be recording")
	}

	// 2. Type 'A'
	mgr.Filter(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, Char: 'a', VirtualKeyCode: vtinput.VK_A})

	// 3. Stop recording via Ctrl+.
	stopEvent := &vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		Char: '.', ControlKeyState: vtinput.LeftCtrlPressed,
	}
	res := mgr.Filter(stopEvent)

	if !res {
		t.Error("Stop trigger should be consumed (return true)")
	}
	if mgr.Recording {
		t.Error("Should have stopped recording")
	}

	// 4. Verify buffer: should ONLY contain 'a', NOT the trigger dot
	if len(mgr.Buffer) != 1 || mgr.Buffer[0].Char != 'a' {
		t.Errorf("Macro buffer polluted or incomplete. Items: %d", len(mgr.Buffer))
	}
}

func TestMacro_AssignRobustness(t *testing.T) {
	// Clean manager for testing
	mgr := &MacroManager{Macros: make(map[string][]*vtinput.InputEvent)}
	mgr.Buffer = []*vtinput.InputEvent{{Char: 'x', KeyDown: true}}
	f := NewMacroAssignFrame(mgr)

	// 1. Standalone modifiers should be ignored (dialog stays open)
	f.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_SHIFT})
	if f.Done {
		t.Error("Assign dialog should ignore standalone Shift")
	}

	// 2. Esc SHOULD now cancel the dialog without assignment
	f.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_ESCAPE})
	if !f.Done {
		t.Error("Assign dialog should close after pressing Esc")
	}

	escKey := KeyStr(vtinput.VK_ESCAPE, 0)
	if _, ok := mgr.Macros[escKey]; ok {
		t.Error("Esc should cancel, not assign a macro")
	}

	// 3. Test Alt+X assignment
	f.Done = false
	mgr.Buffer = []*vtinput.InputEvent{{Char: 'y', KeyDown: true}}
	f.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_X, ControlKeyState: vtinput.LeftAltPressed,
	})

	altXKey := KeyStr(vtinput.VK_X, vtinput.LeftAltPressed)
	if _, ok := mgr.Macros[altXKey]; !ok {
		t.Error("Macro failed to assign to Alt+X")
	}
}
func TestMacro_KeyUpConsumption(t *testing.T) {
	mgr := NewMacroManager("unused.ini")

	// Start recording
	ctrlDot := &vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_OEM_PERIOD, ControlKeyState: vtinput.LeftCtrlPressed,
	}
	mgr.Filter(ctrlDot)

	// Release trigger (KeyUp)
	ctrlDotUp := &vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: false,
		VirtualKeyCode: vtinput.VK_OEM_PERIOD, ControlKeyState: vtinput.LeftCtrlPressed,
	}

	if !mgr.Filter(ctrlDotUp) {
		t.Error("KeyUp for Ctrl+. should be consumed by the filter")
	}

	// Normal key release during recording should NOT be added to buffer
	keyAUp := &vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: false,
		VirtualKeyCode: vtinput.VK_A, Char: 'a',
	}
	mgr.Filter(keyAUp)
	if len(mgr.Buffer) != 0 {
		t.Errorf("KeyUp should not be recorded in macro buffer, got length %d", len(mgr.Buffer))
	}
}

func TestMacro_CancelEsc(t *testing.T) {
	tmpPath := filepath.Join(os.TempDir(), "esc.ini")
	os.Remove(tmpPath)
	defer os.Remove(tmpPath)

	mgr := NewMacroManager(tmpPath)
	mgr.Recording = true
	mgr.Buffer = []*vtinput.InputEvent{{Char: 'h', KeyDown: true}}

	assign := NewMacroAssignFrame(mgr)
	escEvent := &vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_ESCAPE,
	}

	assign.ProcessKey(escEvent)

	key := KeyStr(vtinput.VK_ESCAPE, 0)
	if _, ok := mgr.Macros[key]; ok {
		t.Error("Esc should cancel, not assign a macro")
	}
	if !assign.Done {
		t.Error("Assign frame should be Done after cancellation")
	}
}

func TestMacro_Clear(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "clear_macros.ini")
	mgr := NewMacroManager(tmpFile)

	// 1. Assign a macro first
	key := KeyStr(vtinput.VK_F3, 0)
	mgr.Macros[key] = []*vtinput.InputEvent{
		{Type: vtinput.KeyEventType, KeyDown: true, Char: 'x'},
	}
	mgr.Save()

	// 2. Simulate empty recording and assigning to F3 (to clear it)
	mgr.Buffer = nil // Empty recording

	assignFrame := NewMacroAssignFrame(mgr)
	assignFrame.ProcessKey(&vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_F3,
	})

	// 3. Verify it is deleted from active map
	if _, ok := mgr.Macros[key]; ok {
		t.Error("Macro should be completely deleted from map when assigned an empty buffer")
	}

	// 4. Verify it is deleted from saved file
	mgr2 := NewMacroManager(tmpFile)
	if _, ok := mgr2.Macros[key]; ok {
		t.Error("Cleared macro should not persist in the saved INI file")
	}
}

func TestMacro_CharTrigger(t *testing.T) {
	mgr := NewMacroManager("unused.ini")

	// Test trigger using Char instead of VK (for terminals that map dot differently)
	event := &vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		Char: '.', VirtualKeyCode: 0, ControlKeyState: vtinput.LeftCtrlPressed,
	}

	if !mgr.Filter(event) {
		t.Error("Macro recording should start via Char '.' detection")
	}
	if !mgr.Recording {
		t.Error("Manager failed to enter recording state via Char trigger")
	}
}
func TestMacro_AssignFrame_Structure(t *testing.T) {
	mgr := &MacroManager{Macros: make(map[string][]*vtinput.InputEvent)}
	f := NewMacroAssignFrame(mgr)

	// Check that it's a proper window with a child (the prompt text)
	if len(f.GetChildren()) == 0 {
		t.Error("MacroAssignFrame should have at least one child (prompt)")
	}

	// Validate Layout
	vtui.AssertLayout(t, f)

	// Verify focus logic: it should NOT allow Tab to cycle away
	// because any key (including Tab) must be captured as a macro.
	tabEvent := &vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_TAB,
	}

	mgr.Buffer = []*vtinput.InputEvent{{Char: 't', KeyDown: true}}
	handled := f.ProcessKey(tabEvent)

	if !handled {
		t.Error("MacroAssignFrame should handle (capture) Tab key")
	}
	if !f.IsDone() {
		t.Error("MacroAssignFrame should close after capturing a key")
	}

	// Verify that macro was assigned to Tab
	tabKey := KeyStr(vtinput.VK_TAB, 0)
	if _, ok := mgr.Macros[tabKey]; !ok {
		t.Error("Macro failed to assign to Tab key")
	}
}

func TestMacroKeyStrDistinguishesEnhancedKeys(t *testing.T) {
	// Standard Delete has the EnhancedKey modifier in modern protocols
	delKeyStr := KeyStr(vtinput.VK_DELETE, vtinput.EnhancedKey)

	// Numpad Delete (NumDel) does not have the EnhancedKey modifier
	numDelKeyStr := KeyStr(vtinput.VK_DELETE, 0)

	if delKeyStr == numDelKeyStr {
		t.Errorf("Expected different KeyStr representations for standard Del (%q) and NumDel (%q), but they are identical", delKeyStr, numDelKeyStr)
	}
}
