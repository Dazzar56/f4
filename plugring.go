package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/unxed/vtui"
)

// PlugRingCatalogURL is the URL where f4 fetches the compiled JSON catalog.
// During development, it can point to a raw file. In production, it might point to gh-pages.
var PlugRingCatalogURL = "https://raw.githubusercontent.com/unxed/f4/main/plugring/index.json"

// PlugRingItem represents a single plugin available in the store.
type PlugRingItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Entrypoint  string `json:"entrypoint"`
	SetupCmd    string `json:"setup_cmd"`
}

// FetchCatalog downloads and parses the plugin catalog.
func FetchCatalog(ctx context.Context) ([]PlugRingItem, error) {
	// Developer convenience: load local index if available, but only if we are using the default URL
	if PlugRingCatalogURL == "https://raw.githubusercontent.com/unxed/f4/main/plugring/index.json" {
		if data, err := os.ReadFile(filepath.Join("plugring", "index.json")); err == nil {
			var items []PlugRingItem
			if json.Unmarshal(data, &items) == nil {
				vtui.DebugLog("PLUGRING: Loaded catalog from local file")
				return items, nil
			}
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", PlugRingCatalogURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Disable caching for the catalog fetch to ensure fresh results
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("User-Agent", "f4-plugring")

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error while fetching catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var items []PlugRingItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("failed to parse catalog JSON: %w", err)
	}

	return items, nil
}

// ResolveAssetURL replaces platform-specific placeholders in the download URL.
func ResolveAssetURL(urlTpl string) string {
	res := strings.ReplaceAll(urlTpl, "{os}", runtime.GOOS)
	res = strings.ReplaceAll(res, "{arch}", runtime.GOARCH)
	return res
}

// GetInstalledPlugRingItems scans the local plugins directory for manifests.
func GetInstalledPlugRingItems() map[string]PlugRingItem {
	dir := filepath.Join(GetF4ConfigDir(), "plugring")
	res := make(map[string]PlugRingItem)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return res
	}
	for _, e := range entries {
		if e.IsDir() {
			manifestPath := filepath.Join(dir, e.Name(), "manifest.json")
			data, err := os.ReadFile(manifestPath)
			if err == nil {
				var item PlugRingItem
				if json.Unmarshal(data, &item) == nil {
					res[item.ID] = item
				}
			}
		}
	}
	return res
}
