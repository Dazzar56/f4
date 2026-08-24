package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/unxed/vtui"
)

func TestF4HistoryProvider_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "history.json")

	hp := &F4HistoryProvider{
		path: dbPath,
		data: make(map[string][]string),
	}

	// 1. Save data
	items := []string{"cmd1", "cmd2"}
	hp.SaveHistory("test", items)

	// 2. Verify file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("History file was not created")
	}

	// 3. Create new provider and load
	hp2 := &F4HistoryProvider{
		path: dbPath,
		data: make(map[string][]string),
		rich: make(map[string][]HistoryRecord),
	}
	hp2.load()

	loaded := hp2.LoadHistory("test")
	if len(loaded) != 2 || loaded[0] != "cmd1" || loaded[1] != "cmd2" {
		t.Errorf("Persistence failed. Expected %v, got %v", items, loaded)
	}
}

func TestF4HistoryProvider_MigratesPlainBucketsAndKeepsRichMetadata(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.json")
	plain := map[string][]string{
		"cmdline": {"echo old", "git status"},
		"folders": {"/old", "/new"},
	}
	data, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	hp := &F4HistoryProvider{path: dbPath}
	hp.load()
	commands := hp.LoadRichHistory("cmdline")
	if len(commands) != 2 || commands[0].Name != "echo old" {
		t.Fatalf("migrated command history = %#v", commands)
	}

	stamp := time.Date(2026, time.August, 24, 12, 34, 56, 0, time.UTC)
	hp.SaveRichHistory("cmdline", []HistoryRecord{{Name: "git status", Dir: "/work", Timestamp: stamp, Lock: true}})
	if got := hp.LoadHistory("cmdline"); len(got) != 1 || got[0] != "git status" {
		t.Fatalf("plain view after rich save = %#v", got)
	}

	hp.SaveHistory("cmdline", []string{"git status"})
	commands = hp.LoadRichHistory("cmdline")
	if len(commands) != 1 || commands[0].Dir != "/work" || !commands[0].Lock || !commands[0].Timestamp.Equal(stamp) {
		t.Fatalf("plain save discarded rich metadata = %#v", commands)
	}
}

func TestAddFolderHistory(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "history_mru.json")

	hp := &F4HistoryProvider{
		path: dbPath,
		data: make(map[string][]string),
		rich: make(map[string][]HistoryRecord),
	}
	previous := vtui.GlobalHistoryProvider
	vtui.GlobalHistoryProvider = hp
	t.Cleanup(func() { vtui.GlobalHistoryProvider = previous })

	// 1. Начальное наполнение (должно идти в порядке MRU: последний добавленный сверху)
	AddFolderHistory("/path/a")
	AddFolderHistory("/path/b")

	h := hp.LoadHistory("folders")
	if len(h) != 2 || h[0] != "/path/b" || h[1] != "/path/a" {
		t.Errorf("Expected '/path/b' to be on top, got %v", h)
	}

	// 2. Дедупликация и перемещение вверх списка (MRU)
	AddFolderHistory("/path/a") // "/path/a" должна вернуться наверх
	h = hp.LoadHistory("folders")
	if len(h) != 2 || h[0] != "/path/a" || h[1] != "/path/b" {
		t.Errorf("Deduplication and MRU move failed, got: %v", h)
	}

	// 3. Проверка лимита в 100 элементов
	for i := 0; i < 110; i++ {
		AddFolderHistory(filepath.Join("/path", strconv.Itoa(i)))
	}
	h = hp.LoadHistory("folders")
	if len(h) > 100 {
		t.Errorf("Expected history size to be capped at 100, got %d", len(h))
	}
}

func TestLockedFolderAndCommandHistorySurviveLimits(t *testing.T) {
	hp := &F4HistoryProvider{
		path: filepath.Join(t.TempDir(), "history.json"),
		data: make(map[string][]string),
		rich: make(map[string][]HistoryRecord),
	}
	previous := vtui.GlobalHistoryProvider
	vtui.GlobalHistoryProvider = hp
	t.Cleanup(func() { vtui.GlobalHistoryProvider = previous })

	lockedPath := filepath.Join("/path", "locked")
	saveFolderHistoryRecords(hp, []HistoryRecord{{Name: lockedPath, Lock: true}})
	for i := 0; i < 110; i++ {
		AddFolderHistory(filepath.Join("/path", strconv.Itoa(i)))
	}
	folders, _ := loadFolderHistoryRecords(hp)
	found := false
	for _, record := range folders {
		if record.Name == lockedPath && record.Lock {
			found = true
		}
	}
	if !found {
		t.Fatal("locked folder was evicted by the history limit")
	}

	commands := []HistoryRecord{{Name: "newest"}, {Name: "pinned", Lock: true}, {Name: "old-1"}, {Name: "old-2"}}
	commands = limitRichHistory(commands, 2)
	if len(commands) != 2 || commands[0].Name != "newest" || commands[1].Name != "pinned" || !commands[1].Lock {
		t.Fatalf("limited command history = %#v", commands)
	}
}
