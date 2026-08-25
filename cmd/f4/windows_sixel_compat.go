package main

import (
	"os"
	"strings"
)

// configureWindowsTerminalSixel selects the single-palette encoder for
// Windows Terminal. Its sixel parser keeps image pixels indexed until it
// flushes a raster, so redefining a register for a later band can recolour
// earlier bands. Adaptive keeps the picture free of dithering while staying
// within the palette semantics Windows Terminal reliably supports.
//
// An explicit VTUI_SIXEL_PALETTE remains authoritative. This is deliberately
// an environment-level compatibility switch because vtui reads that setting
// when it lazily creates its sixel encoder.
func configureWindowsTerminalSixel() {
	configureWindowsTerminalSixelWith(os.Getenv, os.Setenv)
}

func configureWindowsTerminalSixelWith(env func(string) string, setenv func(string, string) error) {
	if env == nil || setenv == nil || env("WT_SESSION") == "" {
		return
	}
	if strings.TrimSpace(env("VTUI_SIXEL_PALETTE")) != "" {
		return
	}
	_ = setenv("VTUI_SIXEL_PALETTE", "adaptive")
}
