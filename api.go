package main

import "github.com/unxed/vtui"

// HostAPI defines the functions f4 exposes to plugins.
import "github.com/unxed/f4/vfs"

type HostAPI interface {
	GetVersion() string
	Log(msg string)
	Message(msg string)
	RegisterVFSProvider(p vfs.VFSProvider)
}

// coreAPI implements HostAPI.
type coreAPI struct{}

func (c *coreAPI) GetVersion() string {
	return "f4 v0.1.0-alpha"
}

func (c *coreAPI) Log(msg string) {
	vtui.DebugLog("PLUGIN.LOG: %s", msg)
}

func (c *coreAPI) Message(msg string) {
	vtui.DebugLog("PLUGIN MESSAGE BOX: %s", msg)
	// Safely push to the main UI thread to avoid race conditions from background plugin loads
	vtui.FrameManager.PostTask(func() {
		vtui.ShowMessage(" Plugin Message ", msg, []string{"&Ok"})
	})
}
func (c *coreAPI) RegisterVFSProvider(p vfs.VFSProvider) {
	vtui.DebugLog("CORE: Registering VFS Provider: %s", p.Name())
	vfs.RegisterProvider(p)
}
