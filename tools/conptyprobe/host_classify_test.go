package main

import "testing"

func TestClassifyHost(t *testing.T) {
	tests := []struct {
		name, class, wt, want string
		visible               bool
	}{
		{"classic", "ConsoleWindowClass", "", "classic-conhost", true},
		{"visible classic beats inherited WT", "ConsoleWindowClass", "stale-session", "classic-conhost", true},
		{"hidden classic", "ConsoleWindowClass", "", "hidden-console-window", false},
		{"WT environment wins", "PseudoConsoleWindow", "session-id", "windows-terminal", false},
		{"other pseudoconsole", "PseudoConsoleWindow", "", "pseudoconsole-host", false},
		{"no window", "", "", "no-console-window", false},
		{"unknown", "SomethingNew", "", "unknown-console-host", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyHost(tt.class, tt.visible, tt.wt); got != tt.want {
				t.Fatalf("classifyHost(%q, %v, %q) = %q, want %q",
					tt.class, tt.visible, tt.wt, got, tt.want)
			}
		})
	}
}

func TestHostFileTag(t *testing.T) {
	if got := hostFileTag("windows-terminal"); got != "wt" {
		t.Fatalf("WT tag = %q", got)
	}
	if got := hostFileTag("classic-conhost"); got != "conhost" {
		t.Fatalf("conhost tag = %q", got)
	}
	if got := hostFileTag("unknown-console-host"); got != "other" {
		t.Fatalf("other tag = %q", got)
	}
}

func TestClassifyWindowsTerminalHandoffWithoutEnvironment(t *testing.T) {
	id := classifyHostIdentity(
		"PseudoConsoleWindow", "CASCADIA_HOSTING_WINDOW_CLASS", true, "",
		"f4probe.exe<-explorer.exe; OpenConsole.exe WindowsTerminal.exe",
	)
	if id.Kind != "windows-terminal" || id.LaunchMode != "default-terminal-handoff-no-wt-session" || id.FileTag != "wt-handoff" {
		t.Fatalf("identity = %+v", id)
	}
}

func TestClassifyWindowsTerminalSession(t *testing.T) {
	id := classifyHostIdentity("PseudoConsoleWindow", "", true, "session", "")
	if id.Kind != "windows-terminal" || id.LaunchMode != "wt-session" || id.FileTag != "wt-env" {
		t.Fatalf("identity = %+v", id)
	}
}
