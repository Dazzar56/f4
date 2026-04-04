package main

import (
	"testing"
	"github.com/unxed/vtui"
)

func TestArkanoid_Init(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	SetDefaultF4Palette()

	af := NewArkanoidFrame()
	if af == nil {
		t.Fatal("Failed to create Arkanoid frame")
	}

	if af.lives != 3 {
		t.Errorf("Expected 3 lives, got %d", af.lives)
	}

	if len(af.bricks) == 0 {
		t.Error("Arkanoid started with no bricks")
	}

	af.Close()
}