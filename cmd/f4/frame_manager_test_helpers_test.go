package main

import (
	"testing"

	"github.com/unxed/vtui"
	"github.com/unxed/vtui/vreactive"
)

// swapFrameManager replaces the global vtui.FrameManager with a fresh,
// independent instance and returns a function that restores the original
// pointer. In particular, the fresh manager has its own task queue: Init
// deliberately preserves an existing queue, which is useful in production
// but lets queued UI work escape from one test into the next.
func swapFrameManager(t *testing.T) func() {
	t.Helper()
	old := vtui.FrameManager
	oldUpdateQueue := vreactive.GlobalUpdateQueue
	oldAnimationManager := vreactive.GlobalAnimationManager
	vtui.FrameManager = vtui.NewFrameManager()

	return func() {
		vtui.FrameManager = old
		vreactive.GlobalUpdateQueue = oldUpdateQueue
		vreactive.GlobalAnimationManager = oldAnimationManager
	}
}
