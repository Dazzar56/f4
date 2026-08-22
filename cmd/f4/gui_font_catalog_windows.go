//go:build windows

package main

import (
	"sort"
	"strings"

	"golang.org/x/sys/windows/registry"
)

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
