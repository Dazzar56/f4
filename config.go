package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/unxed/vtui"
)

type F4Config struct {
	ShowHiddenFiles bool
	HighlightDir    bool
}

var AppConfig = F4Config{
	ShowHiddenFiles: true,
	HighlightDir:    false,
}

var getConfigIniPath = func() string {
	configDir, _ := os.UserConfigDir()
	return filepath.Join(configDir, "f4", "settings.ini")
}

func LoadConfig() {
	path := getConfigIniPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}
	ini := LoadIni(path)

	AppConfig.ShowHiddenFiles = ini.GetString("Panel", "ShowHiddenFiles", "1") == "1"
	AppConfig.HighlightDir = ini.GetString("Panel", "HighlightDir", "0") == "1"

	vtui.DebugLog("CONFIG: Loaded application settings from %s", path)
}

func SaveConfig() {
	path := getConfigIniPath()
	os.MkdirAll(filepath.Dir(path), 0755)

	var sb strings.Builder
	sb.WriteString("[Panel]\n")
	sb.WriteString(fmt.Sprintf("ShowHiddenFiles = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.ShowHiddenFiles]))
	sb.WriteString(fmt.Sprintf("HighlightDir = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.HighlightDir]))

	err := os.WriteFile(path, []byte(sb.String()), 0644)
	if err != nil {
		vtui.DebugLog("CONFIG: Failed to save application settings: %v", err)
		return
	}

	vtui.DebugLog("CONFIG: Saved application settings to %s", path)
}