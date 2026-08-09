package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestLangConsistency(t *testing.T) {
	enData, err := os.ReadFile(filepath.Join("lang", "en.lng"))
	if err != nil {
		t.Fatalf("Failed to read en.lng: %v", err)
	}

	enIni := ParseIni(bytes.NewReader(enData))
	enStrings := loadLangMapFromINI(enIni)

	var enKeys []string
	lines := strings.Split(string(enData), "\n")
	inStringsSection := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[Strings]") {
			inStringsSection = true
			continue
		} else if strings.HasPrefix(line, "[") {
			inStringsSection = false
			continue
		}
		if !inStringsSection || line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx > 0 {
			key := strings.TrimSpace(line[:idx])
			if _, ok := enStrings[key]; ok {
				found := false
				for _, k := range enKeys {
					if k == key {
						found = true
						break
					}
				}
				if !found {
					enKeys = append(enKeys, key)
				}
			}
		}
	}

	files, err := filepath.Glob(filepath.Join("lang", "*.lng"))
	if err != nil {
		t.Fatalf("Failed to glob lang/*.lng: %v", err)
	}

	placeholderRe := regexp.MustCompile(`%[sdvq]`)

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Errorf("Failed to read %s: %v", file, err)
			continue
		}

		ini := ParseIni(bytes.NewReader(data))
		stringsMap := loadLangMapFromINI(ini)

		code := ini.GetString("Language", "Code", "")
		expectedCode := strings.TrimSuffix(filepath.Base(file), ".lng")
		if code != expectedCode {
			t.Errorf("%s: [Language] Code is '%s', expected '%s'", file, code, expectedCode)
		}
		if ini.GetString("Language", "Name", "") == "" {
			t.Errorf("%s: [Language] Name is missing", file)
		}

		if filepath.Base(file) == "en.lng" {
			continue
		}

		seenKeys := make(map[string]bool)
		flines := strings.Split(string(data), "\n")
		targetInStringsSection := false
		for _, line := range flines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "[Strings]") {
				targetInStringsSection = true
				continue
			} else if strings.HasPrefix(line, "[") {
				targetInStringsSection = false
				continue
			}
			if !targetInStringsSection || line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
				continue
			}
			idx := strings.Index(line, "=")
			if idx > 0 {
				key := strings.TrimSpace(line[:idx])
				if seenKeys[key] {
					t.Errorf("%s: Duplicate key found: %s", file, key)
				}
				seenKeys[key] = true

				if _, ok := enStrings[key]; !ok {
					t.Errorf("%s: Key '%s' does not exist in en.lng", file, key)
				}
			} else {
				t.Errorf("%s: Invalid line without '=' in [Strings]: %s", file, line)
			}
		}

		for _, key := range enKeys {
			enVal := enStrings[key]
			val, ok := stringsMap[key]
			if !ok {
				t.Errorf("%s: Missing key '%s'", file, key)
				continue
			}

			enNewlines := strings.Count(enVal, "\n")
			valNewlines := strings.Count(val, "\n")
			if enNewlines != valNewlines {
				t.Errorf("%s: Key '%s' has %d newlines, expected %d", file, key, valNewlines, enNewlines)
			}

			enPlaces := placeholderRe.FindAllString(enVal, -1)
			valPlaces := placeholderRe.FindAllString(val, -1)
			if len(enPlaces) != len(valPlaces) {
				t.Errorf("%s: Key '%s' has %v placeholders, expected %v", file, key, valPlaces, enPlaces)
			} else {
				for i := range enPlaces {
					if enPlaces[i] != valPlaces[i] {
						t.Errorf("%s: Key '%s' placeholder mismatch at %d: %s vs %s", file, key, i, valPlaces[i], enPlaces[i])
					}
				}
			}

			// Check & hotkeys
			valNoDbl := strings.ReplaceAll(val, "&&", "")
			ampCount := strings.Count(valNoDbl, "&")
			if ampCount > 1 {
				t.Errorf("%s: Key '%s' has multiple single '&'", file, key)
			} else if ampCount == 1 {
				if strings.HasSuffix(valNoDbl, "&") {
					t.Errorf("%s: Key '%s' has trailing '&'", file, key)
				}
			}
		}
	}

	baselineData, err := os.ReadFile(filepath.Join("lang", "coverage_baseline.txt"))
	if err == nil {
		baselineLines := strings.Split(string(baselineData), "\n")
		for _, line := range baselineLines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Split(line, "=")
			if len(parts) == 2 {
				code := parts[0]
				var expectedCount int
				fmt.Sscanf(parts[1], "%d", &expectedCount)

				file := filepath.Join("lang", code+".lng")
				data, err := os.ReadFile(file)
				if err != nil {
					t.Errorf("Coverage baseline requires %s but file is missing", code)
					continue
				}
				ini := ParseIni(bytes.NewReader(data))
				stringsMap := loadLangMapFromINI(ini)
				if len(stringsMap) < expectedCount {
					t.Errorf("%s has %d keys, baseline requires at least %d", code, len(stringsMap), expectedCount)
				}
			}
		}
	}
}
