package main

import "fmt"

// Lng is a simple map-based localization storage.
// In the future, this can load from JSON/TOML or embed.FS.
var Lng = map[string]string{
	"Panel.Column.Name": "Name",
	"Panel.Column.Size": "Size",
	"Panel.UpDir":       "UP-DIR",
	"Panels.Prompt":     "> ",
	"Edit.NewFileTitle":  " Create New File ",
	"Edit.NewFilePrompt": "File &name:",
	"MakeFolder.Title":  " Create Folder ",
	"MakeFolder.Prompt": "Create the &folder:",
	"Delete.Title":      " Delete ",
	"Delete.Confirm":    "Do you want to delete\n%s?",
	"Delete.Btn":        "&Delete",
	"Copy.Title":        " Copy ",
	"Copy.Prompt":       "Copy %d item(s) to:",
	"Copy.Btn":          "&Copy",
	"Move.Title":        " Move ",
	"Move.Prompt":       "Rename or move %d item(s) to:",
	"Move.Btn":          "&Rename",
	"Btn.OverwriteAll":  "Overwrite &All",
	"Btn.SkipAll":       "S&kip All",
	"Btn.Retry":         "&Retry",
	"Btn.Ignore":        "&Ignore",
	"Operation.Error":   "Operation failed:\n%s",
	"Viewer.Title":      " View ",
	"Viewer.ModeText":   "Text",
	"Viewer.ModeHex":    "Hex",
	"Viewer.SearchTitle": " Search ",

	// Macros
	"Macro.AssignTitle":  " Assign Macro ",
	"Macro.AssignPrompt": "Press the desired key combination",
	// Top Menu
	"Menu.Left":     "Left",
	"Menu.Files":    "Files",
	"Menu.Commands": "Commands",
	"Menu.Options":  "Options",
	"Menu.Right":    "Right",
	"Menu.Exit":     "Exit",

	// KeyBar Normal
	"KeyBar.F1":  "Help",
	"KeyBar.F2":  "Menu",
	"KeyBar.F3":  "View",
	"KeyBar.F4":  "Edit",
	"KeyBar.F5":  "Copy",
	"KeyBar.F6":  "RenMov",
	"KeyBar.F7":  "MkDir",
	"KeyBar.F8":  "Delete",
	"KeyBar.F9":  "ConfMenu",
	"KeyBar.F10": "Quit",
	"KeyBar.F11": "Plugin",
	"KeyBar.F12": "Screen",

	// KeyBar Alt
	"KeyBar.AltF1": "Left",
	"KeyBar.AltF2": "Right",
	"KeyBar.AltF3": "Hex",

	// KeyBar Editor
	"KeyBar.EditorF1":  "Help",
	"KeyBar.EditorF2":  "Save",
	"KeyBar.EditorF3":  "Wrap",
	"KeyBar.EditorF10": "Quit",
	"KeyBar.ViewerF1": "Help",
	"KeyBar.ViewerF2": "Wrap",
	"KeyBar.ViewerF3": "Exit",
	"KeyBar.ViewerF4": "Hex",
	"KeyBar.ViewerF7": "Search",
	"KeyBar.ViewerF10": "Quit",
}

// Msg retrieves a localized string by key.
func Msg(key string) string {
	if val, ok := Lng[key]; ok {
		return val
	}
	return fmt.Sprintf("{%s}", key) // Return key in braces if not found
}
