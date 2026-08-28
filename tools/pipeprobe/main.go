//go:build windows

// pipeprobe answers, in one run, whether direction A works: can f4 run a
// program over plain pipes instead of a pseudoconsole, and be its terminal?
//
// The reason this matters is in docs/CONPTY_RESEARCH.md §7. Under ConPTY f4
// receives a description of a screen, and where a line wrapped is not in it,
// so the scrollback cannot be re-wrapped. Over a pipe f4 receives the bytes
// the program wrote, wraps them itself, and the question does not arise.
// Programs that need the Win32 console API (cmd, dir, batch files) must keep
// ConPTY; programs that only write VT to stdout (WSL, PowerShell 7, ssh.exe,
// most modern tooling) do not.
//
// The awkward case is ssh.exe, and it is the reason this probe exists in this
// shape. It is a VT program -- it forwards bytes -- but it uses the console
// for two things: to learn the window size, and to read keys in raw mode.
// Started over pipes it may not know the size, and the far end's shell will
// then wrap at 80 columns. So the probe asks, for every program and for ssh
// specifically:
//
//	1. Does it run at all over pipes, and what does it produce?
//	2. Does it produce VT colour when told the terminal supports it?
//	3. Does it honour COLUMNS/LINES for the width, having no console?
//	4. (ssh) Does it start, and does it report a size to the far end?
//
// Every answer is a fact about this machine's binaries, not a guess, and the
// combination decides whether A is worth building and whether ssh needs
// special handling. Nothing is installed or changed.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var out *os.File

func say(format string, a ...any) {
	line := fmt.Sprintf(format, a...)
	fmt.Print(line)
	if out != nil {
		out.WriteString(line)
	}
}

func main() {
	exe, _ := os.Executable()
	path := filepath.Join(filepath.Dir(exe), "pipeprobe-report.txt")
	if f, err := os.Create(path); err == nil {
		out = f
		defer f.Close()
	}

	say("=== pipeprobe 1 (issue #425: can f4 be the terminal over pipes?) ===\n")
	say("time: %s\n", time.Now().Format("2006-01-02 15:04:05 -0700"))
	say("report file: %s\n\n", path)

	reportEnvironment()
	reportPipeBehaviour()
	reportSSH()
	reportConsoleUsers()

	say("\n--- Done ---\n")
	say("Please attach %s to the issue.\n", path)
	fmt.Print("\nPress Enter to close...")
	fmt.Scanln()
}

func reportEnvironment() {
	say("--- Where this is running ---\n")
	say("windows: %s\n", windowsVersion())
	for _, name := range []string{"ssh.exe", "wsl.exe", "pwsh.exe", "powershell.exe", "git.exe"} {
		if p, err := exec.LookPath(name); err == nil {
			say("%-15s %s\n", name+":", p)
		} else {
			say("%-15s not found\n", name+":")
		}
	}
	say("TERM=%s COLORTERM=%s COLUMNS=%s LINES=%s\n\n",
		orNone(os.Getenv("TERM")), orNone(os.Getenv("COLORTERM")),
		orNone(os.Getenv("COLUMNS")), orNone(os.Getenv("LINES")))
}

func orNone(s string) string {
	if s == "" {
		return "(unset)"
	}
	return s
}

func windowsVersion() string {
	b, err := exec.Command("cmd.exe", "/c", "ver").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(strings.ReplaceAll(string(b), "\r\n", " "))
}

// runPiped runs a command with pipes only -- no console of any kind -- and
// returns what it wrote. This is exactly the shape direction A proposes: f4
// reads bytes and is the terminal.
func runPiped(env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	// CREATE_NO_WINDOW and no console inheritance: the child must have no
	// console at all, so that what it does is what it would do under f4's
	// pipes.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x08000000 | 0x00000008, // CREATE_NO_WINDOW | DETACHED_PROCESS
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return "", err
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return buf.String(), err
	case <-time.After(8 * time.Second):
		cmd.Process.Kill()
		return buf.String(), fmt.Errorf("timed out")
	}
}

func describe(text string, err error) {
	if err != nil {
		say("    error: %v\n", err)
	}
	esc := strings.Count(text, "\x1b")
	lines := strings.Count(text, "\n")
	say("    %d bytes, %d lines, %d escape sequences\n", len(text), lines, esc)
	sample := strings.TrimSpace(text)
	if len(sample) > 220 {
		sample = sample[:220] + "..."
	}
	say("    sample: %q\n", sample)
}

// reportPipeBehaviour asks the central question for A: over pipes, do the
// programs f4 would route this way still behave? Colour is the visible proxy
// for "does it think a terminal is there", and width is the one that decides
// whether the scrollback f4 keeps will look right.
func reportPipeBehaviour() {
	say("--- Question 1: what do programs do over pipes, with no console? ---\n")
	say("If a program produces the same VT it would on a terminal, f4 can be its\n")
	say("terminal and own the wrapping. If it goes plain or refuses, it needs a\n")
	say("console and stays on ConPTY.\n\n")

	type probe struct {
		label string
		env   []string
		name  string
		args  []string
	}
	var probes []probe

	if _, err := exec.LookPath("wsl.exe"); err == nil {
		probes = append(probes,
			probe{"wsl: ls -C, no hints", nil, "wsl.exe", []string{"ls", "-C", "/usr/bin"}},
			probe{"wsl: ls --color=always, TERM+COLUMNS", []string{"TERM=xterm-256color", "COLUMNS=100", "LINES=30"},
				"wsl.exe", []string{"ls", "-C", "--color=always", "/usr/bin"}},
			probe{"wsl: what does the shell think COLUMNS is", []string{"COLUMNS=100"},
				"wsl.exe", []string{"sh", "-c", "echo COLUMNS=$COLUMNS; tput cols 2>/dev/null || echo 'tput: no'"}},
		)
	}
	if _, err := exec.LookPath("pwsh.exe"); err == nil {
		probes = append(probes,
			probe{"pwsh 7: width it reports", []string{"COLUMNS=100"}, "pwsh.exe",
				[]string{"-NoProfile", "-NonInteractive", "-Command", "$Host.UI.RawUI.BufferSize.Width"}},
			probe{"pwsh 7: colour when asked", []string{"TERM=xterm-256color"}, "pwsh.exe",
				[]string{"-NoProfile", "-NonInteractive", "-Command", "$PSStyle.OutputRendering='ANSI'; Write-Host -ForegroundColor Green hello"}},
		)
	}
	probes = append(probes,
		probe{"powershell 5: width it reports", []string{"COLUMNS=100"}, "powershell.exe",
			[]string{"-NoProfile", "-NonInteractive", "-Command", "$Host.UI.RawUI.BufferSize.Width"}},
		probe{"cmd: dir (the console-API case)", nil, "cmd.exe", []string{"/c", "dir", "/-p", os.Getenv("SystemRoot")}},
	)

	for _, p := range probes {
		say("  %s\n", p.label)
		if len(p.env) > 0 {
			say("    env: %s\n", strings.Join(p.env, " "))
		}
		text, err := runPiped(p.env, p.name, p.args...)
		describe(text, err)
		say("\n")
	}
}

// reportSSH is the case that decides whether direction A covers remote work
// or leaves a hole. ssh.exe forwards bytes, so f4 could be its terminal --
// but it learns the window size from the console, and over pipes there is
// none. If it honours COLUMNS, A covers ssh too; if not, a remote session
// started this way would be laid out for someone else's idea of the width.
func reportSSH() {
	say("--- Question 2: ssh.exe over pipes ---\n")
	sshPath, err := exec.LookPath("ssh.exe")
	if err != nil {
		say("ssh.exe not found; skipping.\n\n")
		return
	}
	say("using %s\n", sshPath)

	// -V and -G do not connect anywhere, so this is safe on any machine.
	text, err := runPiped(nil, "ssh.exe", "-V")
	say("  version (stderr):\n")
	describe(text, err)

	// Does it start and read config without a console at all? A failure here
	// would mean ssh.exe cannot run over pipes, full stop.
	text, err = runPiped([]string{"COLUMNS=137", "LINES=42", "TERM=xterm-256color"},
		"ssh.exe", "-G", "example.invalid")
	say("  `ssh -G` with COLUMNS=137 (does it run, and what does it resolve):\n")
	describe(text, err)

	say("\n  What is still unknown after this: whether ssh.exe *sends* 137 to the\n")
	say("  far end. That needs a real server, so if you have one to hand:\n")
	say("      ssh <host> \"stty size; tput cols\"\n")
	say("  run once from f4's terminal and once from cmd.exe, and paste both.\n")
	say("  A difference is the size of the problem; no difference means ssh\n")
	say("  takes the size from somewhere f4 can control.\n\n")
}

// reportConsoleUsers separates the programs that genuinely need a console
// from the ones that merely have one. Direction A routes by kind, and this
// is the evidence for where each kind sits: a program that runs identically
// with no console attached never needed one.
func reportConsoleUsers() {
	say("--- Question 3: which of these actually need a console? ---\n")
	say("Run with no console at all. Anything that still works belongs on pipes.\n\n")

	cases := []struct {
		label string
		name  string
		args  []string
	}{
		{"cmd /c echo", "cmd.exe", []string{"/c", "echo", "hello"}},
		{"cmd /c dir (one line)", "cmd.exe", []string{"/c", "dir", "/b", "/-p", os.Getenv("SystemRoot") + `\explorer.exe`}},
		{"cmd /c mode con (asks the console its size)", "cmd.exe", []string{"/c", "mode", "con"}},
		{"where.exe", "where.exe", []string{"cmd"}},
	}
	if _, err := exec.LookPath("wsl.exe"); err == nil {
		cases = append(cases, struct {
			label string
			name  string
			args  []string
		}{"wsl echo", "wsl.exe", []string{"echo", "hello"}})
	}
	for _, c := range cases {
		say("  %s\n", c.label)
		text, err := runPiped(nil, c.name, c.args...)
		describe(text, err)
		say("\n")
	}
	say("Reading this: `mode con` failing with no console is expected and is\n")
	say("the signature of a program that truly needs one. `dir` succeeding\n")
	say("means cmd.exe can produce output over pipes -- but note it will format\n")
	say("for 80 columns, because that is what it assumes without a console,\n")
	say("which is why cmd stays on ConPTY regardless.\n")
}
