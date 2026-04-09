package main

import (
	"os"
	"path/filepath"
	"runtime/pprof"
	"fmt"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
	"github.com/unxed/vtinput"
	"golang.org/x/term"
)

func main() {
	// Defer disk logging to prevent launcher processes from polluting rotation queue.
	// Logging will be enabled in InitCore() for workers and standalone sessions.
	vtui.ConfigDiskLogging(false)
	var serverPath, clientPath string
	var cpuprofile string

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
		case "--cpuprofile":
			if i+1 < len(os.Args) {
				cpuprofile = os.Args[i+1]
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
	if cpuprofile != "" {
		f, err := os.Create(cpuprofile)
		if err != nil {
			panic(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	// If we are here, no special mode was requested
	ManageSessions()
}

func InitCore() *vtui.ScreenBuf {
	vtui.DebugLog("=== F4 STARTUP [%s] PID:%d ===", vtui.GetVersionInfo(), os.Getpid())
	vtui.ConfigDiskLogging(true)
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
	vtui.GlobalHistoryProvider = NewF4HistoryProvider()
	vtinput.Logger = vtui.DebugLog // Pipe vtinput logs to vtui's debug logger
	vtui.GlobalClipboardAccessManager = NewF4ClipboardAuth()
	RegisterDrive("&1. Local ( / )", func() vfs.VFS { return vfs.NewOSVFS("/") })
	RegisterDrive("&2. Home ( ~ )", func() vfs.VFS { home, _ := os.UserHomeDir(); return vfs.NewOSVFS(home) })
	RegisterDrive("&4. Null VFS (Test)", func() vfs.VFS { return vfs.NewNullVFS(50 * 1024 * 1024) }) // 50 MB/s

	configDir, err := os.UserConfigDir()
	if err == nil {
		configPath := filepath.Join(configDir, "f4", "farcolors.ini")
		ini := LoadIni(configPath)
		InitColors(ini)
	}

	os.MkdirAll(filepath.Join(configDir, "f4"), 0755)
	MacroMgr = NewMacroManager(filepath.Join(configDir, "f4", "key_macros.ini"))
	vtui.FrameManager.EventFilter = MacroMgr.Filter
	LoadSession()
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

var getSessionIniPath = func() string {
	configDir, _ := os.UserConfigDir()
	return filepath.Join(configDir, "f4", "session.ini")
}

func LoadSession() {
	path := getSessionIniPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}
	ini := LoadIni(path)

	LastEditorSearch = ini.GetString("EditorSearch", "Pattern", "")
	LastEditorSearchCase = ini.GetString("EditorSearch", "CaseSensitive", "0") == "1"
	LastEditorSearchReverse = ini.GetString("EditorSearch", "Reverse", "0") == "1"

	LastFindFileMask = ini.GetString("FindFile", "Mask", "*")
	LastFindFileText = ini.GetString("FindFile", "Text", "")
	vtui.DebugLog("SESSION: Loaded state from %s", path)
}

func SaveSession() {
	path := getSessionIniPath()
	os.MkdirAll(filepath.Dir(path), 0755)

	f, err := os.Create(path)
	if err != nil {
		vtui.DebugLog("SESSION: Failed to save state: %v", err)
		return
	}
	defer f.Close()

	fmt.Fprintln(f, "[EditorSearch]")
	fmt.Fprintf(f, "Pattern = %s\n", LastEditorSearch)
	fmt.Fprintf(f, "CaseSensitive = %d\n", map[bool]int{true: 1, false: 0}[LastEditorSearchCase])
	fmt.Fprintf(f, "Reverse = %d\n", map[bool]int{true: 1, false: 0}[LastEditorSearchReverse])

	fmt.Fprintln(f, "\n[FindFile]")
	fmt.Fprintf(f, "Mask = %s\n", LastFindFileMask)
	fmt.Fprintf(f, "Text = %s\n", LastFindFileText)

	vtui.DebugLog("SESSION: Saved state to %s", path)
}
