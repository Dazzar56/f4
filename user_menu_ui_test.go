package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unxed/vtui"
)

func TestParseFunctionKey(t *testing.T) {
	cases := []struct {
		in   string
		want uint32
	}{
		{"F1", 1},
		{"F2", 2},
		{"F12", 12},
		{"F24", 24},
		{"f3", 3}, // case insensitive on the leading F
		{"F0", 0},
		{"F25", 0},
		{"F", 0},
		{"FF", 0},
		{"a", 0},
		{"", 0},
		{"--", 0},
		{"F1a", 0},
	}
	for _, c := range cases {
		if got := parseFunctionKey(c.in); got != c.want {
			t.Errorf("parseFunctionKey(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestEscapeAmpersand(t *testing.T) {
	cases := []struct{ in, want string }{
		{"foo", "foo"},
		{"R&D", "R&&D"},
		{"&start", "&&start"},
		{"a&b&c", "a&&b&&c"},
	}
	for _, c := range cases {
		if got := escapeAmpersand(c.in); got != c.want {
			t.Errorf("escapeAmpersand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStripAmpersand(t *testing.T) {
	cases := []struct{ in, want string }{
		{"foo", "foo"},
		{"R&&D", "R&D"},
		{"&Open", "Open"},
		{"a&b&c", "abc"},
		{"foo&&bar", "foo&bar"},
	}
	for _, c := range cases {
		if got := stripAmpersand(c.in); got != c.want {
			t.Errorf("stripAmpersand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsMenuComment(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"REM this is a comment", true},
		{"rem lowercase too", true},
		{"REM", true},
		{"REM\tcomment with tab", true},
		{"REMOVE", false}, // no separator after REM
		{":: shell-style comment", true},
		{":single colon", false},
		{"normal command", false},
		{"  REM indented", false}, // caller strips spaces first
	}
	for _, c := range cases {
		if got := isMenuComment(c.in); got != c.want {
			t.Errorf("isMenuComment(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestFormatMenuItemText_SingleChar(t *testing.T) {
	got := formatMenuItemText(UserMenuItem{HotKey: "a", Label: "Apple"})
	// Hotkey marker '&a' plus enough padding to bring the label to column 6
	if !strings.HasPrefix(got, "&a") {
		t.Errorf("missing & marker for single-char hotkey: %q", got)
	}
	if !strings.HasSuffix(got, "Apple") {
		t.Errorf("label missing: %q", got)
	}
}

func TestFormatMenuItemText_FunctionKey(t *testing.T) {
	got := formatMenuItemText(UserMenuItem{HotKey: "F3", Label: "Build"})
	// F-keys must not use the '&' marker (would underline 'F').
	if strings.HasPrefix(got, "&") {
		t.Errorf("F-key should not have & marker: %q", got)
	}
	if !strings.HasPrefix(got, "F3") || !strings.HasSuffix(got, "Build") {
		t.Errorf("unexpected layout: %q", got)
	}
}

func TestFormatMenuItemText_NoHotkey(t *testing.T) {
	got := formatMenuItemText(UserMenuItem{HotKey: "", Label: "Plain"})
	if !strings.HasSuffix(got, "Plain") {
		t.Errorf("missing label: %q", got)
	}
}

func TestFormatMenuItemText_AmpersandInLabel(t *testing.T) {
	got := formatMenuItemText(UserMenuItem{HotKey: "x", Label: "R&D"})
	// The '&' in label must be doubled so vtui doesn't underline 'D'.
	if !strings.Contains(got, "R&&D") {
		t.Errorf("label ampersand not escaped: %q", got)
	}
}

func TestFindLocalFarMenu_WalksUp(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	wanted := filepath.Join(root, "a", farMenuFileName)
	if err := os.WriteFile(wanted, []byte("x:  X\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, found := findLocalFarMenu(deep)
	if !found {
		t.Fatalf("expected to find FarMenu.ini at %q from %q", wanted, deep)
	}
	// Resolve to absolute to avoid /var vs /private/var on macOS.
	gotAbs, _ := filepath.EvalSymlinks(got)
	wantAbs, _ := filepath.EvalSymlinks(wanted)
	if gotAbs != wantAbs {
		t.Errorf("found %q, want %q", gotAbs, wantAbs)
	}
}

func TestFindLocalFarMenu_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, found := findLocalFarMenu(dir)
	if found {
		t.Errorf("expected no FarMenu.ini in empty tree")
	}
}

func TestFindLocalFarMenu_PicksClosest(t *testing.T) {
	// When two ancestors have FarMenu.ini, the closer one wins.
	root := t.TempDir()
	mid := filepath.Join(root, "mid")
	leaf := filepath.Join(mid, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	rootMenu := filepath.Join(root, farMenuFileName)
	midMenu := filepath.Join(mid, farMenuFileName)
	if err := os.WriteFile(rootMenu, []byte("r:  R\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(midMenu, []byte("m:  M\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ := findLocalFarMenu(leaf)
	gotAbs, _ := filepath.EvalSymlinks(got)
	midAbs, _ := filepath.EvalSymlinks(midMenu)
	if gotAbs != midAbs {
		t.Errorf("closest wins: got %q, want %q", gotAbs, midAbs)
	}
}

func TestMainMenuFilePath_HasExpectedSuffix(t *testing.T) {
	p := MainMenuFilePath()
	want := filepath.Join("f4", "settings", "user_menu.ini")
	if !strings.HasSuffix(p, want) {
		t.Errorf("MainMenuFilePath()=%q, want suffix %q", p, want)
	}
}

func TestFindMenuItemByUserData(t *testing.T) {
	menu := vtui.NewVMenu("test")
	menu.AddItem(vtui.MenuItem{Text: "a", UserData: 3})
	menu.AddItem(vtui.MenuItem{Text: "b", UserData: 7})
	menu.AddItem(vtui.MenuItem{Text: "c", UserData: 1})

	if idx, ok := findMenuItemByUserData(menu, 7); !ok || idx != 1 {
		t.Errorf("got idx=%d ok=%v, want 1/true", idx, ok)
	}
	if _, ok := findMenuItemByUserData(menu, 99); ok {
		t.Errorf("expected not found for unknown UserData")
	}
}
