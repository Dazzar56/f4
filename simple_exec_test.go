package main

import (
	"testing"
	"time"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

func TestSimpleInline_CommandExecution(t *testing.T) {
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)
	SetDefaultF4Palette()

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeSimpleInline
	pf.ResizeConsole(80, 25)

	oldWait := waitForAnyKey
	waitForAnyKey = func() {}
	defer func() { waitForAnyKey = oldWait }()

	pf.runSimpleInlineCommand(t.TempDir(), "echo simple_inline_test")
}

func TestSimpleCaptured_CommandExecution(t *testing.T) {
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)
	SetDefaultF4Palette()

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeSimpleCaptured
	pf.ResizeConsole(80, 25)

	pf.runSimpleCapturedCommand(t.TempDir(), "echo simple_captured_test")
	top := vtui.FrameManager.GetTopFrame()
	if top == nil || top.GetType() != vtui.TypeDialog {
		t.Fatal("runSimpleCapturedCommand should open a captured output dialog")
	}
}

func TestSimpleInline_ToggleAndAnyKeyReturn(t *testing.T) {
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)
	SetDefaultF4Palette()

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeSimpleInline
	pf.ResizeConsole(80, 25)
	vtui.FrameManager.Push(pf)

	// 1. Panel.Toggle hides panels in view-only primary screen
	RunAction("Panel.Toggle")
	if pf.showPanels {
		t.Fatal("Panel.Toggle should hide panels in SimpleInline mode")
	}

	// 2. Any key in SimpleInline with hidden panels returns to panels
	pressKey(pf, &vtinput.InputEvent{
		Type:    vtinput.KeyEventType,
		KeyDown: true,
		Char:    ' ',
	})
	if !pf.showPanels {
		t.Fatal("Any keypress while viewing primary screen in SimpleInline mode must restore panels")
	}
}

// TestSimpleInline_CtrlOKeyUpDoesNotRestorePanels reproduces a flicker seen
// under Wine: Ctrl+O's KeyDown correctly hides the panels via the hotkey
// dispatcher, but its trailing KeyUp event (delivered as a separate
// InputEvent, e.KeyDown == false) used to fall through to the "any key
// returns to panels" fallback below unfiltered, immediately undoing the
// toggle within the same keystroke.
func TestSimpleInline_CtrlOKeyUpDoesNotRestorePanels(t *testing.T) {
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)
	SetDefaultF4Palette()

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeSimpleInline
	pf.ResizeConsole(80, 25)
	vtui.FrameManager.Push(pf)

	RunAction("Panel.Toggle")
	if pf.showPanels {
		t.Fatal("Panel.Toggle should hide panels in SimpleInline mode")
	}

	// The KeyUp of the same Ctrl+O press that triggered the toggle above.
	pressKey(pf, &vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		KeyDown:         false,
		VirtualKeyCode:  vtinput.VK_O,
		Char:            0x0F,
		ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if pf.showPanels {
		t.Fatal("Ctrl+O's KeyUp event must not restore panels on its own")
	}

	// A genuine subsequent keypress should still restore panels as normal.
	pressKey(pf, &vtinput.InputEvent{
		Type:    vtinput.KeyEventType,
		KeyDown: true,
		Char:    ' ',
	})
	if !pf.showPanels {
		t.Fatal("A real keypress after Ctrl+O's KeyUp should still restore panels")
	}
}

func TestSimpleCaptured_ToggleShowsToast(t *testing.T) {
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)
	SetDefaultF4Palette()

	// Drain any stale tasks left over from a previous test sharing the
	// global TaskChan/FrameManager, so we don't pick up someone else's
	// queued toast below.
drain:
	for {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		default:
			break drain
		}
	}

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.shellMode = ShellModeSimpleCaptured
	pf.ResizeConsole(80, 25)
	vtui.FrameManager.Push(pf)

	RunAction("Panel.Toggle")
	if !pf.showPanels {
		t.Fatal("Panel.Toggle should not hide panels in SimpleCaptured mode")
	}

	// ShowToast is posted to the UI task queue; pump it like the main loop would.
	var toast string
	timeout := time.After(1 * time.Second)
Loop:
	for {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
			if toast = vtui.FrameManager.GetActiveToast(); toast != "" {
				break Loop
			}
		case <-timeout:
			t.Fatal("Timeout waiting for toast")
		}
	}
	if toast != Msg("Terminal.NotAvailableInEnv") {
		t.Errorf("Expected toast %q, got %q", Msg("Terminal.NotAvailableInEnv"), toast)
	}
}
