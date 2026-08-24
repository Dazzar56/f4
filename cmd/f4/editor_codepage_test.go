package main

import (
	"testing"

	"github.com/unxed/f4/piecetable"
	"github.com/unxed/vtui"
)

func TestEditorView_PasteConvertsInternalClipboardCodepage(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	source := NewEditorView(piecetable.New([]byte("Привет")), nil, "")
	source.Codepage = 11111 // ANSI
	source.CursorPos = len([]byte("Привет"))
	source.selActive = true
	source.selAnchorOffset = 0
	source.CopySelection()
	defer source.Close()

	target := NewEditorView(piecetable.New(nil), nil, "")
	target.Codepage = 22222 // OEM
	target.PasteText(vtui.GetClipboard())
	defer target.Close()

	if got := target.GetText(); got != "Привет" {
		t.Fatalf("pasted text = %q, want %q", got, "Привет")
	}
}
