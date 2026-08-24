package main

import (
	"os"
	"strconv"

	"github.com/unxed/vtui"
)

// PreferCompatibleGraphicsProtocol switches Konsole to the existing kitty
// transport. Konsole's sixel decoder changes indexed palette entries in place,
// while vtui's true-color sixel encoder intentionally changes the palette for
// every sixel band. That combination recolors bands that were already drawn.
//
// This is deliberately an application-level choice: the shared sixel encoder
// remains unchanged for terminals that handle its full-color output correctly.
func PreferCompatibleGraphicsProtocol(scr *vtui.ScreenBuf) {
	preferCompatibleGraphicsProtocol(scr, os.Getenv)
}

func preferCompatibleGraphicsProtocol(scr *vtui.ScreenBuf, env func(string) string) {
	if scr == nil || !konsoleKittyGraphicsAvailable(env) {
		return
	}

	// A valid VTUI_GRAPHICS value is an explicit user choice. Do not silently
	// replace it, especially VTUI_GRAPHICS=sixel, which remains useful on
	// Konsole versions where the user has a reason to force that protocol.
	if forced := env("VTUI_GRAPHICS"); forced != "" {
		if _, ok := vtui.ParseGraphicsProtocol(forced); ok {
			return
		}
	}

	if scr.Graphics().Protocol() == vtui.GraphicsSixel {
		scr.Graphics().SetProtocol(vtui.GraphicsKitty)
		vtui.DebugLog("GRAPHICS: Konsole sixel is palette-incompatible; using kitty graphics")
	}
}

func konsoleKittyGraphicsAvailable(env func(string) string) bool {
	version := env("KONSOLE_VERSION")
	if version == "" {
		return false
	}

	// Konsole exports a numeric version such as 220400. Kitty graphics support
	// is present in Konsole 22.04 and later. Keep older versions on their
	// existing sixel path instead of sending a protocol they do not know.
	numericVersion, err := strconv.Atoi(version)
	return err == nil && numericVersion >= 220400
}
