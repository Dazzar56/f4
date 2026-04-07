package main

import (
	"os"
	"path/filepath"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
	"github.com/unxed/vtinput"
	"golang.org/x/term"
)

func main() {
	var serverPath, clientPath string

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--debug":
			os.Setenv("VTUI_DEBUG", "1")
		case "--log":
			if i+1 < len(os.Args) {
				os.Setenv("VTUI_DEBUG", os.Args[i+1])
				i++
			}
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
	vtui.DebugLog("=== F4 STARTUP [%s] PID:%d ===", vtui.GetVersionInfo(), os.Getpid())
	width, height, err := term.GetSize(0)
	if err != nil {
		vtui.DebugLog("CORE: term.GetSize(0) failed: %v", err)
	}
	if width <= 0 { width = 80 }
	if height <= 0 { height = 24 }

	scr := vtui.NewScreenBuf()
	scr.AllocBuf(width, height)

	vtui.FrameManager.Init(scr)
	SetDefaultF4Palette()
	InitLang()
	vtinput.Logger = vtui.DebugLog // Pipe vtinput logs to vtui's debug logger
	vtui.GlobalClipboardAccessManager = NewF4ClipboardAuth()
	RegisterDrive("&1. Local ( / )", func() vfs.VFS { return vfs.NewOSVFS("/") })
	RegisterDrive("&2. Home ( ~ )", func() vfs.VFS { home, _ := os.UserHomeDir(); return vfs.NewOSVFS(home) })

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