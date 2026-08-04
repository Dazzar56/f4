package main

import "testing"

func TestActionRegistry(t *testing.T) {
	called := false
	RegisterAction("Test.Action", func() {
		called = true
	})

	if !RunAction("Test.Action") {
		t.Error("Expected RunAction to return true for registered action")
	}
	if !called {
		t.Error("Expected registered handler to be executed")
	}

	called2 := false
	RegisterAction("Another.Test", func() {
		called2 = true
	})
	if !RunAction("another.test") {
		t.Error("Expected RunAction to be case insensitive")
	}
	if !called2 {
		t.Error("Expected lowercase lookup to trigger action")
	}

	if RunAction("Nonexistent.Action") {
		t.Error("Expected RunAction to return false for unknown action")
	}
}
