// Package hostmode holds the single, once-decided answer to "which
// personality does the file layer run in" (WINE.md §13, Part E): posix
// (real POSIX paths and syscalls via libwinescape) or windows (today's
// Win32/os.* behavior, unchanged). vfs/hostfs and vfs/hostpath both consult
// it so the decision is made in exactly one place, not duplicated.
package hostmode

import (
	"os"
	"sync"

	winescape "github.com/unxed/libwinescape/go"
)

var (
	once  sync.Once
	posix bool
)

// Posix reports whether the host-filesystem layer should run in posix
// personality. Decided once, the first time this is called, and never
// changes afterward: WINE.md §13.4 requires this, because switching live
// would race panel goroutines into a half-switched state. A future config
// setting (WINE.md Stage D7/E6) applies from the next restart, not live --
// this function is where that restriction is enforced by construction, not
// just by convention.
//
// On every non-Windows GOOS this is unconditionally false: os.*/path/filepath
// already speak POSIX there, so hostfs/hostpath never have a second
// personality to switch to (winescape.IsWine/Available are themselves
// hard-wired false outside Windows, so this would resolve to false even
// without the GOOS check -- it's here for clarity, not correctness).
func Posix() bool {
	once.Do(func() {
		// Escape hatch ahead of the real config setting (WINE.md Stage D7):
		// F4_WINE_POSIX=1 forces posix mode on for debugging/testing even if
		// the automatic probe below would say no; =0 forces it off. Anything
		// else (unset, or any other value) falls through to auto-detection.
		switch os.Getenv("F4_WINE_POSIX") {
		case "1":
			posix = true
			return
		case "0":
			posix = false
			return
		}
		posix = winescape.IsWine() && winescape.Available()
	})
	return posix
}
