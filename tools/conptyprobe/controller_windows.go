//go:build windows

package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type probeOptions struct {
	internal bool
	label    string
	scope    string
}

func parseProbeOptions(args []string) probeOptions {
	o := probeOptions{scope: "full"}
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--internal-child="):
			o.internal = true
			o.label = strings.TrimPrefix(arg, "--internal-child=")
		case strings.HasPrefix(arg, "--scope="):
			o.scope = strings.TrimPrefix(arg, "--scope=")
		}
	}
	if o.scope != "host" && o.scope != "full" {
		o.scope = "full"
	}
	return o
}

type childRun struct {
	label, description string
	launch             func(exe string) error
	timeout            time.Duration
}

func runController() {
	outDir := probeOutputDir()
	exe, err := os.Executable()
	if err != nil {
		fmt.Println("f4probe: cannot locate itself:", err)
		return
	}

	fmt.Println("f4probe 6 exhaustive: one launcher, no environment variables or manual log handling.")
	fmt.Println("It will open/close test consoles itself and package every result into one ZIP.")
	fmt.Println()

	runs := []childRun{
		{
			label: "forced-conhost", description: "forced inbox classic conhost + full ConPTY/f4 matrix", timeout: 8 * time.Minute,
			launch: func(exe string) error {
				cmd := exec.Command("conhost.exe", "-ForceNoHandoff", "--", exe,
					"--internal-child=forced-conhost", "--scope=full")
				return cmd.Start()
			},
		},
		{
			label: "explicit-wt", description: "explicit Windows Terminal host", timeout: 90 * time.Second,
			launch: func(exe string) error {
				cmd := exec.Command("wt.exe", "-w", "new", "--title", "f4probe explicit WT", exe,
					"--internal-child=explicit-wt", "--scope=host")
				return cmd.Start()
			},
		},
		{
			label: "default-handoff", description: "configured default terminal, WT_SESSION removed in child", timeout: 90 * time.Second,
			launch: func(exe string) error {
				cmd := exec.Command(exe, "--internal-child=default-handoff", "--scope=host")
				cmd.Env = envWithout(os.Environ(), "WT_SESSION", "WT_PROFILE_ID")
				cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
				return cmd.Start()
			},
		},
	}

	var manifest strings.Builder
	fmt.Fprintf(&manifest, "f4probe 6 controller %s\r\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&manifest, "probe=%s\r\n\r\n", exe)
	var files []string
	for _, run := range runs {
		logPath := filepath.Join(outDir, "f4probe-"+run.label+".log")
		donePath := logPath + ".done"
		_ = os.Remove(logPath)
		_ = os.Remove(donePath)
		fmt.Printf("Starting: %s\n", run.description)
		started := time.Now()
		err := run.launch(exe)
		if err == nil {
			err = waitForFile(donePath, run.timeout)
		}
		status := "complete"
		if err != nil {
			status = "FAILED: " + err.Error()
		}
		fmt.Printf("  %s (%v)\n", status, time.Since(started).Round(time.Second))
		fmt.Fprintf(&manifest, "%s = %s\r\n", run.label, status)
		if _, statErr := os.Stat(logPath); statErr == nil {
			files = append(files, logPath)
		}
		_ = os.Remove(donePath)
	}

	for _, pattern := range []string{"f4probe-f4-*.log"} {
		matches, _ := filepath.Glob(filepath.Join(outDir, pattern))
		files = append(files, matches...)
	}
	manifestPath := filepath.Join(outDir, "f4probe-manifest.txt")
	_ = os.WriteFile(manifestPath, []byte(manifest.String()), 0644)
	files = append(files, manifestPath)

	zipPath := filepath.Join(outDir, "f4probe-results.zip")
	_ = os.Remove(zipPath)
	if err := writeResultsZip(zipPath, uniqueFiles(files)); err != nil {
		fmt.Println("Could not create results ZIP:", err)
	} else {
		fmt.Println()
		fmt.Println("DONE. Attach this one file:")
		fmt.Println(zipPath)
	}
	if _, ok := getConsoleMode(getStdHandle(stdInputHandle)); ok {
		fmt.Print("Press Enter to close. ")
		fmt.Fscanln(os.Stdin)
	}
}

func envWithout(base []string, names ...string) []string {
	remove := make(map[string]bool, len(names))
	for _, name := range names {
		remove[strings.ToUpper(name)] = true
	}
	out := make([]string, 0, len(base))
	for _, entry := range base {
		name := entry
		if i := strings.IndexByte(entry, '='); i >= 0 {
			name = entry[:i]
		}
		if !remove[strings.ToUpper(name)] {
			out = append(out, entry)
		}
	}
	return out
}

func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if st, err := os.Stat(path); err == nil && st.Size() != 0 {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %v waiting for %s", timeout, filepath.Base(path))
}

func uniqueFiles(in []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, path := range in {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

func writeResultsZip(path string, files []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	for _, path := range files {
		in, err := os.Open(path)
		if err != nil {
			continue
		}
		out, err := zw.Create(filepath.Base(path))
		if err == nil {
			_, err = io.Copy(out, in)
		}
		in.Close()
		if err != nil {
			zw.Close()
			f.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
