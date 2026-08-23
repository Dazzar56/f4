package main

import (
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// discoverInstalledGuiFonts is a variable so the settings dialog and the
// language recommendation can share one catalog while tests can avoid
// depending on the host's installed fonts.
var discoverInstalledGuiFonts = platformGuiFontFiles

// guiFontChoices keeps the current value even when it is a manually entered
// path or family name that the platform catalog cannot discover.
func guiFontChoices(language, current string) []string {
	choices := make([]string, 0)
	appendUnique := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range choices {
			if sameGuiFontValue(existing, value) {
				return
			}
		}
		choices = append(choices, value)
	}

	appendUnique(current)
	for _, path := range discoverInstalledGuiFonts(language) {
		appendUnique(path)
	}
	return choices
}

// platformGuiFontDisplayChoices and platformGuiFontDisplayName are indirection
// variables so the non-Windows build never references the Windows-only font
// name helpers: those live in the //go:build windows file and override these
// from init there. The defaults keep the previous path-based behaviour.
var platformGuiFontDisplayChoices = func(language, current string) []string {
	return guiFontChoices(language, current)
}

var platformGuiFontDisplayName = func(value string) string {
	return value
}

// guiFontDisplayChoices returns the strings shown in the font picker. On
// Windows these are font family names (e.g. "Cascadia Mono") instead of file
// paths; elsewhere the platform catalog has no name metadata, so the paths are
// returned as before.
func guiFontDisplayChoices(language, current string) []string {
	return platformGuiFontDisplayChoices(language, current)
}

// guiFontDisplayName maps a stored font value to its picker label, resolving a
// Windows font file path back to its family name when possible.
func guiFontDisplayName(value string) string {
	return platformGuiFontDisplayName(value)
}

func sameGuiFontValue(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func shouldSuggestFontForLanguage(language, current string) bool {
	if !isCJKLanguage(language) {
		return false
	}
	if strings.TrimSpace(current) == "" {
		return len(discoverInstalledGuiFonts(language)) > 0
	}
	for _, path := range discoverInstalledGuiFonts(language) {
		if sameGuiFontValue(current, path) {
			return false
		}
	}
	return true
}

func isCJKLanguage(language string) bool {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" {
		return false
	}
	for _, prefix := range []string{"zh", "ja", "ko"} {
		if language == prefix || strings.HasPrefix(language, prefix+"_") || strings.HasPrefix(language, prefix+"-") {
			return true
		}
	}
	return false
}

func cjkFontconfigPattern(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	switch {
	case strings.HasPrefix(language, "ja"):
		return ":lang=ja"
	case strings.HasPrefix(language, "ko"):
		return ":lang=ko"
	case strings.HasPrefix(language, "zh"):
		return ":lang=zh"
	default:
		// 100 is fontconfig's spacing value for monospace fonts. The
		// explicit value is more portable than the human-readable alias.
		return ":spacing=100"
	}
}

func isFontFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".otc", ".otf", ".ttc", ".ttf":
		return true
	default:
		return false
	}
}

func sortCJKFontPaths(paths []string, language string) {
	if !isCJKLanguage(language) {
		sort.Strings(paths)
		return
	}
	sort.SliceStable(paths, func(i, j int) bool {
		iCJK := looksLikeCJKFontPath(paths[i])
		jCJK := looksLikeCJKFontPath(paths[j])
		if iCJK != jCJK {
			return iCJK
		}
		return paths[i] < paths[j]
	})
}

func looksLikeCJKFontPath(path string) bool {
	path = strings.ToLower(path)
	for _, marker := range []string{
		"cjk", "chinese", "droid", "gothic", "han", "japan", "jp", "korea", "ko", "ming", "noto", "simsun", "song", "wqy", "yahei",
	} {
		if strings.Contains(path, marker) {
			return true
		}
	}
	return false
}

func parseFontconfigPaths(output string) []string {
	var paths []string
	seen := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		path := strings.TrimSpace(line)
		if path == "" || !isFontFile(path) {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
