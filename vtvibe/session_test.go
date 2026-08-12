package vtvibe

import (
	"testing"
)

func TestSession_Draft(t *testing.T) {
	s := NewSession()
	if s.Draft() != "" {
		t.Errorf("expected empty draft, got %q", s.Draft())
	}
	s.tree.writeFile("/draft.md", []byte("hello model"))
	if s.Draft() != "hello model" {
		t.Errorf("expected draft to be read")
	}
	s.ClearDraft()
	if s.Draft() != "" {
		t.Errorf("expected empty draft after clear")
	}
}

func TestSession_Reset(t *testing.T) {
	s := NewSession()
	s.tree.writeFile("/ctx/test.go", []byte("package main"))
	s.appendTurn(Turn{Role: "user", Text: "hello"})

	s.Reset(true)
	if len(s.Turns()) != 0 {
		t.Errorf("expected turns to be cleared")
	}
	if _, ok := s.tree.readFile("/ctx/test.go"); !ok {
		t.Errorf("expected context to be kept")
	}

	s.Reset(false)
	if _, ok := s.tree.readFile("/ctx/test.go"); ok {
		t.Errorf("expected context to be cleared")
	}
}
