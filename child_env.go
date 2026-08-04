package main

// The environment the built-in terminal hands to the program it starts. This
// is where the terminal says what it can do: a program that draws pictures
// picks its protocol long before it prints anything, and it picks by looking
// at these variables.

import (
	"os"
	"strings"

	"github.com/unxed/vtui"
)

// kittyGraphicsEnv is the variable kitty exports and image tools look for.
const kittyGraphicsEnv = "KITTY_WINDOW_ID"

// terminalChildEnv builds the environment of a program started in the
// built-in terminal.
func terminalChildEnv() []string {
	return buildChildEnv(os.Environ(), terminalShowsImages())
}

// buildChildEnv is the half of terminalChildEnv that depends on nothing but
// its arguments.
func buildChildEnv(env []string, graphics bool) []string {
	out := make([]string, 0, len(env)+3)
	for _, kv := range env {
		// Whatever we inherited describes the terminal that started f4; the
		// program we are about to start talks to us instead.
		if strings.HasPrefix(kv, kittyGraphicsEnv+"=") || strings.HasPrefix(kv, "TERM_PROGRAM=") {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, "F4_NESTED=1", "TERM_PROGRAM=f4")

	if graphics {
		// The built-in terminal speaks the kitty graphics protocol, so it
		// says so the way kitty itself does. Claiming it while the screen
		// f4 draws on cannot show a picture would only make programs
		// produce output nobody sees.
		out = append(out, kittyGraphicsEnv+"=1")
	}
	return out
}

// terminalShowsImages reports whether the screen f4 draws on can display
// images at all.
func terminalShowsImages() bool {
	scr := vtui.FrameManager.Screen()
	return scr != nil && scr.SupportsGraphics()
}