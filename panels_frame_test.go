package main

import (
	"testing"
)

func TestPanelsFrame_Layout(t *testing.T) {
	pf := NewPanelsFrame()

	// Simulate 80x25 terminal
	pf.ResizeConsole(80, 25)

	// 1. Check reserved rows with KeyBar visible
	// KeyBar at 24, CommandLine at 23, Panels at 0-22
	if pf.keyBar.Y1 != 24 {
		t.Errorf("KeyBar position error: %d", pf.keyBar.Y1)
	}
	if pf.cmdLine.Y1 != 23 {
		t.Errorf("CommandLine position error: %d", pf.cmdLine.Y1)
	}

	// 2. Check layout after hiding KeyBar
	pf.showKeyBar = false
	pf.ResizeConsole(80, 25)

	// CommandLine should move to the bottom row
	if pf.cmdLine.Y1 != 24 {
		t.Errorf("CommandLine should be at 24 when KeyBar hidden, got %d", pf.cmdLine.Y1)
	}
	if pf.keyBar.IsVisible() {
		t.Error("KeyBar should be invisible")
	}
}