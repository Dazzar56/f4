package main

import (
	"testing"
	"time"

	"github.com/unxed/vtui"
	"github.com/unxed/vtui/vreactive"
)

// waitForDirectoryLoads blocks until no directory-load worker is running
// anywhere in the process.
//
// The workers read vtui.FrameManager and AppConfig while they run, so a test
// that replaces either one has to know they are all finished first. Panels are
// created deep inside PanelsFrame.ResizeConsole as well as directly, so the
// caller usually has no panel to wait on and this asks the question globally
// instead.
func waitForDirectoryLoads(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		directoryLoadWorkers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for the directory-load workers to stop")
	}
}

// swapFrameManager replaces the global vtui.FrameManager with a fresh,
// independent instance and returns a function that restores the original
// pointer. In particular, the fresh manager has its own task queue: Init
// deliberately preserves an existing queue, which is useful in production
// but lets queued UI work escape from one test into the next.
func swapFrameManager(t *testing.T) func() {
	t.Helper()
	// A directory-load worker left running by an earlier test reads the
	// manager this is about to replace, which the race detector reports
	// against whichever test is unlucky enough to do the replacing. Joining
	// them first is what makes the swap safe.
	waitForDirectoryLoads(t)
	old := vtui.FrameManager
	oldUpdateQueue := vreactive.GlobalUpdateQueue
	oldAnimationManager := vreactive.GlobalAnimationManager
	vtui.FrameManager = vtui.NewFrameManager()

	return func() {
		waitForDirectoryLoads(t)
		vtui.FrameManager = old
		vreactive.GlobalUpdateQueue = oldUpdateQueue
		vreactive.GlobalAnimationManager = oldAnimationManager
	}
}
