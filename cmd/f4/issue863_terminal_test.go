package main

import (
	"testing"
	"time"
)

// TestIssue863TerminalOutputRequestsTrailingRedraw models a two-line command
// whose PTY output arrives in two reads. The second read must not be stranded
// behind the redraw coalescing window: the model may contain both lines while
// the screen still shows only the first one.
func TestIssue863TerminalOutputRequestsTrailingRedraw(t *testing.T) {
	redraws := make(chan struct{}, 2)
	scheduler := newTerminalRedrawScheduler(func() { redraws <- struct{}{} })

	scheduler.request()
	select {
	case <-redraws:
	case <-time.After(time.Second):
		t.Fatal("initial terminal redraw was not requested")
	}

	// This request is guaranteed to fall inside the coalescing interval: the
	// first request's callback is synchronous and the interval has not elapsed.
	scheduler.request()
	select {
	case <-redraws:
	case <-time.After(2 * terminalRedrawInterval):
		t.Fatal("terminal output arriving during redraw coalescing was dropped")
	}
	scheduler.stop()
}
