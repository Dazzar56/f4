package main

import "strings"

type hostIdentity struct {
	Kind       string
	LaunchMode string
	FileTag    string
}

// classifyHost names the terminal that owns the probe's visible console. A
// real visible ConsoleWindowClass wins over inherited environment variables;
// otherwise WT_SESSION identifies Windows Terminal across pseudo-window
// implementation changes.
func classifyHost(windowClass string, visible bool, wtSession string) string {
	return classifyHostIdentity(windowClass, "", visible, wtSession, "").Kind
}

// classifyHostIdentity also recognizes Windows Terminal's default-terminal
// handoff. In that path WT_SESSION is intentionally absent, but the visible
// zero-sized PseudoConsoleWindow is owned by Cascadia and WindowsTerminal /
// OpenConsole is present in the process context.
func classifyHostIdentity(windowClass, ownerClass string, visible bool, wtSession, processContext string) hostIdentity {
	if windowClass == "ConsoleWindowClass" && visible {
		return hostIdentity{"classic-conhost", "classic-conhost", "conhost"}
	}
	hasWTEnv := strings.TrimSpace(wtSession) != ""
	ctx := strings.ToLower(processContext)
	owner := strings.ToLower(ownerClass)
	isWT := hasWTEnv || (windowClass == "PseudoConsoleWindow" &&
		(strings.Contains(owner, "cascadia") || strings.Contains(ctx, "windowsterminal") || strings.Contains(ctx, "openconsole")))
	if isWT {
		if hasWTEnv {
			return hostIdentity{"windows-terminal", "wt-session", "wt-env"}
		}
		return hostIdentity{"windows-terminal", "default-terminal-handoff-no-wt-session", "wt-handoff"}
	}
	switch windowClass {
	case "ConsoleWindowClass":
		return hostIdentity{"hidden-console-window", "unknown", "other"}
	case "PseudoConsoleWindow":
		return hostIdentity{"pseudoconsole-host", "unknown-pseudoconsole", "other"}
	case "":
		return hostIdentity{"no-console-window", "no-console", "other"}
	default:
		return hostIdentity{"unknown-console-host", "unknown", "other"}
	}
}

func hostFileTag(kind string) string {
	switch kind {
	case "windows-terminal":
		return "wt"
	case "classic-conhost":
		return "conhost"
	default:
		return "other"
	}
}
