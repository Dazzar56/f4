//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// waitForLogContains blocks until the file at path contains marker, or until
// timeout. Used so the f4 matrix waits for a durable startup signal (the
// "F4 STARTUP" line f4 writes itself) instead of racing the first PTY flush,
// which on a slow machine can arrive after a fixed drain's quiet window.
func waitForLogContains(path, marker string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), marker) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// probeF4Matrix runs the real bundled f4 four times under a hidden ConPTY.
// The probe owns the environment, input and resizes. This is the part that an
// environment-only probe was missing: it exercises f4's scratch-frame routing
// in off, hint, oracle and diagnostic probe modes without asking the tester to
// set a single variable.
func probeF4Matrix(w *writer) {
	w.section("5. Real f4 integration matrix: off, hint, oracle, probe")
	w.step("  real f4 matrix: off, hint, oracle and probe (automatic)...")

	outDir := probeOutputDir()
	f4path := ""
	for _, name := range []string{"f4-under-test.exe", "f4.exe"} {
		candidate := filepath.Join(outDir, name)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			f4path = candidate
			break
		}
	}
	if f4path == "" {
		w.printf("skipped: f4-under-test.exe is not beside the probe\n")
		w.summary("f4.matrix", "skipped (companion executable missing)")
		return
	}
	w.printf("companion = %q (file version %s)\n", f4path, fileVersion(f4path))
	w.summary("f4.companion", f4path)
	w.summary("f4.companion.file_version", fileVersion(f4path))

	isolation, err := os.MkdirTemp("", "f4probe-f4-matrix-")
	if err != nil {
		w.printf("cannot create isolated f4 profile: %v\n", err)
		w.summary("f4.matrix", "skipped (cannot create isolated profile)")
		return
	}
	defer os.RemoveAll(isolation)

	type modeResult struct {
		mode, verdict string
	}
	var results []modeResult
	for _, mode := range []string{"off", "hint", "oracle", "probe"} {
		w.sub("f4 mode " + mode)
		modeDir := filepath.Join(isolation, mode)
		workDir := filepath.Join(modeDir, "work")
		if err := os.MkdirAll(filepath.Join(workDir, "subdir"), 0755); err != nil {
			w.printf("cannot prepare mode directory: %v\n", err)
			results = append(results, modeResult{mode, "setup failed"})
			continue
		}
		logPath := filepath.Join(outDir, "f4probe-f4-"+mode+".log")
		screenPath := filepath.Join(outDir, "f4probe-f4-"+mode+"-screen.log")
		_ = os.Remove(logPath)
		_ = os.Remove(screenPath)

		cmdline := syscall.EscapeArg(f4path) + " --tty ansi --input ConPTY --log " + syscall.EscapeArg(logPath)
		overrides := map[string]string{
			"F4_WIN_REFLOW": mode,
			"VTUI_DEBUG":    "1",
			"APPDATA":       filepath.Join(modeDir, "AppData", "Roaming"),
			"LOCALAPPDATA":  filepath.Join(modeDir, "AppData", "Local"),
		}
		for _, dir := range []string{overrides["APPDATA"], overrides["LOCALAPPDATA"]} {
			_ = os.MkdirAll(dir, 0755)
		}

		started := time.Now()
		p, err := newPTYProcess(0, 100, 30, cmdline, overrides, workDir)
		if err != nil {
			w.printf("launch failed: %v\n", err)
			w.summary("f4."+mode+".launched", "no ("+err.Error()+")")
			results = append(results, modeResult{mode, "launch failed"})
			continue
		}
		w.summary("f4."+mode+".launched", "yes")

		var screen []byte
		// f4's first flush can land after the drain's quiet window elapses on
		// a slow machine, which used to report startup=0 and fail an
		// otherwise healthy run. Wait for a durable startup signal -- the
		// "F4 STARTUP" line f4 writes to its own debug log -- before draining,
		// so the drain sees the banner rather than racing it.
		waitForLogContains(logPath, "F4 STARTUP", 12*time.Second)
		startup := p.drain(1200*time.Millisecond, 12*time.Second)
		screen = append(screen, startup...)

		// Panels command line: exercise an external command, then two panel
		// directory changes. The latter makes f4 issue its private cmd sync and
		// covers the creep/startup-sync path in the real application.
		p.line("echo F4PROBE_APP_" + strings.Repeat("R", 96))
		commandOut := p.drain(700*time.Millisecond, 6*time.Second)
		screen = append(screen, commandOut...)
		p.line("cd subdir")
		screen = append(screen, p.drain(700*time.Millisecond, 5*time.Second)...)
		p.line("cd ..")
		screen = append(screen, p.drain(700*time.Millisecond, 5*time.Second)...)

		// Resize the *outer* pseudoconsole. f4 must resize its local child,
		// route any oracle repaint through the scratch parser and then repaint
		// its UI. This is the integration boundary the old standalone run did
		// not cover.
		resizeOK := true
		for _, size := range []coord{{40, 30}, {120, 30}, {80, 24}} {
			if err := p.resize(size.X, size.Y); err != nil {
				w.printf("outer resize %dx%d failed: %v\n", size.X, size.Y, err)
				resizeOK = false
				continue
			}
			screen = append(screen, p.drain(800*time.Millisecond, 6*time.Second)...)
		}

		// Raw terminal mode: open a nested cmd, press Enter on a command, exit
		// it and return to panels. This directly captures the O3 input path.
		p.send("\x0f") // Ctrl+O
		screen = append(screen, p.drain(500*time.Millisecond, 3*time.Second)...)
		p.line("cmd")
		screen = append(screen, p.drain(700*time.Millisecond, 5*time.Second)...)
		p.line("echo F4PROBE_NESTED_ENTER_OK")
		nestedOut := p.drain(700*time.Millisecond, 5*time.Second)
		screen = append(screen, nestedOut...)
		p.line("exit")
		screen = append(screen, p.drain(700*time.Millisecond, 5*time.Second)...)
		p.send("\x0f")
		screen = append(screen, p.drain(500*time.Millisecond, 3*time.Second)...)

		// F10. If the input backend does not translate the sequence, close()
		// still terminates only this owned child after the evidence is saved.
		p.send("\x1b[21~")
		screen = append(screen, p.drain(900*time.Millisecond, 4*time.Second)...)
		code, alive := p.exitCode()
		p.close()
		_ = os.WriteFile(screenPath, []byte(Escape(screen)), 0644)

		debug, readErr := os.ReadFile(logPath)
		if readErr != nil {
			w.printf("debug log missing: %v\n", readErr)
		}
		d := string(debug)
		obs := f4MatrixObservations{
			mode:       mode,
			startupLen: len(startup),
			screenLen:  len(screen),
			debugLog:   d,
			logReadErr: readErr != nil,
			resizeOK:   resizeOK,
		}
		verdict, oraclePasses, oracleStamped, oracleRejected := f4MatrixVerdict(obs)
		modeConfirmed := strings.Contains(d, "REFLOW: F4_WIN_REFLOW="+mode)
		oracleSeen := strings.Contains(d, "REFLOW_ORACLE:")
		oracleCompleted := oracleStamped > 0
		syncSeen := strings.Contains(d, "ANSI_PARSER: Excising background Windows CD sync")
		nestedSeen := strings.Contains(string(nestedOut), "F4PROBE_NESTED_ENTER_OK") ||
			strings.Contains(d, "F4PROBE_NESTED_ENTER_OK")

		w.printf("startup=%d command=%d total-screen=%d duration=%v exited=%v code=%#x\n",
			len(startup), len(commandOut), len(screen), time.Since(started).Round(time.Millisecond), !alive, code)
		w.printf("mode-confirmed=%v oracle-log=%v oracle-passes=%d stamped=%d rejected=%d oracle-completed=%v sync-excision=%v nested-enter=%v resize-ok=%v\n",
			modeConfirmed, oracleSeen, oraclePasses, oracleStamped, oracleRejected, oracleCompleted, syncSeen, nestedSeen, resizeOK)
		w.printf("debug excerpt:\n%s\n", Clip(Escape(debug), 14000))
		w.summary("f4."+mode+".mode_confirmed", yesno(modeConfirmed))
		w.summary("f4."+mode+".oracle_observed", yesno(oracleSeen))
		if mode == "oracle" || mode == "probe" {
			w.summary("f4."+mode+".oracle_passes",
				fmt.Sprintf("%d (stamped %d, safely rejected %d)", oraclePasses, oracleStamped, oracleRejected))
		}
		w.summary("f4."+mode+".oracle_completed", yesno(oracleCompleted))
		w.summary("f4."+mode+".sync_excision_observed", yesno(syncSeen))
		w.summary("f4."+mode+".nested_enter_observed", yesno(nestedSeen))
		w.summary("f4."+mode+".outer_resizes", yesno(resizeOK))
		w.summary("f4."+mode+".result", verdict)
		results = append(results, modeResult{mode, verdict})
	}

	var resultText []string
	allComplete := len(results) == 4
	for _, r := range results {
		resultText = append(resultText, r.mode+":"+r.verdict)
		allComplete = allComplete && r.verdict == "complete"
	}
	w.summary("f4.matrix.modes", strings.Join(resultText, ", "))
	w.summary("f4.matrix", map[bool]string{true: "complete", false: "incomplete"}[allComplete])
}

func probeOutputDir() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	return filepath.Dir(exe)
}
