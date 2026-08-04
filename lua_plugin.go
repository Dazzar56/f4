package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/unxed/f4/ffibridge"
	"github.com/unxed/f4/luaplug"
	"github.com/unxed/f4/sdk/f4rpc"
	"github.com/unxed/f4/vfs"
	"github.com/vmihailenco/msgpack/v5"
)

// LuaPlugin runs a Lua plugin inside the f4 process.
//
// It is the same plugin as the out-of-process one, only without the process:
// the script still registers F4-RPC methods and still calls Host.* methods, so
// a plugin does not know or care which transport is carrying it. What it buys
// is distribution, since the user no longer needs a system Lua and a
// MessagePack rock for a plugin to run at all.
type LuaPlugin struct {
	path    string
	runtime *luaplug.Runtime
	bridge  *ffibridge.Bridge
	host    map[string]f4rpc.Handler
	// identity is who this plugin is to the permission model, taken from
	// the manifest when it came from the catalog.
	identity PluginIdentity
}

// SetPermissionIdentity passes on who the manifest says this plugin is, so
// that a grant is remembered under the id PlugRing installed it under, and
// the dialog can quote the author instead of guessing.
func (p *LuaPlugin) SetPermissionIdentity(identity PluginIdentity) {
	p.identity = identity
}

// permissionIdentity falls back to the path for a plugin registered by hand,
// which has no manifest and therefore no id.
func (p *LuaPlugin) permissionIdentity() PluginIdentity {
	if p.identity.Key == "" {
		return PermissionIdentityForPath(p.path)
	}
	return p.identity
}

// NewLuaPlugin prepares a plugin from a Lua script.
func NewLuaPlugin(path string) *LuaPlugin {
	return &LuaPlugin{path: path}
}

// IsLuaEntrypoint reports whether an entrypoint is a bare Lua script that the
// embedded interpreter can run.
func IsLuaEntrypoint(entrypoint string) bool {
	return isBareEntrypointWithExt(entrypoint, ".lua")
}

// isBareEntrypointWithExt reports whether an entrypoint is a single file with
// the given extension. An entrypoint with arguments, such as "lua plugin.lua"
// or ".venv/bin/python main.py", asks for a process and gets one.
func isBareEntrypointWithExt(entrypoint, ext string) bool {
	fields := strings.Fields(entrypoint)
	if len(fields) != 1 {
		return false
	}
	return strings.EqualFold(filepath.Ext(fields[0]), ext)
}

// resolvePluginPath turns an entrypoint into a path, relative to the plugin's
// own directory when it has one.
func resolvePluginPath(dir, entrypoint string) string {
	path := strings.TrimSpace(entrypoint)
	if dir != "" && !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return path
}

// newPluginForEntrypoint picks the transport an entrypoint asks for. dir is the
// plugin's own directory, empty for a plain registered path.
func newPluginForEntrypoint(dir, entrypoint string) Plugin {
	if IsLuaEntrypoint(entrypoint) {
		return NewLuaPlugin(resolvePluginPath(dir, entrypoint))
	}
	if IsWasmEntrypoint(entrypoint) {
		return NewWasmPlugin(resolvePluginPath(dir, entrypoint))
	}
	if dir == "" {
		return NewRPCPlugin(entrypoint)
	}
	return NewRPCPlugRing(dir, entrypoint)
}

// carriesPermissionIdentity is implemented by the transports that can be
// gated.
type carriesPermissionIdentity interface {
	SetPermissionIdentity(PluginIdentity)
}

// newPluginForPlugRingItem is newPluginForEntrypoint with the manifest in
// hand, which is the only place the declared permissions come from.
func newPluginForPlugRingItem(dir string, item PlugRingItem) Plugin {
	plugin := newPluginForEntrypoint(dir, item.Entrypoint)
	// Unconditionally: an identity is needed even when the manifest declares
	// no permissions, because the gate also asks about permissions a plugin
	// never declared.
	if aware, ok := plugin.(carriesPermissionIdentity); ok {
		aware.SetPermissionIdentity(PermissionIdentityForPlugRingItem(item))
	}
	return plugin
}

func (p *LuaPlugin) GetName() string {
	return p.path + " (Lua)"
}

func (p *LuaPlugin) Init(api vfs.HostAPI) error {
	p.bridge = newPluginFFIBridge(p.permissionIdentity())

	runtime, err := luaplug.New(luaplug.Options{
		Name: filepath.Base(p.path),
		Host: luaplug.HostFunc(p.callHost),
		FFI:  p.bridge,
	})
	if err != nil {
		p.bridge.Close()
		return err
	}
	p.runtime = runtime

	// The host methods must exist before the script body runs: a plugin is
	// free to log or ask for its version while it is still loading.
	p.host = newHostMethods(api, p, p.path, p.bridge)

	if err := runtime.LoadFile(p.path); err != nil {
		p.Close()
		return fmt.Errorf("loading %s: %w", p.path, err)
	}

	var res struct{ Drives []string }
	if err := p.Call("Plugin.Init", nil, &res); err != nil {
		p.Close()
		return fmt.Errorf("Plugin.Init failed: %w", err)
	}

	for _, drive := range res.Drives {
		driveName := drive
		api.RegisterDrive(driveName, func() vfs.VFS {
			return NewRPCVFS(p, driveName)
		})
	}
	return nil
}

// Call implements PluginTransport: a request from f4 into the plugin.
func (p *LuaPlugin) Call(method string, params any, result any) error {
	if p.runtime == nil {
		return fmt.Errorf("lua plugin %s is not running", p.path)
	}
	value, err := p.runtime.Dispatch(method, params)
	if err != nil {
		return err
	}
	return decodePluginValue(value, result)
}

// callHost is the other direction: the plugin calling f4.
func (p *LuaPlugin) callHost(method string, params any) (any, error) {
	handler, ok := p.host[method]
	if !ok {
		return nil, fmt.Errorf("unknown host method %q", method)
	}

	var raw msgpack.RawMessage
	if params != nil {
		encoded, err := msgpack.Marshal(params)
		if err != nil {
			return nil, err
		}
		raw = encoded
	}
	return handler(raw)
}

// decodePluginValue moves a value the interpreter produced into the typed
// struct the core expects. Routing it through MessagePack costs a round trip
// but guarantees that both transports agree on field naming, which is exactly
// where the older Far plugin APIs drifted apart.
func decodePluginValue(value any, result any) error {
	if value == nil || result == nil {
		return nil
	}
	encoded, err := msgpack.Marshal(value)
	if err != nil {
		return err
	}
	return msgpack.Unmarshal(encoded, result)
}

func (p *LuaPlugin) Close() error {
	if p.runtime != nil {
		_ = p.runtime.Close()
	}
	if p.bridge != nil {
		p.bridge.Close()
	}
	return nil
}
