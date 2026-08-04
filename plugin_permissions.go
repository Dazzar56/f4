package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/unxed/f4/ffibridge"
	"github.com/unxed/vtui"
)

// Permissions a plugin can be granted.
//
// The list is short on purpose: a permission f4 cannot actually enforce is
// theatre, and teaches people that the dialog means nothing.
const (
	// PermissionFFI lets a plugin call native libraries through the FFI
	// bridge. It is the one that matters: a plugin holding it can do
	// anything f4 itself can do.
	PermissionFFI = "ffi"
	// PermissionUnsafeStdlib opens Lua's os and io to a plugin. Named here,
	// not yet enforced.
	PermissionUnsafeStdlib = "unsafe-stdlib"
	// PermissionNative covers running a platform binary as a subprocess.
	// Named here, not yet enforced.
	PermissionNative = "native"
)

// permissionTitles are what the user is asked about, in their words rather
// than ours.
var permissionTitles = map[string]string{
	PermissionFFI:          "call native system libraries",
	PermissionUnsafeStdlib: "read and write files and run commands",
	PermissionNative:       "run a program of its own",
}

// Decisions, as stored.
const (
	PermissionAllow = "allow"
	PermissionDeny  = "deny"
)

// permissionPromptTimeout bounds the wait for an answer, so a plugin asking
// while no UI is running does not hang forever.
const permissionPromptTimeout = 2 * time.Minute

// PermissionRequest is one question put to the user.
type PermissionRequest struct {
	// Plugin is what the user knows the plugin as.
	Plugin string
	// Permission is one of the constants above.
	Permission string
	// Reason is the plugin author's own explanation, from the manifest, or
	// empty when the plugin never declared this permission.
	Reason string
	// Detail is what the plugin is trying to do right now.
	Detail string
}

// PermissionPrompt asks the user. It is an interface so that the gate can be
// tested without a terminal, which is the only way to test it at all.
type PermissionPrompt interface {
	Ask(req PermissionRequest) bool
}

// PermissionStore remembers answers between runs.
//
// It keeps its own file rather than living in the main configuration, so that
// deleting it is an obvious way to start over and so that a plugin's grants
// travel as one small readable object.
type PermissionStore struct {
	mu      sync.Mutex
	path    string
	granted map[string]map[string]string
}

// DefaultPermissionStorePath is where grants live.
func DefaultPermissionStorePath() string {
	return filepath.Join(GetF4ConfigDir(), "plugin_permissions.json")
}

// LoadPermissionStore reads the store, returning an empty one when there is
// nothing to read. A corrupt file is not an error worth stopping for: the
// worst it costs is being asked again.
func LoadPermissionStore(path string) *PermissionStore {
	store := &PermissionStore{
		path:    path,
		granted: make(map[string]map[string]string),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return store
	}
	var parsed map[string]map[string]string
	if err := json.Unmarshal(data, &parsed); err != nil {
		vtui.DebugLog("PERMISSIONS: %s is unreadable, starting over: %v", path, err)
		return store
	}
	store.granted = parsed
	return store
}

// Decision reports a remembered answer.
func (s *PermissionStore) Decision(plugin, permission string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	decision, ok := s.granted[plugin][permission]
	return decision, ok
}

// Remember records an answer and writes the store out.
func (s *PermissionStore) Remember(plugin, permission, decision string) error {
	s.mu.Lock()
	if s.granted[plugin] == nil {
		s.granted[plugin] = make(map[string]string)
	}
	s.granted[plugin][permission] = decision
	data, err := json.MarshalIndent(s.granted, "", "  ")
	path := s.path
	s.mu.Unlock()

	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// Forget drops every grant a plugin holds, which is what removing it should
// do: reinstalling must not silently inherit the old answers.
func (s *PermissionStore) Forget(plugin string) error {
	s.mu.Lock()
	delete(s.granted, plugin)
	data, err := json.MarshalIndent(s.granted, "", "  ")
	path := s.path
	s.mu.Unlock()

	if err != nil || path == "" {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// PermissionGate decides whether one plugin may do one thing.
type PermissionGate struct {
	plugin  string
	reasons map[string]string
	store   *PermissionStore
	prompt  PermissionPrompt

	mu      sync.Mutex
	refused map[string]bool
}

// NewPermissionGate builds a gate for a plugin. reasons is what the plugin
// declared in its manifest, keyed by permission.
func NewPermissionGate(plugin string, reasons map[string]string, store *PermissionStore, prompt PermissionPrompt) *PermissionGate {
	return &PermissionGate{
		plugin:  plugin,
		reasons: reasons,
		store:   store,
		prompt:  prompt,
		refused: make(map[string]bool),
	}
}

// Allow answers a request, asking the user the first time.
//
// A yes is remembered for good; a no only for this run. The asymmetry is
// deliberate: a stray "deny" that stuck forever would leave a dead plugin and
// no obvious way to revive it, whereas a refusal that lasts until the next
// start needs no undo at all.
func (g *PermissionGate) Allow(permission, detail string) error {
	if g == nil {
		return nil
	}

	g.mu.Lock()
	refused := g.refused[permission]
	g.mu.Unlock()
	if refused {
		return fmt.Errorf("%s is not allowed to %s", g.plugin, permissionTitle(permission))
	}

	if g.store != nil {
		if decision, ok := g.store.Decision(g.plugin, permission); ok {
			if decision == PermissionAllow {
				return nil
			}
			return fmt.Errorf("%s is not allowed to %s", g.plugin, permissionTitle(permission))
		}
	}

	if g.prompt == nil {
		return fmt.Errorf("%s wants to %s and there is nobody to ask", g.plugin, permissionTitle(permission))
	}

	granted := g.prompt.Ask(PermissionRequest{
		Plugin:     g.plugin,
		Permission: permission,
		Reason:     g.reasons[permission],
		Detail:     detail,
	})

	if granted {
		if g.store != nil {
			if err := g.store.Remember(g.plugin, permission, PermissionAllow); err != nil {
				vtui.DebugLog("PERMISSIONS: cannot record the grant: %v", err)
			}
		}
		return nil
	}

	g.mu.Lock()
	g.refused[permission] = true
	g.mu.Unlock()
	return fmt.Errorf("%s is not allowed to %s", g.plugin, permissionTitle(permission))
}

// FFIHook is the gate in the shape ffibridge wants. Every operation of the
// bridge is the same permission: there is no useful sense in which loading a
// library is safer than calling into one.
func (g *PermissionGate) FFIHook() func(ffibridge.Op, string) error {
	if g == nil {
		return nil
	}
	return func(op ffibridge.Op, detail string) error {
		return g.Allow(PermissionFFI, fmt.Sprintf("%s %s", op, detail))
	}
}

func permissionTitle(permission string) string {
	if title, ok := permissionTitles[permission]; ok {
		return title
	}
	return permission
}

// PermissionRequestText is what the dialog says. It is a function so that the
// wording is testable and lives in one place.
func PermissionRequestText(req PermissionRequest) string {
	text := fmt.Sprintf("%s wants to %s.\n\n", req.Plugin, permissionTitle(req.Permission))
	if req.Reason != "" {
		text += "The plugin says:\n" + req.Reason + "\n\n"
	} else {
		text += "The plugin did not say why.\n\n"
	}
	if req.Detail != "" {
		text += "Right now: " + req.Detail + "\n\n"
	}
	text += "Allowing this lets it do anything f4 can do."
	return text
}

// uiPermissionPrompt asks through f4's own dialogs.
type uiPermissionPrompt struct{}

func (uiPermissionPrompt) Ask(req PermissionRequest) bool {
	if vtui.FrameManager == nil {
		// Nothing is drawing yet, which happens when a plugin reaches for
		// native code while still loading. Refusing is the safe answer, and
		// the log says why the plugin then failed.
		vtui.DebugLog("PERMISSIONS: %s asked to %s before the UI existed, refused",
			req.Plugin, permissionTitle(req.Permission))
		return false
	}

	answer := make(chan bool, 1)
	vtui.FrameManager.PostTask(func() {
		dlg := vtui.ShowMessage(" Plugin permission ", PermissionRequestText(req), []string{"&Allow", "&Deny"})
		if dlg == nil {
			answer <- false
			return
		}
		dlg.OnResult = func(code int) { answer <- code == 0 }
	})

	select {
	case granted := <-answer:
		return granted
	case <-time.After(permissionPromptTimeout):
		return false
	}
}

// pluginPermissionStore is the one store the running f4 shares.
var (
	pluginPermissionStoreOnce sync.Once
	pluginPermissionStore     *PermissionStore
)

// PluginPermissions returns the process-wide store.
func PluginPermissions() *PermissionStore {
	pluginPermissionStoreOnce.Do(func() {
		pluginPermissionStore = LoadPermissionStore(DefaultPermissionStorePath())
	})
	return pluginPermissionStore
}

// newPluginFFIBridge builds a plugin's FFI bridge with its permission gate
// already attached, so that no transport can accidentally hand out an
// ungated one.
func newPluginFFIBridge(plugin string, reasons map[string]string) *ffibridge.Bridge {
	gate := NewPermissionGate(plugin, reasons, PluginPermissions(), uiPermissionPrompt{})
	return ffibridge.New(ffibridge.Options{Allow: gate.FFIHook()})
}
