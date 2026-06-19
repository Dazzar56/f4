package main

import (
	"github.com/unxed/vtui"
	"testing"
)

func TestTopBar_Show(t *testing.T) {
	vtui.SetDefaultPalette()
	SetDefaultF4Palette()

	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(40, 5)

	// 1. Valid TopBar with value
	tb := NewTopBar(func() string {
		return "My Test Status"
	})
	tb.SetPosition(0, 0, 39, 0)
	tb.SetVisible(true)

	tb.Show(scr)

	// Verify that the background is filled and text is written
	attr := vtui.Palette[ColViewerStatus]
	for x := 0; x < 40; x++ {
		cell := scr.GetCell(x, 0)
		if cell.Attributes != attr {
			t.Errorf("Expected cell at x=%d to have attribute %016X, got %016X", x, attr, cell.Attributes)
		}
	}

	// Verify the text was printed starting at X1 (0)
	expectedText := "My Test Status"
	for i, r := range expectedText {
		cell := scr.GetCell(i, 0)
		if cell.Char != uint64(r) {
			t.Errorf("Expected char %q at x=%d, got %q", r, i, rune(cell.Char))
		}
	}
}

func TestTopBar_NilCallbackAndInvisible(t *testing.T) {
	vtui.SetDefaultPalette()
	SetDefaultF4Palette()

	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(40, 5)

	// 1. Nil callback - should not panic and should not write anything
	tbNil := NewTopBar(nil)
	tbNil.SetPosition(0, 0, 39, 0)
	tbNil.SetVisible(true)

	tbNil.Show(scr) // should be a no-op except for parent Bar logic

	// 2. Invisible TopBar - should not write anything
	tbInvisible := NewTopBar(func() string {
		return "Should Not Be Seen"
	})
	tbInvisible.SetPosition(0, 0, 39, 0)
	tbInvisible.SetVisible(false)

	tbInvisible.Show(scr) // should be a no-op entirely
}