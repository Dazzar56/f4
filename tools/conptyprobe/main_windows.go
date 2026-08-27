//go:build windows

// f4probe collects, in one run, every measurement docs/TERMINAL_LEDGER.md,
// docs/TERMINAL_CONPTY_FINDINGS.md and docs/WINCON_805_HANDOVER.md list as
// missing or as measured on one build only. It changes nothing on the machine:
// it starts its own hidden cmd.exe on its own pseudoconsole, reads process
// lists and window attributes, and writes f4probe.log next to itself.
//
// Build:  GOOS=windows GOARCH=amd64 go build -o f4probe.exe .
// Run:    f4probe.exe        (about a minute and a half)
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	opts := parseProbeOptions(os.Args[1:])
	if !opts.internal {
		runController()
		return
	}
	runProbe(opts)
}

func runProbe(opts probeOptions) {
	w := &writer{}
	start := time.Now()
	identity := currentHostIdentity()
	logName := "f4probe-" + identity.FileTag + ".log"
	if opts.label != "" {
		logName = "f4probe-" + opts.label + ".log"
	}

	w.printf("f4probe 6 exhaustive -- %s\n", time.Now().Format(time.RFC3339))
	w.printf("issue #425 (Windows terminal) and issue #805 (console images)\n")
	w.printf("Nothing here writes to the system. One hidden cmd.exe is started on\n")
	w.printf("a pseudoconsole of the probe's own, and killed at the end.\n")

	fmt.Printf("f4probe 6 child %q: scope=%s\n", opts.label, opts.scope)
	fmt.Println("Please do not type in this window while it runs.")

	run := func(name string, f func(*writer)) {
		defer func() {
			if r := recover(); r != nil {
				w.printf("\n!! %s panicked: %v\n", name, r)
				w.summary("error."+name, fmt.Sprintf("%v", r))
				fmt.Printf("  %s failed: %v (continuing)\n", name, r)
			}
		}()
		f(w)
	}

	run("host", describeHost)
	if opts.scope == "full" {
		run("flags", probeFlags)
		run("reflow", probeReflow)
		run("cmdsession", probeCmdSession)
		run("f4matrix", probeF4Matrix)
	}

	w.summary("probe.duration", time.Since(start).Round(time.Second).String())
	w.printf("\n\nfinished in %v\n", time.Since(start).Round(time.Second))

	logPath := filepath.Join(probeOutputDir(), logName)
	if err := w.save(logPath); err != nil {
		fmt.Println("could not write", logPath, ":", err)
		os.Exit(1)
	}
	_ = os.WriteFile(logPath+".done", []byte("ok\n"), 0644)

	fmt.Println()
	if ptyBroken != "" {
		fmt.Println("!! The pseudoconsole produced no output, so sections 3 and 4 were")
		fmt.Println("!! skipped:", ptyBroken)
		fmt.Println("!! Please send the log anyway -- section 1 and the failure lines")
		fmt.Println("!! are exactly what is needed to fix this.")
		fmt.Println()
	}
	fmt.Print(w.summaryBlock())
	fmt.Println()
	fmt.Printf("Written: %s -- please attach it and all f4probe-f4-*.log files.\n", logPath)

}
