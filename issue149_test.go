package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/unxed/f4/plugins/archive"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/zip"
)

// TestIssue149_LocatingETA_Progression reproduces the incorrect ETA calculation
// and lack of progress indication when skipping files in large or solid archives.
func TestIssue149_LocatingETA_Progression(t *testing.T) {
	// Simulate task: 1000 files, total volume 1GB.
	// We will skip most files (Locating) and extract only the last one.
	totalStats := vfs.OpStats{
		Files: 1000,
		Bytes: 1024 * 1024 * 1024,
	}

	tracker := NewFileOpTracker(totalStats)
	startTime := time.Now().Add(-10 * time.Second) // 10 seconds elapsed

	// Logic for ETA calculation from file_ops.go
	getETA := func(action string) string {
		processed, total := tracker.GetStats()
		elapsed := time.Since(startTime)

		const ItemOverhead = 32 * 1024
		vProcessed := float64(processed.Bytes + (processed.Files+processed.Dirs)*ItemOverhead)
		vTotal := float64(total.Bytes + (total.Files+total.Dirs)*ItemOverhead)

		if action == "Locating" || action == "Waiting" || action == "Scanning" || action == "Archiving" {
			return "Remaining: ??:??:??"
		}

		if vTotal > 0 && vProcessed > 0 && elapsed.Seconds() > 0.5 {
			ratio := vProcessed / vTotal
			etaSecs := (elapsed.Seconds() / ratio) - elapsed.Seconds()
			if etaSecs < 0 {
				etaSecs = 0
			}
			if etaSecs > 359999 {
				return "Remaining: >99 hours"
			}
			etaDur := time.Duration(etaSecs * float64(time.Second))
			return fmt.Sprintf("Remaining: %02d:%02d:%02d", int(etaDur.Hours()), int(etaDur.Minutes())%60, int(etaDur.Seconds())%60)
		}
		return "Remaining: ??:??:??"
	}

	// 1. MASKING CHECK: ETA must be hidden during the 'Locating' phase.
	etaDuringLocating := getETA("Locating")
	if etaDuringLocating != "Remaining: ??:??:??" {
		t.Errorf("ETA must be masked during 'Locating', got %q", etaDuringLocating)
	}

	// 2. REPRODUCTION OF "CRAZY ETA":
	// In the buggy implementation, when a file is skipped during a bulk operation
	// (e.g. in solid 7z), tracker.FileSkipped() is not called.
	// Consequently, processed.Files count remains 0, and vProcessed stays near zero.

	processedBefore, _ := tracker.GetStats()
	if processedBefore.Files != 0 {
		t.Fatal("Tracker should start with 0 processed files")
	}

	// Start extracting the 501st file after 10 seconds of "searching".
	tracker.StartFile("file_501.bin", 100*1024*1024)
	tracker.UpdateBytes(1024) // Read first kilobyte

	etaStartExtract := getETA("Copying")

	// Since skipped files weren't counted, we "processed" only 1KB in 10s.
	// Effective speed is extremely low, leading to a massive ETA.
	if !strings.Contains(etaStartExtract, ">99 hours") && !strings.Contains(etaStartExtract, "99:59:59") {
		t.Logf("Buggy ETA value: %q", etaStartExtract)
	} else {
		t.Log("Reproduced: ETA is capped or extremely large because skipped items aren't counted.")
	}

	// 3. VERIFY FIX LOGIC:
	// If we correctly call FileSkipped for each skipped item:
	tracker = NewFileOpTracker(totalStats)
	for i := 0; i < 500; i++ {
		tracker.StartFile(fmt.Sprintf("skipped_%d", i), 0)
		tracker.FileSkipped()
	}

	tracker.StartFile("file_501.bin", 100*1024*1024)
	tracker.UpdateBytes(1024)

	etaWithFix := getETA("Copying")
	t.Logf("Realistic ETA with fix: %q", etaWithFix)

	if strings.Contains(etaWithFix, "??") || strings.Contains(etaWithFix, ">99") {
		t.Errorf("ETA is still unrealistic even with proper skipping: %q", etaWithFix)
	}
}

// TestIssue149_Reproduction verifies that FileSkipped is correctly called
// during bulk extraction, ensuring that progress and ETA remain accurate
// even when many files are skipped in solid archives.
func TestIssue149_Reproduction(t *testing.T) {
	vfs.RegisterProvider(&archive.ArchiveProvider{})

	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "test_skip.zip")

	f, _ := os.Create(zipPath)
	zw := zip.NewWriter(f)
	for i := 0; i < 10; i++ {
		w, _ := zw.Create(fmt.Sprintf("file_%d.txt", i))
		w.Write([]byte("data"))
	}
	zw.Close()
	f.Close()

	parentVFS := vfs.NewOSVFS(tmpDir)
	arcVfs, _ := archive.NewArchiveVFS(parentVFS, zipPath)
	defer arcVfs.Close()

	dstVFS := vfs.NewOSVFS(tmpDir)

	// Extract only the LAST file. The first 9 must be skipped.
	names := []string{"file_9.txt"}

	// Tracker with pre-scanned stats for the selected file only.
	totalStats := vfs.OpStats{Files: 1, Bytes: 4}
	tracker := NewFileOpTracker(totalStats)

	rep := &globalAwareReporter{
		original:  &DummyReporter{},
		tracker:   tracker,
		getGlobal: func(action string) (string, int, string) { return "", 0, "" },
	}

	ctx := context.WithValue(context.Background(), "AutoQueue", true)

	err := arcVfs.CopyBulk(ctx, names, dstVFS, tmpDir, rep)
	if err != nil {
		t.Fatalf("CopyBulk failed: %v", err)
	}

	// Verify that only the selected item was processed.
	processed, _ := tracker.GetStats()
	if processed.Files != 1 {
		t.Errorf("Tracker did not record the file. Expected 1, got %d", processed.Files)
	}

	_, totalPct, _ := tracker.GetProgress()
	if totalPct != 100 {
		t.Errorf("Expected 100%% progress at the end, got %d%%", totalPct)
	}
}

type actionCaptureReporter struct {
	DummyReporter
	actions map[string]bool
	mu      sync.Mutex
}

func (r *actionCaptureReporter) StartFile(name string, size int64) {}
func (r *actionCaptureReporter) UpdateBytes(n int)                 {}
func (r *actionCaptureReporter) FileDone()                         {}
func (r *actionCaptureReporter) DirDone()                          {}
func (r *actionCaptureReporter) FileSkipped() {
	time.Sleep(15 * time.Millisecond)
}

func (r *actionCaptureReporter) UpdateTransfer(action, filename string, currentPct int, totalText string, totalPct int, speedText string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions[action] = true
}

func (r *actionCaptureReporter) hasAction(a string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.actions[a]
}

// TestIssue149_LocatingStatusReporting verifies that the "Locating" state
// is correctly reported during bulk extraction when files are being skipped.
func TestIssue149_LocatingStatusReporting(t *testing.T) {
	archive.TestSkipDelay = 15 * time.Millisecond
	defer func() { archive.TestSkipDelay = 0 }()

	vfs.RegisterProvider(&archive.ArchiveProvider{})
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "test_locating.zip")

	f, _ := os.Create(zipPath)
	zw := zip.NewWriter(f)
	// Create 50 files
	for i := 0; i < 50; i++ {
		w, _ := zw.Create(fmt.Sprintf("file_%d.txt", i))
		w.Write([]byte("data"))
	}
	zw.Close()
	f.Close()

	parentVFS := vfs.NewOSVFS(tmpDir)
	arcVfs, _ := archive.NewArchiveVFS(parentVFS, zipPath)
	defer arcVfs.Close()

	dstVFS := vfs.NewOSVFS(tmpDir)

	// Only extract the last file
	names := []string{"file_49.txt"}
	rep := &actionCaptureReporter{actions: make(map[string]bool)}

	// We need to use a context that triggers progress updates
	ctx := context.WithValue(context.Background(), "AutoQueue", true)

	// Run extraction in background to allow ticker to fire
	done := make(chan error, 1)
	go func() {
		done <- arcVfs.CopyBulk(ctx, names, dstVFS, tmpDir, rep)
	}()

	// Wait for the "Locating" status to appear
	timeout := time.After(2 * time.Second)
	found := false
	for !found {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for 'Locating' status to be reported")
		case err := <-done:
			if err != nil {
				t.Fatalf("CopyBulk failed: %v", err)
			}
			found = rep.hasAction("Locating")
			if !found {
				t.Error("'Locating' action was never reported during bulk copy")
			}
		default:
			if rep.hasAction("Locating") {
				found = true
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// TestIssue149_ETA_Stability verifies the fix for "Crazy ETA" by checking
// if processing many files without bytes still yields reasonable ETA.
func TestIssue149_ETA_Stability(t *testing.T) {
	// Scenario: 1000 tiny files, 10 bytes each. Total 10KB.
	// We've processed 500 files (5KB) in 5 seconds.
	totalStats := vfs.OpStats{Files: 1000, Bytes: 10000}
	tracker := NewFileOpTracker(totalStats)

	for i := 0; i < 500; i++ {
		tracker.StartFile("file", 10)
		tracker.UpdateBytes(10)
		tracker.FileDone()
	}

	startTime := time.Now().Add(-5 * time.Second)

	// ETA Logic from file_ops.go
	calcETA := func() string {
		processed, total := tracker.GetStats()
		elapsed := time.Since(startTime)

		const ItemOverhead = 32 * 1024
		vProcessed := float64(processed.Bytes + (processed.Files+processed.Dirs)*ItemOverhead)
		vTotal := float64(total.Bytes + (total.Files+total.Dirs)*ItemOverhead)

		if vTotal > 0 && vProcessed > 0 && elapsed.Seconds() > 0.5 {
			ratio := vProcessed / vTotal
			etaSecs := (elapsed.Seconds() / ratio) - elapsed.Seconds()
			if etaSecs < 0 {
				etaSecs = 0
			}
			if etaSecs > 359999 {
				return "Remaining: >99 hours"
			}
			etaDur := time.Duration(etaSecs * float64(time.Second))
			return fmt.Sprintf("Remaining: %02d:%02d:%02d", int(etaDur.Hours()), int(etaDur.Minutes())%60, int(etaDur.Seconds())%60)
		}
		return "Remaining: ??"
	}

	eta := calcETA()

	// If the fix is working, the ETA should be roughly 5 seconds (Time: 00:00:05)
	// because we are halfway through the items and overhead.
	// If it's broken, it might be huge because processed bytes (5KB) is very small.

	if strings.Contains(eta, ">99 hours") || strings.Contains(eta, "??") {
		t.Errorf("ETA is unrealistic: %q. Item overhead logic likely failed.", eta)
	}

	if !strings.Contains(eta, "00:00:0") { // Expecting roughly 5 seconds remaining
		t.Errorf("ETA seems incorrect: %q. Expected approx 5 seconds.", eta)
	}
}
