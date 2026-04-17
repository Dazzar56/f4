package main

import (
	"os"
	"path/filepath"
	"runtime/pprof"
	"fmt"
	"runtime"
	"strings"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
	"github.com/unxed/vtinput"
)

func main() {
	var sudoDispatcher string

	// Initialize SudoClient immediately for all process types
	execPath, err := os.Executable()
	if err != nil {
		execPath = os.Args[0]
	}
	absExecPath, _ := filepath.Abs(execPath)
	vfs.InitSudoClient(absExecPath, "")

	if os.Getenv("F4_ASKPASS_PARENT") != "" {
		vfs.RunSudoAskpass()
		return
	}

	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "--sudo-dispatcher" {
			if i+1 < len(os.Args) {
				sudoDispatcher = os.Args[i+1]
			}
			break
		} else if strings.HasPrefix(arg, "--sudo-dispatcher=") {
			sudoDispatcher = arg[len("--sudo-dispatcher="):]
			break
		}
	}
	if sudoDispatcher != "" {
		vfs.RunSudoDispatcher(sudoDispatcher)
		return
	}

	vtui.SetupStderrLog()
	vtui.DebugLog("MAIN: Starting with args: %v", os.Args)

	defer func() {
		if r := recover(); r != nil {
			vtui.DebugLog("FATAL PANIC IN MAIN: %v", r)
			crashPath := vtui.RecordCrash(r, nil)
			vtui.Suspend()
			vtui.CleanupStderrLog()
			// We print to os.Stdout here because os.Stderr is redirected to the log file!
			fmt.Fprintf(os.Stdout, "\n[f4] FATAL PANIC IN MAIN: %v\n", r)
			if crashPath != "" {
				fmt.Fprintf(os.Stdout, "[f4] Crash report saved to: %s\n", crashPath)
			}
			os.Exit(2)
		}
	}()
	// Defer disk logging to prevent launcher processes from polluting rotation queue.
	// Logging will be enabled in InitCore() for workers and standalone sessions.
	vtui.ConfigDiskLogging(false)
	var serverPath, clientPath string
	var cpuprofile string

	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]

		// Handle --flag=value format
		flagName := arg
		flagVal := ""
		if eqIdx := strings.IndexByte(arg, '='); eqIdx != -1 {
			flagName = arg[:eqIdx]
			flagVal = arg[eqIdx+1:]
		}

		switch flagName {
		case "--debug":
			os.Setenv("VTUI_DEBUG", "1")
		case "--log":
			if flagVal != "" {
				os.Setenv("VTUI_DEBUG", flagVal)
			} else if i+1 < len(os.Args) {
				os.Setenv("VTUI_DEBUG", os.Args[i+1])
				i++
			}
		case "--server":
			if flagVal != "" {
				serverPath = flagVal
			} else if i+1 < len(os.Args) {
				serverPath = os.Args[i+1]
				i++
			}
		case "--client":
			if flagVal != "" {
				clientPath = flagVal
			} else if i+1 < len(os.Args) {
				clientPath = os.Args[i+1]
				i++
			}
		case "--input":
			if flagVal != "" {
				vtinput.InputMode = flagVal
			} else if i+1 < len(os.Args) {
				vtinput.InputMode = os.Args[i+1]
				i++
			}
		case "--cpuprofile":
			if flagVal != "" {
				cpuprofile = flagVal
			} else if i+1 < len(os.Args) {
				cpuprofile = os.Args[i+1]
				i++
			}
		case "-test-plugins":
			vtui.ConfigDiskLogging(true)
			vtui.DebugLog("--- PLUGIN TEST MODE ---")
			pm := NewPluginManager()
			pm.LoadAll()
			pm.CloseAll()
			return
		case "--sudo-dispatcher":
			if flagVal != "" {
				sudoDispatcher = flagVal
			} else if i+1 < len(os.Args) {
				sudoDispatcher = os.Args[i+1]
				i++
			}
		}
	}

	for _, arg := range os.Args {
		if arg == "--askpass" {
			vfs.RunSudoAskpass()
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
	vtui.ConfigDiskLogging(true)
	vtui.DebugLog("=== F4 STARTUP [%s] PID:%d ===", vtui.GetVersionInfo(), os.Getpid())

	// Environment Diagnostics
	vtui.DebugLog("ENV: OS=%s ARCH=%s", runtime.GOOS, runtime.GOARCH)
	if wt := os.Getenv("WT_SESSION"); wt != "" {
		vtui.DebugLog("ENV: Running inside Windows Terminal (WT_SESSION set)")
	}
	if term := os.Getenv("TERM"); term != "" {
		vtui.DebugLog("ENV: TERM=%s", term)
	}
	width, height, err := vtui.GetTerminalSize()
	if err != nil {
		vtui.DebugLog("CORE: term.GetSize(0) failed: %v", err)
	}
	if width <= 0 { width = 80 }
	if height <= 0 { height = 24 }

	// На Windows (особенно в legacy консоли) иногда нужно явно включить поддержку ESC-последовательностей.
	// vtui.PrepareTerminal() сделает это позже, но буферу размер нужен сразу.

	scr := vtui.NewScreenBuf()
	scr.AllocBuf(width, height)

	vtui.FrameManager.Init(scr)
	SetDefaultF4Palette()
	InitLang()
	vtui.GlobalHistoryProvider = NewF4HistoryProvider()
	vtinput.Logger = vtui.DebugLog // Pipe vtinput logs to vtui's debug logger
	vtui.GlobalClipboardAccessManager = NewF4ClipboardAuth()
	RegisterDrive("Null VFS", func() vfs.VFS { return vfs.NewNullVFS(50 * 1024 * 1024) }) // 50 MB/s

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
	LoadConfig()
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

	var sb strings.Builder
	sb.WriteString("[EditorSearch]\n")
	sb.WriteString(fmt.Sprintf("Pattern = %s\n", LastEditorSearch))
	sb.WriteString(fmt.Sprintf("CaseSensitive = %d\n", map[bool]int{true: 1, false: 0}[LastEditorSearchCase]))
	sb.WriteString(fmt.Sprintf("Reverse = %d\n", map[bool]int{true: 1, false: 0}[LastEditorSearchReverse]))

	sb.WriteString("\n[FindFile]\n")
	sb.WriteString(fmt.Sprintf("Mask = %s\n", LastFindFileMask))
	sb.WriteString(fmt.Sprintf("Text = %s\n", LastFindFileText))

	err := os.WriteFile(path, []byte(sb.String()), 0644)
	if err != nil {
		vtui.DebugLog("SESSION: Failed to save state: %v", err)
		return
	}

	vtui.DebugLog("SESSION: Saved state to %s", path)
}
