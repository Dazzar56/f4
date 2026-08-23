package ttyx

import (
	"os"
	"testing"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

// waitFor polls, because the event loop is a goroutine and the X server is on
// the other end of a socket: nothing here is synchronous.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// focus points the keyboard at a window and waits for the session to notice.
func (f *xFixture) focus(t *testing.T, win xproto.Window) {
	t.Helper()
	if err := xproto.SetInputFocusChecked(f.conn, xproto.InputFocusParent,
		win, xproto.TimeCurrentTime).Check(); err != nil {
		t.Fatalf("set focus: %v", err)
	}
}

// openOn makes a session that believes the fixture's window is the terminal.
func (f *xFixture) openOn(t *testing.T) *Session {
	t.Helper()
	env := map[string]string{
		"DISPLAY":  os.Getenv("DISPLAY"),
		"WINDOWID": itoa(uint32(f.term)),
	}
	s, err := open(func(k string) string { return env[k] }, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestSessionTracksFocusThroughTheEventLoop(t *testing.T) {
	f := newXFixture(t)
	s := f.openOn(t)

	f.focus(t, f.term)
	waitFor(t, "the focus to arrive", s.Focused)

	// A token has to reach whoever is waiting to redraw.
	select {
	case <-s.Changed():
	case <-time.After(2 * time.Second):
		t.Fatal("a focus change must be announced on Changed")
	}

	f.focus(t, f.root)
	waitFor(t, "the focus to leave", func() bool { return !s.Focused() })
}

func TestSessionGeometryFollowsTheWindow(t *testing.T) {
	f := newXFixture(t)
	s := f.openOn(t)

	if got, err := s.Geometry(); err != nil || got.X != 40 || got.Y != 60 {
		t.Fatalf("before the move: %+v %v", got, err)
	}

	err := xproto.ConfigureWindowChecked(f.conn, f.term,
		xproto.ConfigWindowX|xproto.ConfigWindowY,
		[]uint32{140, 260}).Check()
	if err != nil {
		t.Fatalf("move: %v", err)
	}

	waitFor(t, "the geometry to catch up", func() bool {
		g, err := s.Geometry()
		return err == nil && g.X == 140 && g.Y == 260
	})
}

// An override-redirect window is above everything, so it must not be on the
// screen while somebody else is.
func TestOverlayHidesWhileTheTerminalIsNotFocused(t *testing.T) {
	f := newXFixture(t)
	s := f.openOn(t)
	f.focus(t, f.term)
	waitFor(t, "the focus", s.Focused)

	ov, err := s.NewOverlay()
	if err != nil {
		t.Fatalf("overlay: %v", err)
	}
	defer ov.Close()
	if err := ov.Place(Rect{X: 50, Y: 70, W: 20, H: 20}); err != nil {
		t.Fatalf("place: %v", err)
	}
	if !ov.Visible() {
		t.Fatal("a placed overlay over a focused terminal is on the screen")
	}

	f.focus(t, f.root)
	waitFor(t, "the overlay to come down", func() bool { return !ov.Visible() })

	f.focus(t, f.term)
	waitFor(t, "the overlay to come back", ov.Visible)
}

// A picture has to stay over the terminal while the terminal is dragged.
func TestOverlayFollowsTheWindow(t *testing.T) {
	f := newXFixture(t)
	s := f.openOn(t)
	f.focus(t, f.term)
	waitFor(t, "the focus", s.Focused)

	ov, err := s.NewOverlay()
	if err != nil {
		t.Fatalf("overlay: %v", err)
	}
	defer ov.Close()
	if err := ov.Place(Rect{X: 50, Y: 70, W: 20, H: 20}); err != nil {
		t.Fatalf("place: %v", err)
	}

	err = xproto.ConfigureWindowChecked(f.conn, f.term,
		xproto.ConfigWindowX|xproto.ConfigWindowY,
		[]uint32{60, 90}).Check()
	if err != nil {
		t.Fatalf("move: %v", err)
	}

	// The window moved by twenty and thirty, so the overlay must too.
	waitFor(t, "the overlay to follow", func() bool {
		r := ov.Rect()
		return r.X == 70 && r.Y == 100
	})
}

// An overlay that was never placed must not appear when the focus arrives.
func TestOverlayStaysDownUntilItIsPlaced(t *testing.T) {
	f := newXFixture(t)
	s := f.openOn(t)

	ov, err := s.NewOverlay()
	if err != nil {
		t.Fatalf("overlay: %v", err)
	}
	defer ov.Close()

	f.focus(t, f.term)
	waitFor(t, "the focus", s.Focused)
	time.Sleep(50 * time.Millisecond)
	if ov.Visible() {
		t.Error("nobody asked for this overlay to be on the screen")
	}
}

// A grabbed combination has to arrive as an f4 key event, which is the whole
// point of taking it from the X server instead of from the terminal.
func TestGrabbedKeyArrives(t *testing.T) {
	f := newXFixture(t)
	if err := xtest.Init(f.conn); err != nil {
		t.Skipf("no XTEST on this server: %v", err)
	}
	s := f.openOn(t)
	f.focus(t, f.term)
	waitFor(t, "the focus", s.Focused)

	// XK_F5, which no terminal has trouble with, but which is a keysym like
	// any other as far as the grab is concerned.
	const xkF5 = 0xFFC2
	if err := s.GrabKeys([]Combo{{Keysym: xkF5}}); err != nil {
		t.Fatalf("grab: %v", err)
	}
	defer s.UngrabKeys()

	code, err := s.keycodesFor([]Combo{{Keysym: xkF5}})
	if err != nil || len(code) == 0 || code[0] == 0 {
		t.Skipf("this keyboard map has no F5: %v", err)
	}

	xtest.FakeInput(f.conn, xproto.KeyPress, byte(code[0]), 0, f.root, 0, 0, 0)
	xtest.FakeInput(f.conn, xproto.KeyRelease, byte(code[0]), 0, f.root, 0, 0, 0)
	f.conn.Sync()

	select {
	case ev := <-s.Keys():
		if ev == nil || !ev.KeyDown {
			t.Fatalf("expected a key down, got %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the grabbed key never arrived")
	}
}

// A grab is shared state on the X server. Holding one while somebody else is
// typing takes that key away from the whole desktop, so it has to go the
// moment the terminal loses the focus.
func TestGrabsAreDroppedWithTheFocus(t *testing.T) {
	f := newXFixture(t)
	s := f.openOn(t)
	f.focus(t, f.term)
	waitFor(t, "the focus", s.Focused)

	const xkF5 = 0xFFC2
	if err := s.GrabKeys([]Combo{{Keysym: xkF5}}); err != nil {
		t.Fatalf("grab: %v", err)
	}
	defer s.UngrabKeys()
	waitFor(t, "the grab to be taken", func() bool { return s.grabsHeld() })

	f.focus(t, f.root)
	waitFor(t, "the grab to be given back", func() bool { return !s.grabsHeld() })

	f.focus(t, f.term)
	waitFor(t, "the grab to be taken again", func() bool { return s.grabsHeld() })
}

func TestGrabKeysWithoutADisplay(t *testing.T) {
	var s Session
	if err := s.GrabKeys(nil); err != ErrNoDisplay {
		t.Errorf("a session with no connection cannot grab: %v", err)
	}
}

// A grid of thumbnails is one window with the gaps cut out of it, so that the
// captions between the tiles stay the terminal's.
func TestOverlayBoundsCutGaps(t *testing.T) {
	f := newXFixture(t)
	s := f.openOn(t)
	f.focus(t, f.term)
	waitFor(t, "the focus", s.Focused)

	ov, err := s.NewOverlay()
	if err != nil {
		t.Fatalf("overlay: %v", err)
	}
	defer ov.Close()
	if !ov.PassesInput() {
		t.Skip("no SHAPE extension on this server")
	}
	if err := ov.Place(Rect{X: 100, Y: 100, W: 40, H: 40}); err != nil {
		t.Fatalf("place: %v", err)
	}

	if !ov.SetBounds([]Rect{{X: 0, Y: 0, W: 10, H: 10}, {X: 20, Y: 20, W: 10, H: 10}}) {
		t.Fatal("the bounding shape was refused")
	}
	// And back to the whole window, which is what a single picture wants.
	if !ov.SetBounds(nil) {
		t.Error("an empty set must restore the whole window")
	}
}
