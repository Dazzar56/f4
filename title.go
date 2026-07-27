package main

import (
	"os"
	"os/user"
	"runtime"
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
	cachedVersion = getFormattedVersionInfo()
	cachedPlat = runtime.GOARCH
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
