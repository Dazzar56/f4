//go:build windows

package vfs

import (
	winescape "github.com/unxed/libwinescape/go"
)

// winescapeReadDirNames lists the entries of dirPath using libwinescape's raw
// getdents64 (bypassing the Win32/wineserver directory-enumeration round
// trip Wine would otherwise translate every FindFirstFile/FindNextFile call
// into). Returns ok=false whenever the fast path isn't usable or fails for
// any reason -- on native Windows (winescape.Available() is always false
// there) and on any host OS under Wine other than Linux (see
// libwinescape's go/detect_windows.go for exactly which hosts that is and
// why). Callers must fall back to the existing os.Open/(*os.File).ReadDir
// path unchanged whenever ok is false; this function never returns a
// partial or best-effort result.
//
// Per WINE.md Stage D5: this is additive only. It changes how directory
// names are discovered under Wine on a confirmed-safe host; it does not
// change how any entry's metadata (size, mtime, symlink/exec bits) is
// determined -- callers still stat each name exactly as before.
func winescapeReadDirNames(dirPath string) (names []string, ok bool) {
	if !winescape.Available() {
		return nil, false
	}
	entries, err := winescape.ReadDir(dirPath)
	if err != nil {
		return nil, false
	}
	names = make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		names = append(names, e.Name)
	}
	return names, true
}
