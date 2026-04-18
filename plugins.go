package main

import (
	"os"
	"sync"
	"path/filepath"
	"strings"
	"runtime"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/f4/plugins/archive"
	"github.com/unxed/f4/plugins/chroma"
	"github.com/unxed/f4/plugins/dummy_internal"
	"github.com/unxed/f4/plugins/netfox"
	"github.com/unxed/vtui"
)

// Plugin represents a loaded module.
type Plugin interface {
	Init(api vfs.HostAPI) error
	Close() error
	GetName() string
}
type PluginMenuItem struct {
	Label   string
	Handler func(app vfs.App)
}

var PluginMenuItems []PluginMenuItem

func RegisterPluginMenuItem(label string, handler func(app vfs.App)) {
	PluginMenuItems = append(PluginMenuItems, PluginMenuItem{Label: label, Handler: handler})
}

type PluginManager struct {
	mu      sync.Mutex
	api     vfs.HostAPI
	plugins []Plugin
}

func NewPluginManager() *PluginManager {
	return &PluginManager{
		api: &coreAPI{},
	}
}

func (pm *PluginManager) LoadAll() {
	vtui.DebugLog("--- Loading Plugins ---")

	// 1. Load Internal Plugins
	pm.loadInternal()

	// 2. Load External Plugins from ./plugins dir
	pm.loadExternal(filepath.Join(".", "plugins"))
}

func (pm *PluginManager) loadInternal() {
	plugins := []Plugin{
		&chroma.Plugin{},
		&dummy_internal.InternalDummyPlugin{},
		&archive.ArchivePlugin{},
		&netfox.NetFoxPlugin{},
	}

	for _, p := range plugins {
		if err := p.Init(pm.api); err == nil {
			pm.mu.Lock()
			pm.plugins = append(pm.plugins, p)
			pm.mu.Unlock()
			vtui.DebugLog("Loaded internal plugin: %s", p.GetName())
		} else {
			vtui.DebugLog("Failed to init internal plugin %T: %v", p, err)
		}
	}
}

func (pm *PluginManager) loadExternal(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		vtui.DebugLog("Cannot read plugins dir: %v", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Plugins are usually kept in subdirectories
			pm.loadExternal(filepath.Join(dir, entry.Name()))
			continue
		}

		name := entry.Name()
		// Ignore source files and scripts
		if strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".sh") || strings.HasSuffix(name, ".md") {
			continue
		}

		isExec := false
		if runtime.GOOS == "windows" {
			if strings.HasSuffix(strings.ToLower(name), ".exe") {
				isExec = true
			}
		} else {
			if info, err := entry.Info(); err == nil {
				if info.Mode()&0111 != 0 {
					isExec = true
				}
			}
		}

		if isExec {
			path := filepath.Join(dir, name)
			p := NewRPCPlugin(path)
			if err := p.Init(pm.api); err == nil {
				pm.mu.Lock()
				pm.plugins = append(pm.plugins, p)
				pm.mu.Unlock()
				vtui.DebugLog("Loaded RPC plugin: %s", p.GetName())
			} else {
				vtui.DebugLog("Failed RPC plugin %s: %v", path, err)
			}
		}
	}
}

func (pm *PluginManager) CloseAll() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for _, p := range pm.plugins {
		p.Close()
	}
	pm.plugins = nil
}
