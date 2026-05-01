package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/unxed/vtui"
)

type F4Config struct {
	ShowHiddenFiles        bool
	HighlightDir           bool
	SavePanelPaths         bool
	KeepTerminalCursor     bool
	CommandLineAutoComplete bool
	EditorAutoComplete     bool
	EditorAutoCompleteMask string
	EditorExpandTabs       int
	EditorAutoIndent       bool
	EditorCursorBeyondEOL  bool
	EditorTabSize          int
	EditorUseEditorConfig  bool
	EditorCrosshair        bool
	RegisteredPlugins      []string
}

var AppConfig = F4Config{
	ShowHiddenFiles:        true,
	HighlightDir:           false,
	SavePanelPaths:         true,
	KeepTerminalCursor:     false,
	EditorAutoComplete:     true,
	EditorAutoCompleteMask: "*.go;*.c;*.cpp;*.h;*.hpp;*.py;*.js;*.ts;*.rs;*.java;*.sh;*.txt;*.md;*.html;*.css;*.json",
	EditorExpandTabs:       0,
	EditorAutoIndent:       true,
	EditorCursorBeyondEOL:  false,
	EditorTabSize:          4,
	EditorUseEditorConfig:  true,
	EditorCrosshair:        false,
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
	AppConfig.SavePanelPaths = ini.GetString("Panel", "SavePanelPaths", "1") == "1"
	AppConfig.KeepTerminalCursor = ini.GetString("Panel", "KeepTerminalCursor", "0") == "1"
	AppConfig.CommandLineAutoComplete = ini.GetString("Panel", "CommandLineAutoComplete", "1") == "1"

	AppConfig.EditorAutoComplete = ini.GetString("Editor", "AutoComplete", "1") == "1"
	AppConfig.EditorAutoCompleteMask = ini.GetString("Editor", "AutoCompleteMask", "*.go;*.c;*.cpp;*.h;*.hpp;*.py;*.js;*.ts;*.rs;*.java;*.sh;*.txt;*.md;*.html;*.css;*.json")

	AppConfig.EditorExpandTabs = 0
	fmt.Sscanf(ini.GetString("Editor", "ExpandTabs", "0"), "%d", &AppConfig.EditorExpandTabs)
	AppConfig.EditorAutoIndent = ini.GetString("Editor", "AutoIndent", "1") == "1"
	AppConfig.EditorCursorBeyondEOL = ini.GetString("Editor", "CursorBeyondEOL", "0") == "1"
	AppConfig.EditorUseEditorConfig = ini.GetString("Editor", "UseEditorConfig", "1") == "1"
	AppConfig.EditorCrosshair = ini.GetString("Editor", "Crosshair", "0") == "1"
	plugStr := ini.GetString("Plugins", "List", "")
	if plugStr != "" {
		AppConfig.RegisteredPlugins = strings.Split(plugStr, "|")
	}
	AppConfig.EditorTabSize = 4
	fmt.Sscanf(ini.GetString("Editor", "TabSize", "4"), "%d", &AppConfig.EditorTabSize)

	vtui.DebugLog("CONFIG: Loaded application settings from %s", path)
}

func SaveConfig() {
	path := getConfigIniPath()
	os.MkdirAll(filepath.Dir(path), 0755)

	var sb strings.Builder
	sb.WriteString("[Panel]\n")
	sb.WriteString(fmt.Sprintf("ShowHiddenFiles = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.ShowHiddenFiles]))
	sb.WriteString(fmt.Sprintf("HighlightDir = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.HighlightDir]))
	sb.WriteString(fmt.Sprintf("SavePanelPaths = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.SavePanelPaths]))
	sb.WriteString(fmt.Sprintf("KeepTerminalCursor = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.KeepTerminalCursor]))
	sb.WriteString(fmt.Sprintf("CommandLineAutoComplete = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.CommandLineAutoComplete]))

	sb.WriteString("\n[Editor]\n")
	sb.WriteString(fmt.Sprintf("AutoComplete = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.EditorAutoComplete]))
	sb.WriteString(fmt.Sprintf("AutoCompleteMask = %s\n", AppConfig.EditorAutoCompleteMask))

	sb.WriteString(fmt.Sprintf("ExpandTabs = %d\n", AppConfig.EditorExpandTabs))
	sb.WriteString(fmt.Sprintf("AutoIndent = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.EditorAutoIndent]))
	sb.WriteString(fmt.Sprintf("CursorBeyondEOL = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.EditorCursorBeyondEOL]))
	sb.WriteString(fmt.Sprintf("UseEditorConfig = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.EditorUseEditorConfig]))
	sb.WriteString(fmt.Sprintf("Crosshair = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.EditorCrosshair]))
	sb.WriteString(fmt.Sprintf("TabSize = %d\n", AppConfig.EditorTabSize))
	sb.WriteString("\n[Plugins]\n")
	sb.WriteString(fmt.Sprintf("List = %s\n", strings.Join(AppConfig.RegisteredPlugins, "|")))

	err := os.WriteFile(path, []byte(sb.String()), 0644)
	if err != nil {
		vtui.DebugLog("CONFIG: Failed to save application settings: %v", err)
		return
	}

	vtui.DebugLog("CONFIG: Saved application settings to %s", path)
}