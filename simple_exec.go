package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/unxed/vtui"
)

// waitForAnyKey is a mockable hook for reading a pause keypress in SimpleInline mode.
var waitForAnyKey = func() {
	var buf [1]byte
	_, _ = os.Stdin.Read(buf[:])
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

	vtui.Resume()
	if vtui.FrameManager != nil {
		vtui.FrameManager.HardRefresh()
	}
	pf.RefreshAll()
}

// runSimpleCapturedCommand executes a command via LocalCommandRunner and displays
// the streaming output in a scrollable f4 window.
func (pf *PanelsFrame) runSimpleCapturedCommand(dir, command string) {
	runner := NewLocalCommandRunner()
	showRemoteCommandOutput(pf, runner, dir, command)
}
