package main

import (
	"testing"
	"time"

	"github.com/unxed/vtui"
)

// drainUITasks runs everything queued on the frame manager, the way
// FrameManager.Run would. The queue is process-wide, so it may also carry
// leftovers from other tests; draining it is what keeps this one honest.
func drainUITasks(d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// runBusyChange delivers an OSC 133 C/D transition and settles the UI tasks
// it posts.
func runBusyChange(pf *PanelsFrame, busy bool) {
	pf.termView.OnBusyChange(busy)
	drainUITasks(50 * time.Millisecond)
}

func newExecutionTestFrame(t *testing.T) *PanelsFrame {
	t.Helper()
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	drainUITasks(10 * time.Millisecond) // discard anything an earlier test queued
	pf := NewPanelsFrame()
	t.Cleanup(pf.Close)
	pf.showPanels = false
	return pf
}

// A command f4 wrapped in its own OSC 133 C/D pair ends the moment its D
// marker arrives, even though no prompt marker was ever seen before it.
// Shells that do not mark their prompts (every Unix shell: f4 injects PROMPT
// on Windows only) never set shellPromptReady on their own, so treating that
// first D as a stale startup prompt left pf.executing stuck on forever after
// the first command of the session. With a busy terminal the command line and
// the keybar stay hidden, TerminalQuiet stays false so F3/F4 no longer open
// the terminal log, and every keystroke is forwarded raw to the PTY.
func TestWrappedCommandCompletionEndsExecution(t *testing.T) {
	pf := newExecutionTestFrame(t)

	pf.beginManagedExecution()
	runBusyChange(pf, true)
	runBusyChange(pf, false)

	if pf.executing {
		t.Fatal("executing is still set after the command's own OSC 133 D marker")
	}
}

// The startup prompt of a prompt-marking shell can still be crossing ConPTY
// when the command is sent. Consuming it would end the execution while the
// command is only just starting, so the first marker is discarded and the
// real prompt that follows the command ends it.
func TestPromptDrivenCommandIgnoresStartupPrompt(t *testing.T) {
	pf := newExecutionTestFrame(t)

	pf.beginPromptDrivenExecution()
	runBusyChange(pf, false) // stale prompt from shell startup
	if !pf.executing {
		t.Fatal("a stale startup prompt ended the execution")
	}

	runBusyChange(pf, false) // prompt printed after the command finished
	if pf.executing {
		t.Fatal("executing is still set after the command's prompt marker")
	}
}

// Once a prompt has been seen, no marker is stale any more: the next
// prompt-driven command ends on its very first marker.
func TestPromptDrivenCommandAfterPromptSeen(t *testing.T) {
	pf := newExecutionTestFrame(t)

	runBusyChange(pf, false) // shell startup prompt, no command running
	pf.beginPromptDrivenExecution()
	runBusyChange(pf, false)

	if pf.executing {
		t.Fatal("executing is still set after the command's prompt marker")
	}
}
