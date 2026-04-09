package main

import (
	"sync"
	"github.com/unxed/f4/vfs"
)

// FileOpTracker aggregates statistics from a running file operation.
// It is thread-safe and provides normalized progress data for the UI.
type FileOpTracker struct {
	mu sync.RWMutex

	total     vfs.OpStats // Constant results from pre-scan
	processed vfs.OpStats // Accumulated results of finished items

	currentFileName  string
	currentFileBytes int64
	currentFileSize  int64

	completedBytes   int64 // Sum of sizes of fully copied files
}

func NewFileOpTracker(total vfs.OpStats) *FileOpTracker {
	return &FileOpTracker{
		total: total,
	}
}

// StartFile marks the beginning of a new file transfer.
func (t *FileOpTracker) StartFile(name string, size int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentFileName = name
	t.currentFileSize = size
	t.currentFileBytes = 0
}

// UpdateBytes records progress within the current file.
func (t *FileOpTracker) UpdateBytes(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentFileBytes += int64(n)
	// Safety: don't let current file progress exceed 100%
	if t.currentFileSize > 0 && t.currentFileBytes > t.currentFileSize {
		t.currentFileBytes = t.currentFileSize
	}
}

// FileDone completes the current file and updates global counters.
func (t *FileOpTracker) FileDone() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.processed.Files++
	t.completedBytes += t.currentFileSize

	t.currentFileName = ""
	t.currentFileBytes = 0
	t.currentFileSize = 0
}

// DirDone records a successfully created/processed directory.
func (t *FileOpTracker) DirDone() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.processed.Dirs++
}

// GetProgress returns data for both progress bars.
func (t *FileOpTracker) GetProgress() (filePct, totalPct int, currentName string) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	currentName = t.currentFileName

	// 1. Current File Percentage
	if t.currentFileSize > 0 {
		filePct = int((t.currentFileBytes * 100) / t.currentFileSize)
	} else if t.currentFileName != "" {
		filePct = 100 // Zero-byte file is done immediately
	}

	// 2. Total Percentage
	if t.total.Bytes > 0 {
		currentTotalBytes := t.completedBytes + t.currentFileBytes
		totalPct = int((currentTotalBytes * 100) / t.total.Bytes)
	} else {
		// FALLBACK: If total volume is 0 (e.g. copying empty folders or 0-byte files),
		// calculate progress based on item count to avoid a "stuck" bar.
		totalItems := t.total.Files + t.total.Dirs
		processedItems := t.processed.Files + t.processed.Dirs
		if totalItems > 0 {
			totalPct = int((processedItems * 100) / totalItems)
		} else {
			totalPct = 100
		}
	}

	// Safety clamp
	if totalPct > 100 { totalPct = 100 }
	return
}

// GetStats returns the raw accumulated statistics.
func (t *FileOpTracker) GetStats() (processed vfs.OpStats, total vfs.OpStats) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.processed, t.total
}