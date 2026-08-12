package main

import (
	"github.com/unxed/f4/vfs"
	"testing"
)

func TestAIChatPanel_Resize(t *testing.T) {
	fp := NewFileSystemPanel(0, 0, 80, 24, vfs.NewNullVFS(0))
	cp := NewAIChatPanel(fp)
	cp.SetPosition(0, 0, 79, 23)

	if cp.input.Y1 != 19 {
		t.Errorf("Expected input Y1 to be 19, got %d", cp.input.Y1)
	}
	if cp.input.Y2 != 22 {
		t.Errorf("Expected input Y2 to be 22, got %d", cp.input.Y2)
	}
	if cp.Kind() != "ai_chat" {
		t.Errorf("Expected Kind 'ai_chat', got '%s'", cp.Kind())
	}
}

func TestAIChatPanel_CellCutChat(t *testing.T) {
	if cut := cellCutChat("hello", 10); cut != 5 {
		t.Errorf("cellCutChat expected 5, got %d", cut)
	}
	if cut := cellCutChat("hello", 2); cut != 2 {
		t.Errorf("cellCutChat expected 2, got %d", cut)
	}
}
