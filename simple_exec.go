package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/unxed/vtui"
)

// waitForAnyKey reads a single keystroke immediately using _getch on Windows/Wine or stdin read on Unix.
var waitForAnyKey = func() {
	if runtime.GOOS == "windows" {
		mod := os.Getenv("COMSPEC")
		_ = mod
		if proc := modMsvcrtProc(); proc != nil {
			proc.Call()
			return
		}
	}
	var buf [1]byte
	_, _ = os.Stdin.Read(buf[:])
}

func modMsvcrtProc() interface {
	Call(...uintptr) (uintptr, uintptr, error)
} {
	return modMsvcrtProcImpl()
}

// runSimpleInlineCommand executes a command directly in the host console without a PTY
// by suspending vtui, running the command with inherited stdio, waiting for a keypress,
// and restoring vtui.
func (pf *PanelsFrame) runSimpleInlineCommand(dir, command string) {
	shell := GetSystemShell()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command(shell, "/c", command)
	} else {
		cmd = exec.Command(shell, "-c", command)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if dir != "" {
		cmd.Dir = dir
	}

	// Always clear the overlay before running the command so output
	// does not scroll trailing keybar or command-line cells into history.
	pf.clearConsoleOverlay()

	inConsoleView := !pf.showPanels && pf.shellMode == ShellModeSimpleInline &&
		pf.consoleStyle() == ConsoleViewFar

	vtui.Suspend()
	_ = cmd.Run()

	if inConsoleView {
		// Snapshot the console now, while the command's output is still the
		// visible content of hStdOut. Without this, clearConsoleViewBackground()
		// finds no saved buffer on the next Ctrl+O round-trip and blanks the
		// whole window instead of restoring it (the exact bug this comment
		// used to sit next to, minus the missing capture).
		captureHostConsoleBuffer(pf.lastW, pf.lastH)

		// Busy is still set, so Resume() cannot repaint the panels over the
		// console before we switch back to it.
		vtui.Resume()
		vtui.SetAltScreen(false)
		pf.SetBusy(true)
		pf.drawConsoleOverlay()
		return
	}

	fmt.Print("\r\nPress any key to return to f4...")
	waitForAnyKey()

	captureHostConsoleBuffer(pf.lastW, pf.lastH)

	vtui.Resume()
	if vtui.FrameManager != nil {
		vtui.FrameManager.HardRefresh()
	}
	pf.RefreshAll()
}

func captureHostConsoleBuffer(w, h int) {
	captureHostConsoleBufferImpl(w, h)
}

func restoreHostConsoleBuffer() {
	restoreHostConsoleBufferImpl()
}

// restoreHostConsoleBufferIfSize blits the saved console snapshot back only when
// it still matches the current screen. Restoring a stale, differently sized
// snapshot leaves fragments of the old panels hanging over the command output.
func restoreHostConsoleBufferIfSize(w, h int) {
	if hostConsoleBufferMatches(w, h) {
		restoreHostConsoleBufferImpl()
	}
}

// runSimpleCapturedCommand executes a command via LocalCommandRunner and displays
// the streaming output in a scrollable f4 window.
func (pf *PanelsFrame) runSimpleCapturedCommand(dir, command string) {
	runner := NewLocalCommandRunner()
	showRemoteCommandOutput(pf, runner, dir, command)
}
