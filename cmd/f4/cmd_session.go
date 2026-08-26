package main

import (
	"strings"
	"sync"
	"time"

	"github.com/unxed/vtui"
)

// cmdShellSession decides when the local cmd.exe has finished the line f4
// typed into it.
//
// Why a prompt mark alone cannot decide it (issue #409): f4 injects PROMPT so
// that cmd prints an OSC 133 mark with every prompt. But cmd also prints the
// prompt in front of every line of a batch file that runs with ECHO on — so
// the first line of foo.bat looks exactly like "command finished", the panels
// come back and the batch keeps running behind them. cmd interprets batch
// files in-process, so a child-process check is blind to them too.
//
// What tells a real prompt apart is that nothing follows it: after an echoed
// batch line the command text and a line break follow within the same frame,
// while a prompt that waits for input leaves the cursor resting right after
// the prompt text. The session therefore ends an execution only when a prompt
// mark (B, printed after $P$G) has arrived since the command was sent, the
// cursor has rested on that prompt for cmdPromptSettleDelay, the shell has no
// child process (a nested cmd or python prints the same prompt) and the
// console title does not say that cmd is still running something ("<title> -
// <command>", which ConPTY forwards).
//
// The session is only created for the local Windows shell; the remote FISH+
// peer and Unix shells keep their own completion signals.
type cmdShellSession struct {
	mu sync.Mutex
	pf *PanelsFrame

	// promptSeq counts every prompt-end mark; sentSeq is its value when the
	// line now running was typed. A prompt can only end that line if it was
	// printed after it — a startup prompt still crossing ConPTY does not
	// count.
	promptSeq uint64
	sentSeq   uint64
	pending   bool // a typed line (command or directory sync) has no prompt yet
	idleTitle string
	prompt    promptSnapshot
	timer     *time.Timer
	closed    bool
}

// cmdPromptSettleDelay is how long the cursor must rest on a prompt before it
// counts as the shell waiting for input. ConPTY renders the text that follows
// an echoed batch prompt within a frame or two; a long silence is a prompt.
var cmdPromptSettleDelay = 150 * time.Millisecond

// cmdPromptRecheckDelay is the poll interval while a settled prompt is vetoed
// by a child process or the title: a nested shell that exits later leaves a
// prompt that has already settled, so it has to be looked at again.
var cmdPromptRecheckDelay = 250 * time.Millisecond

// windowsShellPrompt marks the prompt start and end. The prompt end mark is
// printed after $P$G, so the cursor position at its arrival is the position
// the shell reads input from.
const windowsShellPrompt = `$E]133;A$E\$P$G$E]133;B$E\`

func newCmdShellSession(pf *PanelsFrame) *cmdShellSession {
	return &cmdShellSession{pf: pf}
}

// noteSent records that a line was typed into the shell and that the
// prompt f4 is waiting for has not been printed yet.
func (s *cmdShellSession) noteSent() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.pending = true
	s.sentSeq = s.promptSeq
	if s.promptSeq == 0 {
		// No prompt has been seen yet, so the shell's startup prompt is
		// still on its way and will arrive after the line: it is not the
		// answer to it.
		s.sentSeq = 1
	}
	s.mu.Unlock()
	s.pf.noteLocalShellBusy(true)
}

// idle reports whether every typed line has been answered by a settled
// prompt, i.e. whether the shell is known to be reading input.
func (s *cmdShellSession) idle() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.pending
}

func (s *cmdShellSession) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
	}
	s.mu.Unlock()
}

// handleMark runs on the PTY goroutine for every OSC 133 mark of the local
// shell.
func (s *cmdShellSession) handleMark(mark string, snap promptSnapshot) {
	if s == nil || mark != "B" {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.promptSeq++
	s.prompt = snap
	seq := s.promptSeq
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(cmdPromptSettleDelay, func() { s.settle(seq) })
	s.mu.Unlock()
}

// settle checks, on the UI goroutine, whether prompt seq is the shell waiting
// for input, and if so ends whatever f4 was waiting for.
func (s *cmdShellSession) settle(seq uint64) {
	manager := vtui.FrameManager
	if manager == nil {
		return
	}
	manager.PostTask(func() {
		s.mu.Lock()
		if s.closed || seq != s.promptSeq {
			s.mu.Unlock()
			return
		}
		snap, sentSeq, pending := s.prompt, s.sentSeq, s.pending
		s.mu.Unlock()

		pf := s.pf
		if pf.termView == nil || !pf.termView.CursorRestsOnPrompt(snap) {
			return
		}
		vtui.DebugLog("CMD_SESSION: prompt %d settled (sent=%d pending=%v)", seq, sentSeq, pending)
		if pending && seq <= sentSeq {
			// This prompt was printed before the line we typed; the shell has
			// not even started on it.
			return
		}
		vetoed := false
		if pty := pf.localPTY(); pty != nil && pty.IsBusy() {
			vetoed = true
		}
		if !vetoed && s.titleSaysRunning(pf.termView.Title) {
			vetoed = true
		}
		if vetoed {
			vtui.DebugLog("CMD_SESSION: prompt %d vetoed (child or title), rechecking", seq)
			s.mu.Lock()
			if !s.closed && seq == s.promptSeq {
				s.timer = time.AfterFunc(cmdPromptRecheckDelay, func() { s.settle(seq) })
			}
			s.mu.Unlock()
			return
		}

		s.mu.Lock()
		s.pending = false
		s.idleTitle = pf.termView.Title
		s.mu.Unlock()
		pf.shellPromptReady = true
		pf.ignoreNextPrompt = false
		if pf.executing {
			pf.endExecution()
		}
		pf.noteLocalShellBusy(false)
		pf.catchUpProcessEnvironment(true)
	})
}

// titleSaysRunning recognizes the "<title> - <command>" form cmd gives the
// console title while it runs an external command or a batch file.
func (s *cmdShellSession) titleSaysRunning(title string) bool {
	s.mu.Lock()
	base := s.idleTitle
	s.mu.Unlock()
	if base == "" || title == base {
		return false
	}
	return strings.HasPrefix(title, base+" - ")
}
