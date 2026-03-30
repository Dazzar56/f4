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
	var serverPath, clientPath string

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--server":
			if i+1 < len(os.Args) {
				serverPath = os.Args[i+1]
				i++
			}
		case "--client":
			if i+1 < len(os.Args) {
				clientPath = os.Args[i+1]
				i++
			}
		case "-test-plugins":
			vtui.DebugLog("--- PLUGIN TEST MODE ---")
			pm := NewPluginManager()
			pm.LoadAll()
			pm.CloseAll()
			return
		}
	}

	if serverPath != "" {
		runServer(serverPath)
		return
	}
	if clientPath != "" {
		runClient(clientPath)
		return
	}

	// If we are here, no special mode was requested
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
		// Just populate entries and refresh.
		// SetViewMode or Refresh will handle creating the correct TableRow wrappers (Medium vs Detailed).
		for i := 0; i < 50; i++ {
			fsp.entries = append(fsp.entries, &fileEntry{VFSItem: vfs.VFSItem{Name: fmt.Sprintf("test_file_%d.txt", i), Size: 1024}})
		}
		fsp.Refresh()
	}

	noPlugins := false
	for _, arg := range os.Args {
		if arg == "--no-plugins" {
			noPlugins = true
			break
		}
	}

	if !noPlugins {
		pluginManager := NewPluginManager()
		go pluginManager.LoadAll()
	} else {
		vtui.DebugLog("CORE: Plugins disabled by --no-plugins flag")
	}

	vtui.DebugLog("CORE: Initialization complete")
	return scr
}