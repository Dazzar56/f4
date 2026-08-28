package main

import (
	"sync"
	"time"
)

// terminalRedrawInterval bounds how often a burst of PTY output can wake the
// UI renderer. PTY data is still parsed immediately; only redundant frame
// requests are coalesced while the renderer is catching up.
const terminalRedrawInterval = 16 * time.Millisecond

type terminalRedrawScheduler struct {
	mu       sync.Mutex
	pending  bool
	trailing bool
	stopped  bool
	redraw   func()
}

func newTerminalRedrawScheduler(redraw func()) *terminalRedrawScheduler {
	return &terminalRedrawScheduler{redraw: redraw}
}

func (s *terminalRedrawScheduler) request() {
	if s == nil {
		return
	}

	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	if s.pending {
		// The parser has already applied this output to the terminal model.
		// Remember that the renderer must get one more frame after the current
		// coalescing window, or the model and the visible screen can diverge.
		s.trailing = true
		s.mu.Unlock()
		return
	}
	s.pending = true
	redraw := s.redraw
	s.mu.Unlock()

	// Keep the first frame of a burst responsive, then suppress further
	// requests until the interval expires. FrameManager.Redraw itself is
	// asynchronous and non-blocking, so this is safe from the PTY reader.
	if redraw != nil {
		redraw()
	}
	time.AfterFunc(terminalRedrawInterval, s.release)
}

// release ends one redraw window. If output arrived while that window was
// active, issue a trailing frame and keep coalescing until that frame's own
// window is complete.
func (s *terminalRedrawScheduler) release() {
	s.mu.Lock()
	if s.stopped {
		s.pending = false
		s.trailing = false
		s.mu.Unlock()
		return
	}
	if !s.trailing {
		s.pending = false
		s.mu.Unlock()
		return
	}
	s.trailing = false
	redraw := s.redraw
	s.mu.Unlock()

	if redraw != nil {
		redraw()
	}
	time.AfterFunc(terminalRedrawInterval, s.release)
}

func (s *terminalRedrawScheduler) stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopped = true
	s.pending = false
	s.trailing = false
	s.mu.Unlock()
}
