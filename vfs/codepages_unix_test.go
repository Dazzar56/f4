//go:build !windows
// +build !windows

package vfs

import (
	"os"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

func TestUnixSystemEncodings(t *testing.T) {
	origLang := os.Getenv("LANG")
	origLcAll := os.Getenv("LC_ALL")
	origLcCtype := os.Getenv("LC_CTYPE")
	defer func() {
		os.Setenv("LANG", origLang)
		os.Setenv("LC_ALL", origLcAll)
		os.Setenv("LC_CTYPE", origLcCtype)
	}()

	os.Setenv("LC_ALL", "")
	os.Setenv("LC_CTYPE", "")

	// Russian locale → OEM CP866, ANSI CP1251
	os.Setenv("LANG", "ru_RU.UTF-8")
	oem := GetSystemOEMEncoding()
	ansi := GetSystemANSIEncoding()
	if oem != charmap.CodePage866 {
		t.Errorf("Expected OEM CP866 for ru_RU, got %v", oem)
	}
	if ansi != charmap.Windows1251 {
		t.Errorf("Expected ANSI CP1251 for ru_RU, got %v", ansi)
	}

	// Czech locale → OEM CP852, ANSI CP1250
	os.Setenv("LANG", "cs_CZ.UTF-8")
	oem = GetSystemOEMEncoding()
	ansi = GetSystemANSIEncoding()
	if oem != charmap.CodePage852 {
		t.Errorf("Expected OEM CP852 for cs_CZ, got %v", oem)
	}
	if ansi != charmap.Windows1250 {
		t.Errorf("Expected ANSI CP1250 for cs_CZ, got %v", ansi)
	}

	// POSIX → fallback to defaults (CP437 / CP1252)
	os.Setenv("LANG", "C")
	oem = GetSystemOEMEncoding()
	ansi = GetSystemANSIEncoding()
	if oem != charmap.CodePage437 {
		t.Errorf("Expected OEM CP437 for C locale, got %v", oem)
	}
	if ansi != charmap.Windows1252 {
		t.Errorf("Expected ANSI CP1252 for C locale, got %v", ansi)
	}
}
