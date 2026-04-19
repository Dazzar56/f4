package main

import (
	"testing"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

func TestAttributesDialog_Layout(t *testing.T) {
	vtui.SetDefaultPalette()
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)

	v := vfs.NewOSVFS(".")
	item := vfs.VFSItem{Name: "test.txt", Uid: 1000, Gid: 1000, UnixMode: 0644}

	// We test only Unix layout in this env, but it proves the engine works
	showAttributesUnix(nil, v, "test.txt", item)

	top := vtui.FrameManager.GetTopFrame()
	dlg, ok := top.(vtui.Container)
	if !ok {
		t.Fatal("Dialog not found on top")
	}

	vtui.AssertLayout(t, dlg)
}