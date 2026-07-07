package main

import (
	"bytes"
	"testing"
)

func TestExtUiProtocolRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := map[string]any{
		"type":   "frame",
		"width":  2,
		"height": 1,
		"full":   true,
		"cells":  [][3]uint64{{0, 'A', 0x010203}, {1, 'B', 0x040506}},
	}
	if err := extUiSendMessage(&buf, want); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	got, err := extUiReadMessage(&buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if extUiString(got, "type") != "frame" {
		t.Fatalf("type mismatch: %q", extUiString(got, "type"))
	}
	if extUiInt(got, "width") != 2 || extUiInt(got, "height") != 1 {
		t.Fatalf("size mismatch: %dx%d", extUiInt(got, "width"), extUiInt(got, "height"))
	}
	if !extUiBool(got, "full") {
		t.Fatal("full flag was not preserved")
	}
}