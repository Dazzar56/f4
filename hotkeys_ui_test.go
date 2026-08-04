package main

import (
	"testing"
)

func TestHotkeyRow(t *testing.T) {
	row := hotkeyRow{
		Action: "Test.Action",
		Label:  "Test Label",
		Area:   "Common",
		Key:    "F12",
		Desc:   "Description",
	}

	if row.GetCellText(0) != "Test Label" {
		t.Errorf("Expected Test Label")
	}
	if row.GetCellText(1) != "F12" {
		t.Errorf("Expected F12")
	}
	if row.GetCellText(2) != "Common" {
		t.Errorf("Expected Common")
	}
	if row.GetCellText(3) != "Description" {
		t.Errorf("Expected Description")
	}
}
