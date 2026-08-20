//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

type fontEntry struct {
	base string
	file string
}

// windowsFontFile resolves a Windows font family name (as shown in settings)
// to the actual font file path via the registry. vtui's getFontCandidates only
// matches file names literally, so "Cascadia Mono" would otherwise miss
// CascadiaMono.ttf and silently fall back to Consolas. Returns "" if the
// family is not found; callers then keep the original name.
func windowsFontFile(fontName string) string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()

	names, err := key.ReadValueNames(-1)
	if err != nil {
		return ""
	}

	var entries []fontEntry
	for _, n := range names {
		file, _, err := key.GetStringValue(n)
		if err != nil {
			continue
		}
		switch strings.ToLower(filepath.Ext(file)) {
		case ".ttf", ".ttc", ".otf":
		default:
			continue
		}
		base := strings.TrimSpace(strings.TrimSuffix(n, " (TrueType)"))
		base = strings.TrimSpace(strings.TrimSuffix(base, " (OpenType)"))
		entries = append(entries, fontEntry{base: base, file: file})
	}
	return matchWindowsFontFamily(fontName, entries)
}

func matchWindowsFontFamily(fontName string, entries []fontEntry) string {
	fontName = strings.TrimSpace(fontName)
	if fontName == "" {
		return ""
	}
	want := strings.ToLower(fontName)

	for _, e := range entries {
		if strings.ToLower(e.base) == want {
			return fontFilePath(e.file)
		}
	}
	// The registry records families with their style, e.g. "Cascadia Mono
	// Regular", so accept family + " <style>". Prefer the regular weight when
	// more than one style is installed.
	var first string
	for _, e := range entries {
		got := strings.ToLower(e.base)
		if !strings.HasPrefix(got, want+" ") {
			continue
		}
		if strings.Contains(got, "regular") {
			return fontFilePath(e.file)
		}
		if first == "" {
			first = fontFilePath(e.file)
		}
	}
	return first
}

func fontFilePath(f string) string {
	if filepath.IsAbs(f) {
		return f
	}
	dir := os.Getenv("WINDIR")
	if dir == "" {
		dir = `C:\Windows`
	}
	return filepath.Join(dir, "Fonts", f)
}
