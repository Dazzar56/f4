package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
	mockData := []PlugRingItem{
		{
			ID:          "test-plugin",
			Name:        "Test Plugin",
			Version:     "1.0",
			Author:      "Tester",
			Description: "A mock plugin",
			URL:         "http://example.com",
			Entrypoint:  "main.exe",
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockData)
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

	t.Run("Bad JSON", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{ "bad": json`))
		}))
		defer ts.Close()

		PlugRingCatalogURL = ts.URL
		_, err := FetchCatalog(context.Background())
		if err == nil {
			t.Error("Expected JSON parse error, got nil")
		}
	})
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
