package main

import (
	"os"
	"testing"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

func TestViewerView_NavigationAndEOF(t *testing.T) {
	vtui.SetDefaultPalette()
	tmp := t.TempDir() + "/test.txt"
	os.WriteFile(tmp, []byte("L1\nL2\nL3\nL4\nL5"), 0644) // 5 lines total

	vv, err := NewViewerView(tmp)
	if err != nil {
		t.Fatal(err)
	}
	vv.SetPosition(0, 0, 10, 3) // Height 4 (Y:0..3). 1 line status, 3 lines content.

	scr := vtui.NewScreenBuf()
	scr.AllocBuf(11, 4)

	// 1. Initial Render
	vv.Show(scr)
	if vv.TopOffset != 0 {
		t.Errorf("Initial offset should be 0, got %d", vv.TopOffset)
	}
	if vv.eofVisible {
		t.Error("EOF should not be visible initially")
	}

	// 2. Scroll Down (should move to L2)
	vv.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
	vv.Show(scr)
	if vv.TopOffset <= 0 {
		t.Errorf("Offset should increase after VK_DOWN, got %d", vv.TopOffset)
	}

	// 3. Jump to End (L3, L4, L5 visible)
	vv.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_END})
	vv.Show(scr)

	if !vv.eofVisible {
		t.Error("EOF should be visible after VK_END")
	}

	// 4. Try scrolling past EOF
	oldOffset := vv.TopOffset
	vv.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_DOWN})
	if vv.TopOffset != oldOffset {
		t.Errorf("VK_DOWN should be blocked when eofVisible is true. Offset changed from %d to %d", oldOffset, vv.TopOffset)
	}
}