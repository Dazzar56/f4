package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/unxed/vtui"
	"golang.org/x/term"

	"github.com/unxed/f4/vfs"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-test-plugins" {
		vtui.DebugLog("--- TEST MODE ---")
		pm := NewPluginManager()
		pm.LoadAll()
		pm.CloseAll()
		return
	}

	ManageSessions()
}

func InitCore() *vtui.ScreenBuf {
	vtui.DebugLog("CORE: InitCore() called. PID: %d", os.Getpid())
	width, height, _ := term.GetSize(0)
	if width <= 0 { width = 80 }
	if height <= 0 { height = 24 }

	scr := vtui.NewScreenBuf()
	scr.AllocBuf(width, height)

	vtui.FrameManager.Init(scr)
	SetDefaultF4Palette()

	configDir, err := os.UserConfigDir()
	if err == nil {
		configPath := filepath.Join(configDir, "f4", "farcolors.ini")
		ini := LoadIni(configPath)
		InitColors(ini)
	}

	os.MkdirAll(filepath.Join(configDir, "f4"), 0755)
	MacroMgr = NewMacroManager(filepath.Join(configDir, "f4", "key_macros.ini"))
	vtui.FrameManager.EventFilter = MacroMgr.Filter
	vtui.FrameManager.Push(vtui.NewDesktop())

	panels := NewPanelsFrame()
	panels.ResizeConsole(width, height)
	vtui.FrameManager.Push(panels)

	vtui.FrameManager.MenuBar = panels.menuBar
	vtui.FrameManager.KeyBar = panels.keyBar

	if fsp, ok := panels.left.(*FileSystemPanel); ok {
		for i := 0; i < 50; i++ {
			fsp.entries = append(fsp.entries, &fileEntry{VFSItem: vfs.VFSItem{Name: fmt.Sprintf("test_file_%d.txt", i), Size: 1024}})
		}
		rows := make([]vtui.TableRow, len(fsp.entries))
		for i, e := range fsp.entries { rows[i] = e }
		fsp.table.SetRows(rows)
	}

	pluginManager := NewPluginManager()
	go pluginManager.LoadAll()

	vtui.DebugLog("CORE: Initialization complete")
	return scr
}