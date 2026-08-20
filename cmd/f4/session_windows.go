//go:build windows

package main

import (
	"fmt"
	"os"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

func SupportsBackgrounding() bool {
	return false
}

type SessionInfo struct {
	PID      int
	Title    string
	SockPath string
}

func listSessions() []SessionInfo {
	return nil
}

func runSessionPicker(sessions []SessionInfo) *SessionInfo {
	return nil
}

func ManageSessions() {
	stopWindowAppearanceManager := startWindowsConsoleWindowAppearanceManager()
	defer stopWindowAppearanceManager()

	InitCore()

	restore, err := vtui.PrepareTerminal()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}
	if restore != nil {
		defer restore()
	}

	// Unlike the Unix build (session_unix.go's runServer, attach time),
	// there's no separate daemon/client split here to defer this past --
	// this *is* the one and only session, already fully up (panels pushed
	// inside InitCore(), terminal just prepared above), so opening right
	// here is the direct equivalent of that hook. openDashEFileIfRequested
	// itself no-ops when -e wasn't given.
	openDashEFileIfRequested()

	reader := vtinput.NewReader(os.Stdin, false)
	vtui.FrameManager.Run(reader)
}

func runServer(sockPath string)                {}
func runClient(sockPath string, serverPID int) {}
