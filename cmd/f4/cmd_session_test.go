package main

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/unxed/vtui"
)

const testPrompt = "\x1b]133;A\x1b\\C:\\work>\x1b]133;B\x1b\\"

// newCmdSessionFrame builds a panels frame whose terminal is driven by the
// cmd session, the way NewPanelsFrame does on Windows, and shortens the
// settle delay so the tests do not wait for real ConPTY timings.
func newCmdSessionFrame(t *testing.T) *PanelsFrame {
	t.Helper()
	oldSettle, oldRecheck := cmdPromptSettleDelay, cmdPromptRecheckDelay
	cmdPromptSettleDelay, cmdPromptRecheckDelay = 20*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { cmdPromptSettleDelay, cmdPromptRecheckDelay = oldSettle, oldRecheck })

	t.Cleanup(swapFrameManager(t))
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	pf := setupMockPanelsFrame(t)
	t.Cleanup(pf.Close)
	pf.cmdSession = newCmdShellSession(pf)
	pf.termView.OnShellMark = func(mark string, snap promptSnapshot) { pf.cmdSession.handleMark(mark, snap) }
	pf.parser = NewAnsiParser(pf.termView, nil)
	return pf
}

func feedAndWait(pf *PanelsFrame, data string, wait time.Duration) {
	pf.parser.Process([]byte(data))
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		drainUITasks()
		time.Sleep(5 * time.Millisecond)
	}
}

// A batch file with ECHO on prints the prompt in front of every line it
// runs. Those prompts must not be taken for the one that follows the batch
// (issue #409): the command text and the line break after them move the
// cursor away, so they never settle.
func TestCmdSessionBatchEchoPromptsDoNotEndExecution(t *testing.T) {
	pf := newCmdSessionFrame(t)
	feedAndWait(pf, "Microsoft Windows\r\n\r\n"+testPrompt, 60*time.Millisecond)
	if !pf.cmdSession.idle() {
		t.Fatal("startup prompt did not settle")
	}

	pf.executing = true
	pf.returnToPanels = true
	pf.showPanels = false
	pf.cmdSession.noteSent()

	// Echo of the typed line, then two batch lines with their prompts.
	feedAndWait(pf, "foo.bat\r\n\r\n"+testPrompt+"echo started\r\nstarted\r\n\r\n", 60*time.Millisecond)
	feedAndWait(pf, testPrompt+"timeout /t 5\r\nWaiting for 5 seconds...", 60*time.Millisecond)
	if !pf.executing || pf.showPanels {
		t.Fatal("an echoed batch prompt ended the execution")
	}

	// The real prompt after the batch: nothing follows it.
	feedAndWait(pf, "\r\n\r\n"+testPrompt, 80*time.Millisecond)
	if pf.executing || !pf.showPanels {
		t.Fatal("the settled prompt after the batch did not return the panels")
	}
}

// A startup prompt that crosses ConPTY after the command was typed was not
// printed for that command and cannot end it.
func TestCmdSessionStalePromptDoesNotEndExecution(t *testing.T) {
	pf := newCmdSessionFrame(t)
	pf.executing = true
	pf.cmdSession.noteSent()
	// The shell is still printing its startup prompt; the typed line has
	// not been echoed yet (it is queued in the console input).
	feedAndWait(pf, testPrompt, 60*time.Millisecond)
	if !pf.executing {
		t.Fatal("a prompt printed before the command was accepted as its end")
	}
	// Now the echo, the output and the prompt that belongs to the command.
	feedAndWait(pf, "dir\r\nfiles...\r\n\r\n"+testPrompt, 80*time.Millisecond)
	if pf.executing {
		t.Fatal("the prompt after the command did not end it")
	}
}

// A prompt that has settled is looked at again: a nested shell prints the
// same prompt, and the parent's prompt only counts once the child is gone.
func TestCmdSessionChildProcessVetoesSettledPrompt(t *testing.T) {
	pf := newCmdSessionFrame(t)
	busy := &toggleBusyPty{}
	pf.pty = busy
	feedAndWait(pf, testPrompt, 60*time.Millisecond)
	busy.busy.Store(true)
	pf.executing = true
	pf.cmdSession.noteSent()
	feedAndWait(pf, "cmd\r\n"+testPrompt, 80*time.Millisecond)
	if !pf.executing {
		t.Fatal("a prompt of a running child shell ended the execution")
	}
	busy.busy.Store(false)
	feedAndWait(pf, "", 100*time.Millisecond)
	if pf.executing {
		t.Fatal("the prompt was not reconsidered after the child exited")
	}
}

// While cmd runs something it appends the command to the console title.
// ConPTY forwards the title, and a prompt printed under such a title is a
// batch line, not the shell waiting for input.
func TestCmdSessionRunningTitleVetoesPrompt(t *testing.T) {
	pf := newCmdSessionFrame(t)
	feedAndWait(pf, "\x1b]0;C:\\Windows\\system32\\cmd.exe\x07"+testPrompt, 60*time.Millisecond)
	if !pf.cmdSession.idle() {
		t.Fatal("startup prompt did not settle")
	}
	pf.executing = true
	pf.cmdSession.noteSent()
	feedAndWait(pf, "foo.bat\r\n\x1b]0;C:\\Windows\\system32\\cmd.exe - foo.bat\x07"+testPrompt, 80*time.Millisecond)
	if !pf.executing {
		t.Fatal("a prompt printed while the title says the batch is running ended the execution")
	}
	feedAndWait(pf, "\x1b]0;C:\\Windows\\system32\\cmd.exe\x07", 80*time.Millisecond)
	if pf.executing {
		t.Fatal("the prompt was not accepted once the title was restored")
	}
}

// The directory sync typed into the shell is a line like any other: no
// second sync may be typed until its prompt has settled.
func TestCmdSessionSyncWaitsForPrompt(t *testing.T) {
	pf := newCmdSessionFrame(t)
	feedAndWait(pf, testPrompt, 60*time.Millisecond)
	pf.cmdSession.noteSent()
	if pf.cmdSession.idle() {
		t.Fatal("session reported idle with a line outstanding")
	}
	feedAndWait(pf, "cd /d \"C:\\work\" & rem f4_sync\r\n\r\n"+testPrompt, 80*time.Millisecond)
	if !pf.cmdSession.idle() {
		t.Fatal("session did not become idle after the prompt settled")
	}
}

type toggleBusyPty struct {
	mockPty
	busy atomic.Bool
}

func (p *toggleBusyPty) IsBusy() bool { return p.busy.Load() }

// A prompt whose cursor has moved on must not strand the session. Nothing
// clears pf.executing except the settle path, and isPtyBusy reports executing
// as busy, so a wait that never ends disables every hotkey gated on the
// terminal being quiet -- Esc stops toggling the panels while Ctrl+O, which is
// not gated, keeps working. That asymmetry is the symptom to look for.
func TestCmdSessionReleasesAWaitThatNeverSettles(t *testing.T) {
	pf := newCmdSessionFrame(t)
	oldMax := cmdPromptMaxAttempts
	cmdPromptMaxAttempts = 3
	t.Cleanup(func() { cmdPromptMaxAttempts = oldMax })

	feedAndWait(pf, testPrompt, 60*time.Millisecond)
	pf.executing = true
	pf.cmdSession.noteSent()

	// A prompt arrives, but output keeps coming so the cursor never rests on
	// it: the screen is being rewritten under the settle check.
	feedAndWait(pf, "cmd\r\n"+testPrompt+"more output follows", 300*time.Millisecond)

	if pf.executing {
		t.Error("executing stayed set: the terminal would be stuck busy, with Esc dead and Ctrl+O alive")
	}
	if !pf.cmdSession.idle() {
		t.Error("the session never released its wait")
	}
}
