//go:build windows

package vfs

import (
	"path"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// wine_get_unix_file_name is a kernel32.dll export, wine_get_host_version an
// ntdll.dll one — both specific to Wine (see WINE.md §5, Этап B0). They are
// cdecl, which on amd64/arm64 has the same calling convention as stdcall, so
// a plain syscall.LazyProc.Call works. This file lives in package vfs (moved
// here so vfs.OSVFS can call it directly -- package main cannot be imported
// by vfs), so it declares its own kernel32/ntdll handles rather than sharing
// main's; syscall.NewLazyDLL is idempotent per-process, so a second LazyDLL
// for the same DLL name from a different package costs nothing extra.
var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	ntdllWine               = syscall.NewLazyDLL("ntdll.dll")
	procWineGetUnixFileName = kernel32.NewProc("wine_get_unix_file_name")
	procWineGetHostVersion  = ntdllWine.NewProc("wine_get_host_version")
	procGetProcessHeap      = kernel32.NewProc("GetProcessHeap")
	procHeapFree            = kernel32.NewProc("HeapFree")
)

// wineUnixPrefix is the canonical form WINE.md §5/B1 settles on for talking
// to arbitrary unix paths from Win32 code: Wine accepts it as a DOS path
// (same trick used internally by wine_get_dos_file_name and by start.exe
// /unix), and Go's os.* calls leave \\?\-prefixed paths untouched
// (fixLongPath), so filepath.Clean/Join must never see it.
const wineUnixPrefix = `\\?\unix`

// WineAvailable reports whether the Wine unix-path exports are present.
// It is false both on real Windows and, harmlessly, on any non-Windows OS
// (see winepath_other.go) — no environment probing required to call it.
func WineAvailable() bool {
	return procWineGetUnixFileName.Find() == nil
}

// WineHostOS returns the host OS name Wine reports (e.g. "Linux", "FreeBSD",
// "Haiku"), or "" if unavailable. The returned C string is never freed: per
// WINE.md §5/B0 wine_get_host_version's strings do not need HeapFree.
func WineHostOS() string {
	if procWineGetHostVersion.Find() != nil {
		return ""
	}
	var sysnamePtr, releasePtr uintptr
	procWineGetHostVersion.Call(
		uintptr(unsafe.Pointer(&sysnamePtr)),
		uintptr(unsafe.Pointer(&releasePtr)),
	)
	if sysnamePtr == 0 {
		return ""
	}
	return cStringFromPtr(sysnamePtr)
}

// WineDOSFromUnix converts a POSIX path into the \\?\unix\... form that
// Go's os.* calls can use directly under Wine. Purely textual — no Wine
// call, works for paths that don't exist yet (needed for file creation).
// ok is false only for input that isn't an absolute POSIX path.
func WineDOSFromUnix(unixPath string) (string, bool) {
	if !strings.HasPrefix(unixPath, "/") {
		return "", false
	}
	clean := path.Clean(unixPath)
	tail := strings.ReplaceAll(clean, "/", `\`)
	if tail == `\` {
		tail = ""
	}
	return wineUnixPrefix + tail, true
}

// WineUnixFromDOS converts an OS-facing path back to POSIX form. If the
// path already carries the \\?\unix (or NT \??\unix) prefix, the
// conversion is purely textual and always succeeds. Otherwise the input is
// treated as a genuine DOS path (e.g. "C:\...") and resolved via
// wine_get_unix_file_name, which opens the file and is therefore cached;
// this branch needs a live Wine and returns ok=false without one.
func WineUnixFromDOS(dosPath string) (string, bool) {
	if rest, matched := stripWineUnixPrefix(dosPath); matched {
		unixPath := strings.ReplaceAll(rest, `\`, "/")
		if !strings.HasPrefix(unixPath, "/") {
			unixPath = "/" + unixPath
		}
		return path.Clean(unixPath), true
	}
	return wineUnixFromDOSCached(dosPath)
}

// WineUnixFromDOSFast is WineUnixFromDOS without the wine_get_unix_file_name
// fallback: it succeeds only when dosPath already carries the \\?\unix (or
// NT \??\unix) prefix, purely as text, and never calls into Wine or opens
// anything. Safe to call from hot paths (redraw, status line) where
// WineUnixFromDOS's cached-but-still-real Wine call on a cache miss would
// not be.
func WineUnixFromDOSFast(dosPath string) (string, bool) {
	rest, matched := stripWineUnixPrefix(dosPath)
	if !matched {
		return "", false
	}
	unixPath := strings.ReplaceAll(rest, `\`, "/")
	if !strings.HasPrefix(unixPath, "/") {
		unixPath = "/" + unixPath
	}
	return path.Clean(unixPath), true
}

// stripWineUnixPrefix recognizes both the Win32 (\\?\unix) and NT (\??\unix)
// spellings of the Wine unix-path prefix, case-insensitively.
func stripWineUnixPrefix(p string) (rest string, ok bool) {
	for _, prefix := range [...]string{`\\?\unix`, `\??\unix`} {
		if len(p) >= len(prefix) && strings.EqualFold(p[:len(prefix)], prefix) {
			return p[len(prefix):], true
		}
	}
	return "", false
}

type wineUnixFromDOSResult struct {
	path string
	ok   bool
}

var wineUnixFromDOSCache sync.Map // map[string]wineUnixFromDOSResult

func wineUnixFromDOSCached(dosPath string) (string, bool) {
	if v, found := wineUnixFromDOSCache.Load(dosPath); found {
		r := v.(wineUnixFromDOSResult)
		return r.path, r.ok
	}
	unixPath, ok := wineGetUnixFileNameRaw(dosPath)
	if ok {
		unixPath = path.Clean(unixPath)
	}
	wineUnixFromDOSCache.Store(dosPath, wineUnixFromDOSResult{unixPath, ok})
	return unixPath, ok
}

func wineGetUnixFileNameRaw(dosPath string) (string, bool) {
	if procWineGetUnixFileName.Find() != nil {
		return "", false
	}
	dosPtr, err := windows.UTF16PtrFromString(dosPath)
	if err != nil {
		return "", false
	}
	ret, _, _ := procWineGetUnixFileName.Call(uintptr(unsafe.Pointer(dosPtr)))
	if ret == 0 {
		return "", false
	}
	defer freeWineBuffer(ret)
	return cStringFromPtr(ret), true
}

func freeWineBuffer(ptr uintptr) {
	heap, _, _ := procGetProcessHeap.Call()
	if heap == 0 {
		return
	}
	procHeapFree.Call(heap, 0, ptr)
}

// cStringFromPtr reads a NUL-terminated byte string (CP_UNIXCP, UTF-8 in
// practice) starting at ptr.
func cStringFromPtr(ptr uintptr) string {
	var b []byte
	for i := uintptr(0); ; i++ {
		c := *(*byte)(unsafe.Pointer(ptr + i))
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	return string(b)
}
