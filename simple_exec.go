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

	vtui.Suspend()
	_ = cmd.Run()

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

// runSimpleCapturedCommand executes a command via LocalCommandRunner and displays
// the streaming output in a scrollable f4 window.
func (pf *PanelsFrame) runSimpleCapturedCommand(dir, command string) {
	runner := NewLocalCommandRunner()
	showRemoteCommandOutput(pf, runner, dir, command)
}
