package main

import "fmt"

// Lng is a simple map-based localization storage.
// In the future, this can load from JSON/TOML or embed.FS.
var Lng = map[string]string{
	"Panel.Column.Name": "Name",
	"Panel.Column.Size": "Size",
	"Panel.UpDir":       "UP-DIR",
	"Panels.Prompt":     "> ",
	"Desktop.Welcome":   " f4 project - Ctrl+Q to exit ",
}

// Msg retrieves a localized string by key.
func Msg(key string) string {
	if val, ok := Lng[key]; ok {
		return val
	}
	return fmt.Sprintf("{%s}", key) // Return key in braces if not found
}
