package main

import (
	"github.com/unxed/f4/vfs"
	"testing"
)

func TestAIChatPanel_Resize(t *testing.T) {
	fp := NewFileSystemPanel(0, 0, 80, 24, vfs.NewNullVFS())
	cp := NewAIChatPanel(fp)
	cp.SetPosition(0, 0, 79, 23)

	if cp.input.Y1 != 20 {
		t.Errorf("Expected input Y1 to be 20, got %d", cp.input.Y1)
	}
	if cp.input.Y2 != 23 {
		t.Errorf("Expected input Y2 to be 23, got %d", cp.input.Y2)
	}
	if cp.Kind() != "ai_chat" {
		t.Errorf("Expected Kind 'ai_chat', got '%s'", cp.Kind())
	}
}
