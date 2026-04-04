package vfs

import (
	"testing"
	"github.com/klauspost/compress/zip"
)

func TestDecodeZipName(t *testing.T) {
	tests := []struct {
		name     string
		flags    uint16
		version  uint16
		rawName  string
		expected string
	}{
		{
			name:     "UTF-8 flag set (Bit 11)",
			flags:    0x800,
			version:  (3 << 8) | 30,
			rawName:  "Тест_UTF8.txt",
			expected: "Тест_UTF8.txt",
		},
		{
			name:     "Windows ANSI (PackOS 11, Version 20+)",
			flags:    0x0,
			version:  (11 << 8) | 20,
			rawName:  "\xd2\xe5\xf1\xf2_ANSI.txt", // "Тест" в Windows-1251
			expected: "Тест_ANSI.txt",
		},
		{
			name:     "Windows OEM (PackOS 11, Version < 20)",
			flags:    0x0,
			version:  (11 << 8) | 15,
			rawName:  "\x92\xa5\xe1\xe2_OEM.txt", // "Тест" в CP866
			expected: "Тест_OEM.txt",
		},
		{
			name:     "DOS OEM (PackOS 0)",
			flags:    0x0,
			version:  (0 << 8) | 20,
			rawName:  "\x92\xa5\xe1\xe2_DOS.txt",
			expected: "Тест_DOS.txt",
		},
		{
			name:     "OS/2 OEM (PackOS 6)",
			flags:    0x0,
			version:  (6 << 8) | 20,
			rawName:  "\x92\xa5\xe1\xe2_OS2.txt",
			expected: "Тест_OS2.txt",
		},
		{
			name:     "Default Fallback (Unix/Other -> CP437)",
			flags:    0x0,
			version:  (3 << 8) | 30, // Unix
			rawName:  "\xabHello\xbb", // CP437: «Hello»
			expected: "½Hello╗",      // Результат декодирования CP437 байтов 0xAB/0xBB
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hdr := &zip.FileHeader{
				Flags:          tt.flags,
				CreatorVersion: tt.version,
				Name:           tt.rawName,
			}
			got := DecodeZipName(hdr)
			if got != tt.expected {
				t.Errorf("%s: got %q, want %q", tt.name, got, tt.expected)
			}
		})
	}
}