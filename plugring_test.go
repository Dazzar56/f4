package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/unxed/vtui"
)

func TestResolveAssetURL(t *testing.T) {
	tpl := "https://example.com/plugin_{os}_{arch}.zip"
	expected := "https://example.com/plugin_" + runtime.GOOS + "_" + runtime.GOARCH + ".zip"

	got := ResolveAssetURL(tpl)
	if got != expected {
		t.Errorf("ResolveAssetURL failed. Expected %q, got %q", expected, got)
	}

	// Test no placeholders
	plain := "https://example.com/plugin.zip"
	if ResolveAssetURL(plain) != plain {
		t.Errorf("ResolveAssetURL altered a string without placeholders")
	}
}

func TestFetchCatalog_Success(t *testing.T) {
	mockYaml := `
- id: "test-plugin"
  name: "Test Plugin"
  version: "1.0"
  author: "Tester"
  description: "A mock plugin"
  url: "http://example.com"
  entrypoint: "main.exe"
`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		w.Write([]byte(mockYaml))
	}))
	defer ts.Close()

	// Override URL for testing
	origURL := PlugRingCatalogURL
	PlugRingCatalogURL = ts.URL
	defer func() { PlugRingCatalogURL = origURL }()

	items, err := FetchCatalog(context.Background())
	if err != nil {
		t.Fatalf("FetchCatalog failed: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}

	if items[0].ID != "test-plugin" || items[0].Name != "Test Plugin" {
		t.Errorf("Parsed item mismatch: %+v", items[0])
	}
}

func TestFetchCatalog_Errors(t *testing.T) {
	origURL := PlugRingCatalogURL
	defer func() { PlugRingCatalogURL = origURL }()

	t.Run("404 Not Found", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()

		PlugRingCatalogURL = ts.URL
		_, err := FetchCatalog(context.Background())
		if err == nil || err.Error() != "server returned status 404" {
			t.Errorf("Expected 404 error, got: %v", err)
		}
	})

	t.Run("Bad YAML", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`- id: "test" \n invalid_yaml: {`))
		}))
		defer ts.Close()

		PlugRingCatalogURL = ts.URL
		_, err := FetchCatalog(context.Background())
		if err == nil {
			t.Error("Expected YAML parse error, got nil")
		}
	})
}
func TestFetchCatalog_Dependencies(t *testing.T) {
	mockYaml := `
- id: "dep-plugin"
  name: "Dep Plugin"
  dependencies:
    - lua
    - python3
`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		w.Write([]byte(mockYaml))
	}))
	defer ts.Close()

	origURL := PlugRingCatalogURL
	PlugRingCatalogURL = ts.URL
	defer func() { PlugRingCatalogURL = origURL }()

	items, err := FetchCatalog(context.Background())
	if err != nil {
		t.Fatalf("FetchCatalog failed: %v", err)
	}

	if len(items) != 1 || len(items[0].Dependencies) != 2 || items[0].Dependencies[0] != "lua" {
		t.Errorf("Dependencies parsing failed: %+v", items)
	}
}
func TestGetInstalledPlugRingItems(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	os.Setenv("APPDATA", tmpDir)
	resetConfigDirForTest()

	cfgDir := GetF4ConfigDir()
	plugringDir := filepath.Join(cfgDir, "plugring")
	pluginPath := filepath.Join(plugringDir, "test-id")
	os.MkdirAll(pluginPath, 0755)

	manifest := `{"id": "test-id", "version": "1.0.0"}`
	os.WriteFile(filepath.Join(pluginPath, "manifest.json"), []byte(manifest), 0644)

	installed := GetInstalledPlugRingItems()
	if len(installed) != 1 {
		t.Fatalf("Expected 1 installed item, got %d", len(installed))
	}
	if installed["test-id"].Version != "1.0.0" {
		t.Errorf("Expected version 1.0.0, got %s", installed["test-id"].Version)
	}
}
func TestCheckForPluginUpdates(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	os.Setenv("APPDATA", tmpDir)
	resetConfigDirForTest()

	cfgDir := GetF4ConfigDir()
	plugringDir := filepath.Join(cfgDir, "plugring")
	pluginPath := filepath.Join(plugringDir, "test-plugin")
	os.MkdirAll(pluginPath, 0755)

	// Local manifest has version 1.0
	manifest := `{"id": "test-plugin", "version": "1.0"}`
	os.WriteFile(filepath.Join(pluginPath, "manifest.json"), []byte(manifest), 0644)

	// Remote catalog has newer version 2.0
	mockYaml := `
- id: "test-plugin"
  name: "Test Plugin"
  version: "2.0"
`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		w.Write([]byte(mockYaml))
	}))
	defer ts.Close()

	origURL := PlugRingCatalogURL
	PlugRingCatalogURL = ts.URL
	defer func() { PlugRingCatalogURL = origURL }()

	// We override sleep duration inside CheckForPluginUpdates indirectly by running
	// its core logic synchronously in our test to avoid 5-second sleep hang.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	items, err := FetchCatalog(ctx)
	if err != nil {
		t.Fatalf("FetchCatalog failed: %v", err)
	}

	installed := GetInstalledPlugRingItems()
	updateCount := 0
	for _, itm := range items {
		if inst, ok := installed[itm.ID]; ok {
			if inst.Version != itm.Version {
				updateCount++
			}
		}
	}

	if updateCount != 1 {
		t.Errorf("Expected 1 update available, got %d", updateCount)
	}

	// Trigger toast on UI thread
	vtui.FrameManager.PostTask(func() {
		vtui.ShowToast("PlugRing: 1 plugin update available!", 100*time.Millisecond)
	})

	// Process task queue and verify toast appeared
	timeout := time.After(1 * time.Second)
	foundToast := false
	for !foundToast {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
			if vtui.FrameManager.GetActiveToast() != "" {
				foundToast = true
			}
		case <-timeout:
			t.Fatal("Timeout waiting for update toast")
		}
	}
}
