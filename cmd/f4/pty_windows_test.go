//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

func TestConPTYAvailable_DoesNotPanic(t *testing.T) {
	avail := conPTYAvailable()
	if !avail {
		pty, err := NewPTY()
		if err == nil {
			if pty != nil {
				_ = pty.Close() // Cleanup is secondary to the unexpected allocation success.
			}
			t.Fatal("NewPTY succeeded when conPTYAvailable() reported false")
		}
	}
}

func TestActionExecuteBatchDoesNotReturnPanelsEarly(t *testing.T) {
	if !conPTYAvailable() {
		t.Skip("ConPTY unavailable")
	}

	oldSpawn := spawnLocalShellPTY
	oldConfig := AppConfig
	defer func() {
		spawnLocalShellPTY = oldSpawn
		AppConfig = oldConfig
	}()
	spawnLocalShellPTY = true
	AppConfig.ConsoleMode = "own"
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25)

	deadline := time.Now().Add(5 * time.Second)
	for pf.getActivePTY() == nil {
		if time.Now().After(deadline) {
			t.Fatal("local ConPTY did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	dir := t.TempDir()
	finished := filepath.Join(dir, "finished.marker")
	script := filepath.Join(dir, "f4-batch-probe.cmd")
	content := "@echo off\r\necho started>started.marker\r\nping.exe -n 4 127.0.0.1 >nul\r\necho finished>finished.marker\r\nping.exe -n 4 127.0.0.1 >nul\r\n"
	if err := os.WriteFile(script, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	actionExecute(pf, vfs.NewOSVFS(dir), dir, filepath.Base(script), script)
	start := time.Now()
	for pf.showPanels {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		default:
		}
		if time.Since(start) > 5*time.Second {
			t.Fatal("actionExecute did not hide panels")
		}
		time.Sleep(10 * time.Millisecond)
	}
	panelsReturned := time.Duration(0)
	for time.Since(start) < 10*time.Second {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		default:
		}
		if pf.showPanels {
			panelsReturned = time.Since(start)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if panelsReturned == 0 {
		t.Fatal("panels did not return after batch completion")
	}
	if _, err := os.Stat(finished); err != nil {
		t.Fatalf("panels returned after %v before batch finished: %v", panelsReturned, err)
	}
	t.Logf("panels returned after batch completion in %v", panelsReturned)
}
