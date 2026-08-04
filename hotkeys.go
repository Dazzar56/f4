package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HotkeyManager handles mapping of key combinations to application actions.
type HotkeyManager struct {
	Bindings map[string]map[string]string // Area -> Key -> ActionName
	Defaults map[string]map[string]string // Area -> Key -> ActionName
	iniPath  string
}

var GlobalHotkeysMgr *HotkeyManager

func NewHotkeyManager(iniPath string) *HotkeyManager {
	hm := &HotkeyManager{
		Bindings: make(map[string]map[string]string),
		Defaults: make(map[string]map[string]string),
		iniPath:  iniPath,
	}
	hm.initDefaults()
	hm.Load()
	return hm
}

// GetActiveBindings returns a map of Area -> Key -> ActionName containing all active bindings.
func (hm *HotkeyManager) GetActiveBindings() map[string]map[string]string {
	res := make(map[string]map[string]string)
	for area, binds := range hm.Defaults {
		res[area] = make(map[string]string)
		for k, v := range binds {
			res[area][k] = v
		}
	}
	for area, binds := range hm.Bindings {
		if res[area] == nil {
			res[area] = make(map[string]string)
		}
		for k, v := range binds {
			if v == "None" || v == "" {
				delete(res[area], k)
			} else {
				res[area][k] = v
			}
		}
	}
	return res
}

func (hm *HotkeyManager) initDefaults() {
	hm.Defaults = map[string]map[string]string{
		"Shell": {
			"F3":      "File.View",
			"F4":      "File.Edit",
			"F5":      "File.Copy",
			"F6":      "File.Move",
			"F7":      "File.MakeDir",
			"F8":      "File.Delete",
			"ShiftF4": "File.New",
			"ShiftF6": "File.Rename",
			"AltF7":   "File.Find",
			"CtrlO":   "Panel.Toggle",
			"CtrlU":   "Panel.Swap",
			"CtrlR":   "Panel.Rescan",
			"AltF12":  "Panel.FoldersHistory",
			"AltF8":   "Panel.CommandHistory",
		},
		"Editor": {
			"F2":         "Editor.Save",
			"F3":         "Editor.WordWrap",
			"F5":         "Editor.ShowWhitespaces",
			"F6":         "Editor.SwitchToViewer",
			"F7":         "Editor.Search",
			"ShiftF7":    "Editor.SearchNext",
			"CtrlF7":     "Editor.Replace",
			"F8":         "Editor.CodepageNext",
			"ShiftF8":    "Editor.CodepageMenu",
			"F10":        "Editor.Quit",
			"Esc":        "Editor.Quit",
			"F4":         "Editor.Quit",
			"CtrlA":      "Editor.SelectAll",
			"CtrlY":      "Editor.DeleteLine",
			"CtrlZ":      "Editor.Undo",
			"CtrlShiftZ": "Editor.Redo",
		},
		"Viewer": {
			"Esc":     "Viewer.Quit",
			"F10":     "Viewer.Quit",
			"F3":      "Viewer.Quit",
			"F2":      "Viewer.WrapMode",
			"F4":      "Viewer.HexMode",
			"F6":      "Viewer.SwitchToEditor",
			"F8":      "Viewer.CodepageNext",
			"ShiftF8": "Viewer.CodepageMenu",
			"F7":      "Viewer.Search",
		},
		"Terminal": {
			"CtrlO": "Panel.Toggle",
		},
		"Common": {},
	}
}

// Load reads bindings from the INI file, overlaying them onto the defaults.
func (hm *HotkeyManager) Load() {
	hm.Bindings = make(map[string]map[string]string)

	// Copy defaults
	for area, binds := range hm.Defaults {
		hm.Bindings[area] = make(map[string]string)
		for k, v := range binds {
			hm.Bindings[area][k] = v
		}
	}

	if hm.iniPath == "" {
		return
	}

	ini := LoadIni(hm.iniPath)
	for area, binds := range ini.data {
		if hm.Bindings[area] == nil {
			hm.Bindings[area] = make(map[string]string)
		}
		for key, action := range binds {
			if action == "" {
				delete(hm.Bindings[area], key)
			} else if strings.EqualFold(action, "none") {
				hm.Bindings[area][key] = "None"
			} else {
				hm.Bindings[area][key] = action
			}
		}
	}
}

// Save writes only overridden or new bindings to the INI file.
func (hm *HotkeyManager) Save() {
	if hm.iniPath == "" {
		return
	}

	var sb strings.Builder
	for area, binds := range hm.Bindings {
		diffs := make(map[string]string)

		// Find overrides and additions
		for key, action := range binds {
			if defAction, ok := hm.Defaults[area][key]; !ok || defAction != action {
				diffs[key] = action
			}
		}

		// Find removals
		if defArea, ok := hm.Defaults[area]; ok {
			for key := range defArea {
				if _, exists := binds[key]; !exists {
					diffs[key] = "None"
				}
			}
		}

		if len(diffs) > 0 {
			sb.WriteString(fmt.Sprintf("[%s]\n", area))
			for key, action := range diffs {
				sb.WriteString(fmt.Sprintf("%s=%s\n", key, action))
			}
			sb.WriteString("\n")
		}
	}

	os.MkdirAll(filepath.Dir(hm.iniPath), 0755)
	os.WriteFile(hm.iniPath, []byte(sb.String()), 0644)
}

// GetAction returns the action name mapped to the key in the given area.
func (hm *HotkeyManager) GetAction(area, key string) string {
	if binds, ok := hm.Bindings[area]; ok {
		if action, ok := binds[key]; ok {
			return action
		}
	}
	if area != "Common" {
		if binds, ok := hm.Bindings["Common"]; ok {
			if action, ok := binds[key]; ok {
				return action
			}
		}
	}
	return ""
}

// Bind assigns an action to a key in a specific area.
func (hm *HotkeyManager) Bind(area, key, action string) {
	if hm.Bindings[area] == nil {
		hm.Bindings[area] = make(map[string]string)
	}
	hm.Bindings[area][key] = action
}

// Unbind removes a hotkey binding.
func (hm *HotkeyManager) Unbind(area, key string) {
	if binds, ok := hm.Bindings[area]; ok {
		delete(binds, key)
	}
}
