package vfs

import (
	"testing"
)

func TestCodepages_Basic(t *testing.T) {
	if _, ok := FindCodepage(65001); !ok {
		t.Error("Expected UTF-8 to be found")
	}

	cp, ok := DetectBOM([]byte{0xEF, 0xBB, 0xBF, 'a'})
	if !ok || cp != 65001 {
		t.Errorf("BOM detection failed: got %d, %v", cp, ok)
	}

	testStr := "Привет"
	encoded, err := EncodeBytes([]byte(testStr), 1251)
	if err != nil {
		t.Fatalf("Failed to encode CP1251: %v", err)
	}
	decoded, err := DecodeBytes(encoded, 1251)
	if err != nil {
		t.Fatalf("Failed to decode CP1251: %v", err)
	}
	if string(decoded) != testStr {
		t.Errorf("Roundtrip failed: expected %q, got %q", testStr, string(decoded))
	}
}
