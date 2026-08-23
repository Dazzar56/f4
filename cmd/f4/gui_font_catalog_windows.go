//go:build windows

package main

import (
	"sort"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func init() {
	platformGuiFontDisplayChoices = windowsGuiFontDisplayChoices
	platformGuiFontDisplayName = windowsGuiFontDisplayName
}

func windowsFontEntries() []fontEntry {
	var entries []fontEntry
	for _, hive := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		key, err := registry.OpenKey(hive, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		names, err := key.ReadValueNames(-1)
		if err == nil {
			for _, name := range names {
				file, _, err := key.GetStringValue(name)
				if err != nil || !isFontFile(file) {
					continue
				}
				base := strings.TrimSpace(strings.TrimSuffix(name, " (TrueType)"))
				base = strings.TrimSpace(strings.TrimSuffix(base, " (OpenType)"))
				entries = append(entries, fontEntry{base: base, file: file})
			}
		}
		key.Close()
	}
	return entries
}

func platformGuiFontFiles(language string) []string {
	entries := windowsFontEntries()
	paths := make([]string, 0, len(entries))
	seen := make(map[string]struct{})
	for _, entry := range entries {
		path := fontFilePath(entry.file)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	if isCJKLanguage(language) {
		sort.SliceStable(paths, func(i, j int) bool {
			iCJK := looksLikeCJKFontPath(paths[i])
			jCJK := looksLikeCJKFontPath(paths[j])
			if iCJK != jCJK {
				return iCJK
			}
			return strings.ToLower(paths[i]) < strings.ToLower(paths[j])
		})
	} else {
		sort.Strings(paths)
	}
	return paths
}

// windowsGuiFontDisplayChoices returns the font family names to show in the
// picker (e.g. "Cascadia Mono") instead of file paths.
func windowsGuiFontDisplayChoices(language, current string) []string {
	entries := windowsFontEntries()
	pathToName := make(map[string]string)
	nameNormToName := make(map[string]string)
	seenName := make(map[string]struct{})
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		pathToName[strings.ToLower(fontFilePath(e.file))] = e.base
		nameNormToName[normalizeFontName(e.base)] = e.base
		if _, ok := seenName[strings.ToLower(e.base)]; ok {
			continue
		}
		seenName[strings.ToLower(e.base)] = struct{}{}
		names = append(names, e.base)
	}

	choices := make([]string, 0, len(names)+1)
	appendUnique := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range choices {
			if strings.EqualFold(existing, value) {
				return
			}
		}
		choices = append(choices, value)
	}

	// Show the current value by its family name when it resolves, otherwise
	// keep it verbatim (a manual path or custom family the catalog lacks).
	if current != "" {
		if name, ok := pathToName[strings.ToLower(fontFilePath(current))]; ok {
			appendUnique(name)
		} else if name, ok := nameNormToName[normalizeFontName(current)]; ok {
			appendUnique(name)
		} else {
			appendUnique(current)
		}
	}
	for _, name := range names {
		appendUnique(name)
	}

	if isCJKLanguage(language) {
		sort.SliceStable(choices, func(i, j int) bool {
			iCJK := looksLikeCJKFontName(choices[i])
			jCJK := looksLikeCJKFontName(choices[j])
			if iCJK != jCJK {
				return iCJK
			}
			return strings.ToLower(choices[i]) < strings.ToLower(choices[j])
		})
	} else {
		sort.SliceStable(choices, func(i, j int) bool {
			return strings.ToLower(choices[i]) < strings.ToLower(choices[j])
		})
	}
	return choices
}

// windowsGuiFontDisplayName maps a stored font value to its family name label.
func windowsGuiFontDisplayName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	entries := windowsFontEntries()
	for _, e := range entries {
		if strings.EqualFold(fontFilePath(e.file), fontFilePath(value)) {
			return e.base
		}
		if normalizeFontName(e.base) == normalizeFontName(value) {
			return e.base
		}
	}
	return value
}

func normalizeFontName(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", ""))
}

func looksLikeCJKFontName(name string) bool {
	name = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", ""))
	for _, marker := range []string{
		"cjk", "chinese", "droid", "gothic", "han", "japan", "jp", "korea", "ko", "ming", "noto", "simsun", "song", "wqy", "yahei",
	} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}
