package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnforceColorCorrection_ConfigRoundtrip(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "f4-config-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	oldGetConfig := getUserConfigIniPath
	defer func() { getUserConfigIniPath = oldGetConfig }()
	getUserConfigIniPath = func() string {
		return filepath.Join(tmpDir, "settings.ini")
	}

	AppConfig.EnforceColorCorrection = true
	SaveConfig()

	LoadConfig()
	if !AppConfig.EnforceColorCorrection {
		t.Errorf("expected EnforceColorCorrection to be saved as true and loaded as true")
	}

	AppConfig.EnforceColorCorrection = false
	SaveConfig()

	LoadConfig()
	if AppConfig.EnforceColorCorrection {
		t.Errorf("expected EnforceColorCorrection to be saved as false and loaded as false")
	}
}
