package main

import (
	"os"
	"os/user"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/unxed/vtui"
)

var (
	titleOnce     sync.Once
	cachedHost    string
	cachedUser    string
	cachedAdmin   string
	cachedVersion string
	cachedPlat    string
)

func initTitleCache() {
	h, _ := os.Hostname()
	cachedHost = h

	u, err := user.Current()
	if err == nil && u != nil {
		cachedUser = u.Username
		if idx := strings.LastIndex(cachedUser, "\\"); idx != -1 {
			cachedUser = cachedUser[idx+1:]
		}
	} else {
		cachedUser = "user"
	}

	cachedAdmin = getAdminString()
	cachedVersion = getShortVersionInfo()
	cachedPlat = runtime.GOARCH
}

func getShortVersionInfo() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		var vcsRev string
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				vcsRev = s.Value
				if len(vcsRev) > 7 {
					vcsRev = vcsRev[:7]
				}
			}
		}
		if vcsRev != "" {
			return vcsRev
		}
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
	}
	return "v0.1.1-alpha"
}

func getLongVersionInfo() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		var vcsRev, vcsDirty, vcsTime string
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				vcsRev = s.Value
				if len(vcsRev) > 7 {
					vcsRev = vcsRev[:7]
				}
			case "vcs.modified":
				if s.Value == "true" {
					vcsDirty = " (dirty)"
				}
			case "vcs.time":
				vcsTime = s.Value
				if len(vcsTime) >= 16 {
					vcsTime = strings.Replace(vcsTime[:16], "T", " ", 1)
				}
			}
		}
		var sb strings.Builder
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			sb.WriteString(info.Main.Version)
		} else {
			sb.WriteString("v0.1.1-alpha")
		}
		if vcsRev != "" {
			sb.WriteString("-" + vcsRev + vcsDirty)
		}
		if vcsTime != "" {
			sb.WriteString(" [" + vcsTime + "]")
		}
		sb.WriteString(" (go: " + info.GoVersion + ")")
		return sb.String()
	}
	return "v0.1.1-alpha"
}

func UpdateWindowTitle(scr *vtui.ScreenBuf) {
	titleOnce.Do(initTitleCache)

	if vtui.FrameManager == nil {
		return
	}

	state := "Panels"
	if len(vtui.FrameManager.Screens) > 0 {
		state = vtui.FrameManager.Screens[vtui.FrameManager.ActiveIdx].GetTitle()
	}

	template := AppConfig.ConsoleTitleTemplate
	if template == "" {
		template = "f4 - %State"
	}

	r := strings.NewReplacer(
		"%State", state,
		"%Ver", cachedVersion,
		"%Platform", cachedPlat,
		"%Backend", getBackendName(),
		"%Host", cachedHost,
		"%User", cachedUser,
		"%Admin", cachedAdmin,
	)

	title := r.Replace(template)
	title = strings.ReplaceAll(title, "  ", " ") // Убираем двойные пробелы, если %Admin пустой
	vtui.SetWindowTitle(title)

	// Macro recording indicator — drawn after MenuBar so it's always on top
	if MacroMgr != nil && MacroMgr.Recording {
		scr.Write(0, 0, vtui.StringToCharInfo(" R ", vtui.SetRGBBoth(0, 0xFFFFFF, 0xFF0000)))
	}
}

func getBackendName() string {
	if vtui.FrameManager == nil {
		return "Console"
	}
	return vtui.FrameManager.GetBackendName()
}
