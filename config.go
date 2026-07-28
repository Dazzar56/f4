package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/unxed/vtui"
)

type F4Config struct {
	ColorStyle              string
	ShowHiddenFiles         bool
	HighlightDir            bool
	SavePanelPaths          bool
	KeepTerminalCursor      bool
	CommandLineAutoComplete bool
	VimHotkeys              bool
	SyncPanelLoad           bool
	EditorAutoComplete      bool
	EditorAutoCompleteMask  string
	EditorExpandTabs        int
	EditorAutoIndent        bool
	EditorCursorBeyondEOL   bool
	EditorTabSize           int
	EditorUseEditorConfig   bool
	EditorCrosshair         bool
	UseExternalEditor       bool
	ExternalEditorCommand   string
	RegisteredPlugins       []string
	ConfirmCopy             bool
	ConfirmMove             bool
	ConfirmDelete           bool
	ConfirmExit             bool
	DefaultFileOpMode       int
	FileOpPathDisplay       int
	GuiFont                 string
	GuiFontSize             int
	GuiCols                 int
	GuiRows                 int
	ConsoleTitleTemplate    string
	UpdateChannel           int    // 0 = Stable, 1 = Nightly
	UpdateInterval          int    // 0 = Never, 1 = Every start, 2 = Daily, 3 = Weekly
	LastUpdateCheck         int64  // Unix timestamp
	LastUpdateVersion       string // Version string or PublishedAt timestamp
}

var AppConfig = F4Config{
	ColorStyle:              "Modern",
	ShowHiddenFiles:         true,
	HighlightDir:            true,
	SavePanelPaths:          true,
	KeepTerminalCursor:      false,
	CommandLineAutoComplete: true,
	VimHotkeys:              false,
	SyncPanelLoad:           false,
	EditorAutoComplete:      true,
	EditorAutoCompleteMask:  "*.go;*.c;*.cpp;*.h;*.hpp;*.py;*.js;*.ts;*.rs;*.java;*.sh;*.txt;*.md;*.html;*.css;*.json",
	EditorExpandTabs:        0,
	EditorAutoIndent:        true,
	EditorCursorBeyondEOL:   false,
	EditorTabSize:           4,
	EditorUseEditorConfig:   true,
	EditorCrosshair:         false,
	UseExternalEditor:       false,
	ExternalEditorCommand:   "",
	ConfirmCopy:             true,
	ConfirmMove:             true,
	ConfirmDelete:           true,
	ConfirmExit:             true,
	DefaultFileOpMode:       0,
	FileOpPathDisplay:       0,
	GuiFont:                 "",
	GuiFontSize:             16,
	GuiCols:                 100,
	GuiRows:                 30,
	ConsoleTitleTemplate:    "f4 %Ver %Platform %Admin - %State",
	UpdateChannel:           0,
	UpdateInterval:          3, // Default to Weekly
	LastUpdateCheck:         0,
	LastUpdateVersion:       "",
}

var getUserConfigIniPath = func() string {
	configDir, _ := os.UserConfigDir()
	return filepath.Join(configDir, "f4", "settings.ini")
}

var getConfigIniPaths = func() []string {
	userPath := getUserConfigIniPath()
	if runtime.GOOS == "windows" {
		progData := os.Getenv("ProgramData")
		if progData != "" {
			return []string{filepath.Join(progData, "f4", "settings.ini"), userPath}
		}
		return []string{userPath}
	}
	// For unix-like systems
	return []string{"/etc/f4/settings.ini", userPath}
}

func LoadConfig() {
	paths := getConfigIniPaths()
	ini := &IniFile{data: make(map[string]map[string]string)}

	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			vtui.DebugLog("CONFIG: Loading and merging config from %s", path)
			partialIni := LoadIni(path)
			ini.Merge(partialIni)
		}
	}

	AppConfig.ShowHiddenFiles = ini.GetString("Panel", "ShowHiddenFiles", "1") == "1"
	AppConfig.ColorStyle = ini.GetString("Interface", "ColorStyle", "Modern")
	AppConfig.ConsoleTitleTemplate = ini.GetString("Interface", "ConsoleTitleTemplate", "f4 %Ver %Platform %Admin - %State")
	AppConfig.HighlightDir = ini.GetString("Panel", "HighlightDir", "1") == "1"
	AppConfig.SavePanelPaths = ini.GetString("Panel", "SavePanelPaths", "1") == "1"
	AppConfig.KeepTerminalCursor = ini.GetString("Panel", "KeepTerminalCursor", "0") == "1"
	AppConfig.CommandLineAutoComplete = ini.GetString("Panel", "CommandLineAutoComplete", "1") == "1"
	AppConfig.VimHotkeys = ini.GetString("Panel", "VimHotkeys", "0") == "1"
	AppConfig.SyncPanelLoad = ini.GetString("Panel", "SyncPanelLoad", "0") == "1"
	fmt.Sscanf(ini.GetString("Panel", "DefaultFileOpMode", "0"), "%d", &AppConfig.DefaultFileOpMode)
	AppConfig.ConfirmCopy = ini.GetString("System", "ConfirmCopy", "1") == "1"
	AppConfig.ConfirmMove = ini.GetString("System", "ConfirmMove", "1") == "1"
	AppConfig.ConfirmDelete = ini.GetString("System", "ConfirmDelete", "1") == "1"
	AppConfig.ConfirmExit = ini.GetString("System", "ConfirmExit", "1") == "1"
	fmt.Sscanf(ini.GetString("Panel", "FileOpPathDisplay", "0"), "%d", &AppConfig.FileOpPathDisplay)
	AppConfig.GuiFont = ini.GetString("Appearance", "GuiFont", "")
	fmt.Sscanf(ini.GetString("Appearance", "GuiFontSize", "16"), "%d", &AppConfig.GuiFontSize)
	if AppConfig.GuiFontSize <= 0 {
		AppConfig.GuiFontSize = 16
	}
	fmt.Sscanf(ini.GetString("Appearance", "GuiCols", "100"), "%d", &AppConfig.GuiCols)
	if AppConfig.GuiCols <= 0 {
		AppConfig.GuiCols = 100
	}
	fmt.Sscanf(ini.GetString("Appearance", "GuiRows", "30"), "%d", &AppConfig.GuiRows)
	if AppConfig.GuiRows <= 0 {
		AppConfig.GuiRows = 30
	}
	fmt.Sscanf(ini.GetString("Update", "Channel", "0"), "%d", &AppConfig.UpdateChannel)
	fmt.Sscanf(ini.GetString("Update", "Interval", "3"), "%d", &AppConfig.UpdateInterval)
	fmt.Sscanf(ini.GetString("Update", "LastCheck", "0"), "%d", &AppConfig.LastUpdateCheck)
	AppConfig.LastUpdateVersion = ini.GetString("Update", "LastVersion", "")

	AppConfig.EditorAutoComplete = ini.GetString("Editor", "AutoComplete", "1") == "1"
	AppConfig.EditorAutoCompleteMask = ini.GetString("Editor", "AutoCompleteMask", "*.go;*.c;*.cpp;*.h;*.hpp;*.py;*.js;*.ts;*.rs;*.java;*.sh;*.txt;*.md;*.html;*.css;*.json")

	AppConfig.EditorExpandTabs = 0
	fmt.Sscanf(ini.GetString("Editor", "ExpandTabs", "0"), "%d", &AppConfig.EditorExpandTabs)
	AppConfig.EditorAutoIndent = ini.GetString("Editor", "AutoIndent", "1") == "1"
	AppConfig.EditorCursorBeyondEOL = ini.GetString("Editor", "CursorBeyondEOL", "0") == "1"
	AppConfig.EditorUseEditorConfig = ini.GetString("Editor", "UseEditorConfig", "1") == "1"
	AppConfig.EditorCrosshair = ini.GetString("Editor", "Crosshair", "0") == "1"
	AppConfig.UseExternalEditor = ini.GetString("Editor", "UseExternalEditor", "0") == "1"
	AppConfig.ExternalEditorCommand = ini.GetString("Editor", "ExternalEditorCommand", "")
	plugStr := ini.GetString("Plugins", "List", "")
	if plugStr != "" {
		AppConfig.RegisteredPlugins = strings.Split(plugStr, "|")
	}
	AppConfig.EditorTabSize = 4
	fmt.Sscanf(ini.GetString("Editor", "TabSize", "4"), "%d", &AppConfig.EditorTabSize)

}

func SaveConfig() {
	path := getUserConfigIniPath()
	os.MkdirAll(filepath.Dir(path), 0755)

	var sb strings.Builder
	sb.WriteString("[Interface]\n")
	sb.WriteString(fmt.Sprintf("ColorStyle = %s\n", AppConfig.ColorStyle))
	sb.WriteString(fmt.Sprintf("ConsoleTitleTemplate = %s\n\n", AppConfig.ConsoleTitleTemplate))
	sb.WriteString("[Panel]\n")
	sb.WriteString(fmt.Sprintf("ShowHiddenFiles = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.ShowHiddenFiles]))
	sb.WriteString(fmt.Sprintf("HighlightDir = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.HighlightDir]))
	sb.WriteString(fmt.Sprintf("SavePanelPaths = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.SavePanelPaths]))
	sb.WriteString(fmt.Sprintf("KeepTerminalCursor = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.KeepTerminalCursor]))
	sb.WriteString(fmt.Sprintf("CommandLineAutoComplete = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.CommandLineAutoComplete]))
	sb.WriteString(fmt.Sprintf("VimHotkeys = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.VimHotkeys]))
	sb.WriteString(fmt.Sprintf("SyncPanelLoad = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.SyncPanelLoad]))
	sb.WriteString(fmt.Sprintf("DefaultFileOpMode = %d\n", AppConfig.DefaultFileOpMode))
	sb.WriteString(fmt.Sprintf("FileOpPathDisplay = %d\n", AppConfig.FileOpPathDisplay))

	sb.WriteString("\n[System]\n")
	sb.WriteString(fmt.Sprintf("ConfirmCopy = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.ConfirmCopy]))
	sb.WriteString(fmt.Sprintf("ConfirmMove = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.ConfirmMove]))
	sb.WriteString(fmt.Sprintf("ConfirmDelete = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.ConfirmDelete]))
	sb.WriteString(fmt.Sprintf("ConfirmExit = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.ConfirmExit]))

	sb.WriteString("\n[Appearance]\n")
	sb.WriteString(fmt.Sprintf("GuiFont = %s\n", AppConfig.GuiFont))
	sb.WriteString(fmt.Sprintf("GuiFontSize = %d\n", AppConfig.GuiFontSize))
	sb.WriteString(fmt.Sprintf("GuiCols = %d\n", AppConfig.GuiCols))
	sb.WriteString(fmt.Sprintf("GuiRows = %d\n", AppConfig.GuiRows))

	sb.WriteString("\n[Update]\n")
	sb.WriteString(fmt.Sprintf("Channel = %d\n", AppConfig.UpdateChannel))
	sb.WriteString(fmt.Sprintf("Interval = %d\n", AppConfig.UpdateInterval))
	sb.WriteString(fmt.Sprintf("LastCheck = %d\n", AppConfig.LastUpdateCheck))
	sb.WriteString(fmt.Sprintf("LastVersion = %s\n", AppConfig.LastUpdateVersion))
	sb.WriteString("\n[Editor]\n")
	sb.WriteString(fmt.Sprintf("AutoComplete = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.EditorAutoComplete]))
	sb.WriteString(fmt.Sprintf("AutoCompleteMask = %s\n", AppConfig.EditorAutoCompleteMask))

	sb.WriteString(fmt.Sprintf("ExpandTabs = %d\n", AppConfig.EditorExpandTabs))
	sb.WriteString(fmt.Sprintf("AutoIndent = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.EditorAutoIndent]))
	sb.WriteString(fmt.Sprintf("CursorBeyondEOL = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.EditorCursorBeyondEOL]))
	sb.WriteString(fmt.Sprintf("UseEditorConfig = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.EditorUseEditorConfig]))
	sb.WriteString(fmt.Sprintf("Crosshair = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.EditorCrosshair]))
	sb.WriteString(fmt.Sprintf("TabSize = %d\n", AppConfig.EditorTabSize))
	sb.WriteString(fmt.Sprintf("UseExternalEditor = %d\n", map[bool]int{true: 1, false: 0}[AppConfig.UseExternalEditor]))
	sb.WriteString(fmt.Sprintf("ExternalEditorCommand = %s\n", AppConfig.ExternalEditorCommand))
	sb.WriteString("\n[Plugins]\n")
	sb.WriteString(fmt.Sprintf("List = %s\n", strings.Join(AppConfig.RegisteredPlugins, "|")))

	err := os.WriteFile(path, []byte(sb.String()), 0644)
	if err != nil {
		vtui.DebugLog("CONFIG: Failed to save application settings: %v", err)
		return
	}

	vtui.DebugLog("CONFIG: Saved application settings to %s", path)
}
