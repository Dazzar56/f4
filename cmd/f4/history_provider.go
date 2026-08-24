package main

import (
	"encoding/json"
	"github.com/unxed/vtui"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

import "time"

type HistoryRecord struct {
	Name string `json:"name"`
	Dir  string `json:"dir,omitempty"`
	// Extra is the pre-rich-history spelling of Dir. Keep reading and
	// writing it for imported/older records, but use Dir for new records.
	Extra     string    `json:"extra,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	Lock      bool      `json:"lock,omitempty"`
}

const (
	historyTypeCommands = iota
	historyTypeFolders
	historyTypeViewEdit
	historyTypeCount
)

const (
	historyShowDateTime = iota
	historyShowDate
	historyShowNone
)

func (r HistoryRecord) directory() string {
	if r.Dir != "" {
		return r.Dir
	}
	return r.Extra
}

func (r HistoryRecord) DisplayText() string {
	res := ""
	if !r.Timestamp.IsZero() {
		res += r.Timestamp.Format("15:04:05 ")
	}
	if extra := r.directory(); extra != "" {
		if len(extra) > 15 {
			extra = "..." + extra[len(extra)-12:]
		}
		if !strings.HasSuffix(extra, "/") && !strings.HasSuffix(extra, "\\") {
			res += extra + "/ "
		} else {
			res += extra + " "
		}
	}
	res += r.Name
	return res
}

type F4HistoryProvider struct {
	mu   sync.Mutex
	path string
	data map[string][]string
	rich map[string][]HistoryRecord
}

func NewF4HistoryProvider() *F4HistoryProvider {
	p := filepath.Join(GetF4ConfigDir(), "history.json")
	hp := &F4HistoryProvider{
		path: p,
		data: make(map[string][]string),
		rich: make(map[string][]HistoryRecord),
	}
	hp.load()
	return hp
}

func (hp *F4HistoryProvider) load() {
	hp.mu.Lock()
	defer hp.mu.Unlock()
	file, err := os.ReadFile(hp.path)
	if err == nil {
		var wrapper struct {
			Data map[string][]string        `json:"data,omitempty"`
			Rich map[string][]HistoryRecord `json:"rich,omitempty"`
		}
		if err := json.Unmarshal(file, &wrapper); err == nil && (wrapper.Data != nil || wrapper.Rich != nil) {
			if wrapper.Data != nil {
				hp.data = wrapper.Data
			}
			if wrapper.Rich != nil {
				hp.rich = wrapper.Rich
			}
		} else {
			var oldData map[string][]string
			if err := json.Unmarshal(file, &oldData); err == nil {
				hp.data = oldData
			}
		}
	}
	if hp.data == nil {
		hp.data = make(map[string][]string)
	}
	if hp.rich == nil {
		hp.rich = make(map[string][]HistoryRecord)
	}
	// A wrapper written before rich history was introduced still has the
	// command and folder buckets in Data. Promote those buckets lazily while
	// retaining Data as the string-compatible view used by vtui.Edit.
	for _, id := range []string{"cmdline", "folders"} {
		if _, ok := hp.rich[id]; ok {
			continue
		}
		if names, ok := hp.data[id]; ok {
			hp.rich[id] = recordsFromNames(names)
		}
	}
	for id, records := range hp.rich {
		if _, ok := hp.data[id]; !ok && (id == "cmdline" || id == "folders") {
			hp.data[id] = extractHistoryNames(records)
		}
	}
}

func (hp *F4HistoryProvider) save() {
	hp.mu.Lock()
	defer hp.mu.Unlock()
	os.MkdirAll(filepath.Dir(hp.path), 0755)
	wrapper := struct {
		Data map[string][]string        `json:"data,omitempty"`
		Rich map[string][]HistoryRecord `json:"rich,omitempty"`
	}{
		Data: hp.data,
		Rich: hp.rich,
	}
	if len(hp.data) == 0 {
		wrapper.Data = nil
	}
	if len(hp.rich) == 0 {
		wrapper.Rich = nil
	}
	file, err := json.MarshalIndent(wrapper, "", "  ")
	if err == nil {
		os.WriteFile(hp.path, file, 0644)
	}
}

func (hp *F4HistoryProvider) LoadHistory(id string) []string {
	hp.mu.Lock()
	defer hp.mu.Unlock()
	if items, ok := hp.data[id]; ok {
		// Return a copy to avoid concurrent slice modification issues
		res := make([]string, len(items))
		copy(res, items)
		return res
	}
	return nil
}

func (hp *F4HistoryProvider) SaveHistory(id string, history []string) {
	hp.mu.Lock()
	if hp.data == nil {
		hp.data = make(map[string][]string)
	}
	hp.data[id] = append([]string(nil), history...)
	if id == "cmdline" || id == "folders" {
		if hp.rich == nil {
			hp.rich = make(map[string][]HistoryRecord)
		}
		hp.rich[id] = mergeHistoryNames(hp.rich[id], history)
	}
	hp.mu.Unlock()
	hp.save()
}
func (hp *F4HistoryProvider) LoadRichHistory(id string) []HistoryRecord {
	hp.mu.Lock()
	defer hp.mu.Unlock()
	if items, ok := hp.rich[id]; ok {
		res := make([]HistoryRecord, len(items))
		copy(res, items)
		return res
	}
	return nil
}

func (hp *F4HistoryProvider) SaveRichHistory(id string, history []HistoryRecord) {
	hp.mu.Lock()
	if hp.rich == nil {
		hp.rich = make(map[string][]HistoryRecord)
	}
	hp.rich[id] = append([]HistoryRecord(nil), history...)
	if hp.data == nil {
		hp.data = make(map[string][]string)
	}
	if id == "cmdline" || id == "folders" {
		hp.data[id] = extractHistoryNames(history)
	}
	hp.mu.Unlock()
	hp.save()
}

func recordsFromNames(names []string) []HistoryRecord {
	if len(names) == 0 {
		return nil
	}
	records := make([]HistoryRecord, 0, len(names))
	for _, name := range names {
		records = append(records, HistoryRecord{Name: name})
	}
	return records
}

func extractHistoryNames(records []HistoryRecord) []string {
	if len(records) == 0 {
		return nil
	}
	names := make([]string, 0, len(records))
	for _, record := range records {
		names = append(names, record.Name)
	}
	return names
}

// mergeHistoryNames updates the string-compatible view without throwing away
// metadata that belongs to an entry which is still present. This is needed
// because vtui.Edit can save a plain []string history after rich history has
// already been loaded.
func mergeHistoryNames(old []HistoryRecord, names []string) []HistoryRecord {
	if len(names) == 0 {
		return nil
	}
	byName := make(map[string]HistoryRecord, len(old))
	for _, record := range old {
		if _, exists := byName[record.Name]; !exists {
			byName[record.Name] = record
		}
	}
	merged := make([]HistoryRecord, 0, len(names))
	for _, name := range names {
		record := byName[name]
		record.Name = name
		merged = append(merged, record)
	}
	return merged
}

func limitRichHistory(history []HistoryRecord, limit int) []HistoryRecord {
	if limit <= 0 || len(history) <= limit {
		return history
	}
	locked := 0
	for _, record := range history {
		if record.Lock {
			locked++
		}
	}
	unlockedBudget := limit - locked
	if unlockedBudget < 0 {
		unlockedBudget = 0
	}
	kept := make([]HistoryRecord, 0, limit)
	for _, record := range history {
		if record.Lock {
			kept = append(kept, record)
			continue
		}
		if unlockedBudget > 0 {
			kept = append(kept, record)
			unlockedBudget--
		}
	}
	return kept
}

func loadFolderHistoryRecords(provider vtui.HistoryProvider) ([]HistoryRecord, *F4HistoryProvider) {
	hp, _ := provider.(*F4HistoryProvider)
	plain := provider.LoadHistory("folders")
	if hp == nil {
		records := make([]HistoryRecord, 0, len(plain))
		for _, path := range plain {
			records = append(records, HistoryRecord{Name: path})
		}
		return records, nil
	}
	rich := hp.LoadRichHistory("folders")
	if len(rich) == 0 && len(plain) > 0 {
		rich = recordsFromNames(plain)
	}
	records := make([]HistoryRecord, 0, len(plain))
	for _, path := range plain {
		var record HistoryRecord
		for _, candidate := range rich {
			if sameFolderHistoryPath(candidate.Name, path) {
				record = candidate
				break
			}
		}
		if record.Name == "" {
			record.Name = path
		}
		record.Name = path
		records = append(records, record)
	}
	return records, hp
}

func saveFolderHistoryRecords(hp *F4HistoryProvider, records []HistoryRecord) {
	if hp == nil {
		return
	}
	hp.SaveRichHistory("folders", records)
	hp.SaveHistory("folders", extractNames(records))
}

func AddFolderHistory(path string) {
	if path == "" || path == "." || vtui.GlobalHistoryProvider == nil {
		return
	}
	if records, hp := loadFolderHistoryRecords(vtui.GlobalHistoryProvider); hp != nil {
		current := HistoryRecord{Name: path, Timestamp: time.Now()}
		newHistory := []HistoryRecord{current}
		for _, record := range records {
			if sameFolderHistoryPath(record.Name, path) {
				newHistory[0].Lock = record.Lock
				continue
			}
			newHistory = append(newHistory, record)
		}
		newHistory = limitRichHistory(newHistory, 100)
		saveFolderHistoryRecords(hp, newHistory)
		return
	}
	h := vtui.GlobalHistoryProvider.LoadHistory("folders")
	// Deduplicate and move to top
	newHist := []string{path}
	for _, item := range h {
		if !sameFolderHistoryPath(item, path) {
			newHist = append(newHist, item)
		}
	}
	// Limit to 100 items
	if len(newHist) > 100 {
		newHist = newHist[:100]
	}
	vtui.GlobalHistoryProvider.SaveHistory("folders", newHist)
}
