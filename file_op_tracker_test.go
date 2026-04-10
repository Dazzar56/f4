package main

import (
	"testing"
	"github.com/unxed/f4/vfs"
)

func TestFileOpTracker_SingleFile(t *testing.T) {
	// Total: 1 file, 1000 bytes
	total := vfs.OpStats{Files: 1, Bytes: 1000}
	tracker := NewFileOpTracker(total)

	tracker.StartFile("test.bin", 1000)
	tracker.UpdateBytes(500)

	filePct, totalPct, name := tracker.GetProgress()

	if name != "test.bin" { t.Errorf("Expected name test.bin, got %q", name) }
	if filePct != 50 { t.Errorf("Expected 50%% file progress, got %d", filePct) }
	if totalPct != 50 { t.Errorf("Expected 50%% total progress, got %d", totalPct) }

	tracker.UpdateBytes(500)
	tracker.FileDone()

	filePct, totalPct, _ = tracker.GetProgress()
	if filePct != 0 { t.Errorf("Expected 0%% file progress after FileDone, got %d", filePct) }
	if totalPct != 100 { t.Errorf("Expected 100%% total progress, got %d", totalPct) }
}

func TestFileOpTracker_MultiFile(t *testing.T) {
	// Total: 2 files, 200 bytes total (100 each)
	total := vfs.OpStats{Files: 2, Bytes: 200}
	tracker := NewFileOpTracker(total)

	// Finish first file
	tracker.StartFile("f1.txt", 100)
	tracker.UpdateBytes(100)
	tracker.FileDone()

	// Half of second file
	tracker.StartFile("f2.txt", 100)
	tracker.UpdateBytes(50)

	filePct, totalPct, _ := tracker.GetProgress()

	// File 2 is at 50%
	if filePct != 50 { t.Errorf("FilePct error: %d", filePct) }
	// Total is (100 + 50) / 200 = 75%
	if totalPct != 75 { t.Errorf("TotalPct error: expected 75, got %d", totalPct) }
}

func TestFileOpTracker_ZeroBytesFallback(t *testing.T) {
	// Scenario: 10 empty folders, 0 bytes total volume
	total := vfs.OpStats{Dirs: 10, Bytes: 0}
	tracker := NewFileOpTracker(total)

	for i := 0; i < 5; i++ {
		tracker.DirDone()
	}

	_, totalPct, _ := tracker.GetProgress()

	// Should use item count (5/10 = 50%)
	if totalPct != 50 {
		t.Errorf("Expected 50%% progress based on item count, got %d", totalPct)
	}
}

func TestFileOpTracker_OverReporting(t *testing.T) {
	total := vfs.OpStats{Files: 1, Bytes: 100}
	tracker := NewFileOpTracker(total)

	tracker.StartFile("growth.log", 100)
	tracker.UpdateBytes(150) // More than announced

	filePct, totalPct, _ := tracker.GetProgress()
	if filePct != 100 || totalPct != 100 {
		t.Errorf("Over-reporting not clamped: file=%d, total=%d", filePct, totalPct)
	}
}

func TestFileOpTracker_EmptyJob(t *testing.T) {
	tracker := NewFileOpTracker(vfs.OpStats{})
	_, totalPct, _ := tracker.GetProgress()
	if totalPct != 100 {
		t.Errorf("Empty job should report 100%%, got %d", totalPct)
	}
}

func TestFileOpTracker_SkippedFiles(t *testing.T) {
	// Total: 10 files, 1000 bytes
	total := vfs.OpStats{Files: 10, Bytes: 1000}
	tracker := NewFileOpTracker(total)

	// Process 2 files
	tracker.StartFile("f1", 100); tracker.UpdateBytes(100); tracker.FileDone()
	tracker.StartFile("f2", 100); tracker.UpdateBytes(100); tracker.FileDone()

	// Skip 1 file (announced size was 100)
	tracker.StartFile("f3", 100)
	tracker.FileSkipped()

	processed, _ := tracker.GetStats()
	if processed.Files != 3 {
		t.Errorf("Expected 3 files processed (including skipped), got %d", processed.Files)
	}

	_, totalPct, _ := tracker.GetProgress()
	// (100 + 100 + 100) / 1000 = 30%
	if totalPct != 30 {
		t.Errorf("Progress mismatch after skip: expected 30%%, got %d%%", totalPct)
	}
}
func TestFileOpTracker_ProcessedBytes(t *testing.T) {
	total := vfs.OpStats{Files: 1, Bytes: 1000}
	tracker := NewFileOpTracker(total)

	tracker.StartFile("f1", 1000)
	tracker.UpdateBytes(450)

	processed, _ := tracker.GetStats()
	if processed.Bytes != 450 {
		t.Errorf("GetStats should include in-progress bytes: expected 450, got %d", processed.Bytes)
	}

	tracker.UpdateBytes(50)
	tracker.FileDone()

	processed, _ = tracker.GetStats()
	if processed.Bytes != 1000 {
		t.Errorf("GetStats failed after FileDone: expected 1000, got %d", processed.Bytes)
	}
}
