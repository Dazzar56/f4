package main

import (
	"path/filepath"
	"testing"
)

func TestConfig_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()

	origPathFunc := getConfigIniPath
	getConfigIniPath = func() string { return filepath.Join(tmpDir, "settings.ini") }

	// Save original config to restore after test
	oldCfg := AppConfig
	defer func() {
		getConfigIniPath = origPathFunc
		AppConfig = oldCfg
	}()

	// 1. Set some non-default values
	AppConfig.ShowHiddenFiles = false
	AppConfig.HighlightDir = true

	// 2. Save
	SaveConfig()

	// 3. Reset to defaults
	AppConfig.ShowHiddenFiles = true
	AppConfig.HighlightDir = false

	// 4. Load
	LoadConfig()

	// 5. Verify
	if AppConfig.ShowHiddenFiles {
		t.Error("LoadConfig failed to restore ShowHiddenFiles")
	}
	if !AppConfig.HighlightDir {
		t.Error("LoadConfig failed to restore HighlightDir")
	}
}