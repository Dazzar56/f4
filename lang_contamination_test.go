package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// germanLeftovers are German key names and verb stems that have no business
// appearing in any translation except the German one. They are the fingerprint
// of the accident that started the localization audit: the German file was used
// as the base for several other languages, and words such as "Umschalt" (Shift)
// or "Entf" (Del) survived the translation pass. They also spread into
// otherwise clean files through a careless search and replace.
//
// This is deliberately a cheap canary rather than real language detection: it
// costs nothing and it caught every file that turned out to be compromised.
// See L10N_PLAN.md, stage S2.
var germanLeftovers = []string{
	"Umschalt",
	"umschaltol",
	"Strg",
	"Einfg",
	"Entf",
	"Eingabe",
	"Abbrechen",
	"Arbeitsspeicher",
	"Auswahl",
	"Berechtigungen",
	"Prozessor",
	"Protokoll",
	"Verzeichnis",
	"Vorlage",
	"Warteschlange",
}

func TestTranslationsAreFreeOfGermanLeftovers(t *testing.T) {
	paths, err := filepath.Glob("lang/*.lng")
	if err != nil {
		t.Fatalf("cannot list language files: %v", err)
	}
	helpPaths, err := filepath.Glob("help/*.hlf")
	if err != nil {
		t.Fatalf("cannot list help files: %v", err)
	}
	paths = append(paths, helpPaths...)

	if len(paths) == 0 {
		t.Fatal("no localization files found, is the test running from the repository root?")
	}

	for _, path := range paths {
		base := filepath.Base(path)
		code := base[:len(base)-len(filepath.Ext(base))]
		if code == "de" {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("cannot read %s: %v", path, err)
			continue
		}

		for i, line := range strings.Split(string(data), "\n") {
			for _, token := range germanLeftovers {
				if strings.Contains(line, token) {
					t.Errorf("%s:%d contains the German word %q, which means the text was copied from the German translation instead of being translated:\n\t%s",
						path, i+1, token, strings.TrimSpace(line))
				}
			}
		}
	}
}
