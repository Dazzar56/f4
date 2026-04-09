package vfs

import (
	"context"
	"testing"
	"time"
	"io"
)

func TestNullVFS_DirectoryListing(t *testing.T) {
	v := NewNullVFS(0)
	ctx := context.Background()

	var files []string
	v.ReadDir(ctx, "/", func(items []VFSItem) {
		for _, item := range items {
			files = append(files, item.Name)
		}
	})

	if len(files) != 6 {
		t.Errorf("Expected 6 items in root (5 files + 1 upload dir), got %d", len(files))
	}
}

func TestNullVFS_Stat(t *testing.T) {
	v := NewNullVFS(0)
	ctx := context.Background()

	// 1. Root static file
	stat, err := v.Stat(ctx, "/10MB.bin")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if stat.Size != 10*1024*1024 {
		t.Errorf("Expected 10MB size, got %d", stat.Size)
	}

	// 2. Upload dir
	statDir, _ := v.Stat(ctx, "/upload")
	if !statDir.IsDir {
		t.Error("/upload should be a directory")
	}

	// 3. Non-existent / uploaded file
	statDummy, _ := v.Stat(ctx, "/upload/test.txt")
	if statDummy.Size != 0 {
		t.Error("Dummy uploaded file should have size 0")
	}
}

func TestNullVFS_Throttling(t *testing.T) {
	// Speed: 10 MB/s
	speed := int64(10 * 1024 * 1024)
	v := NewNullVFS(speed)
	ctx := context.Background()

	f, err := v.Open(ctx, "/10MB.bin")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	buf := make([]byte, 1024*1024) // 1 MB chunk

	start := time.Now()
	n, err := f.Read(ctx, buf)
	duration := time.Since(start)

	if err != nil || n != len(buf) {
		t.Fatalf("Read failed. n=%d, err=%v", n, err)
	}

	// 1 MB at 10 MB/s should take ~100ms
	expectedMs := 100
	actualMs := int(duration.Milliseconds())

	// Allow some jitter
	if actualMs < expectedMs-20 || actualMs > expectedMs+50 {
		t.Errorf("Throttling inaccurate: expected ~%dms, got %dms", expectedMs, actualMs)
	}
}

func TestNullVFS_Writer(t *testing.T) {
	v := NewNullVFS(0)
	ctx := context.Background()

	// Prevent overwriting static root files
	_, err := v.Create(ctx, "/10MB.bin")
	if err == nil {
		t.Error("Create should prevent overwriting static files at root")
	}

	// Allow creating in upload
	w, err := v.Create(ctx, "/upload/test.txt")
	if err != nil {
		t.Fatalf("Create failed in upload dir: %v", err)
	}

	n, err := w.Write([]byte("test data"))
	if err != nil || n != 9 {
		t.Errorf("Write failed: n=%d, err=%v", n, err)
	}
	w.Close()
}

func TestNullVFS_ReadZeroes(t *testing.T) {
	v := NewNullVFS(0)
	f, _ := v.Open(context.Background(), "/1KB.bin")

	buf := []byte{1, 2, 3, 4, 5}
	n, _ := f.Read(context.Background(), buf)

	if n != 5 {
		t.Errorf("Expected to read 5 bytes, got %d", n)
	}

	// Buffer should be overwritten with zeroes
	for i, b := range buf {
		if b != 0 {
			t.Errorf("Buffer at %d was not zeroed: %d", i, b)
		}
	}
}
func TestNullVFS_EOF(t *testing.T) {
	v := NewNullVFS(0)
	ctx := context.Background()
	f, _ := v.Open(ctx, "/1KB.bin")

	// Seek to end
	buf := make([]byte, 10)
	n, err := f.ReadAt(ctx, buf, 1024)
	if n != 0 || err != io.EOF {
		t.Errorf("Expected EOF at offset 1024, got n=%d, err=%v", n, err)
	}
}

func TestNullVFS_Cancellation(t *testing.T) {
	// Speed 1 byte / second
	v := NewNullVFS(1)
	ctx, cancel := context.WithCancel(context.Background())

	f, _ := v.Open(ctx, "/1KB.bin")

	// Trigger cancellation in 50ms
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	buf := make([]byte, 100)
	_, err := f.Read(ctx, buf)
	duration := time.Since(start)

	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got %v", err)
	}

	// Should return almost immediately (~50ms), not in 100 seconds
	if duration > 500*time.Millisecond {
		t.Errorf("Cancellation took too long: %v", duration)
	}
}
func TestNullVFS_BasicMethods(t *testing.T) {
	v := NewNullVFS(0)

	if !v.IsAtRoot() {
		t.Error("Should be at root initially")
	}

	if v.GetPath() != "/" {
		t.Errorf("Expected path /, got %s", v.GetPath())
	}

	v.SetPath("/upload")
	if v.IsAtRoot() {
		t.Error("Should not be at root after SetPath(/upload)")
	}

	if v.Join("/a", "b") != "/a/b" {
		t.Errorf("Join failed: %s", v.Join("/a", "b"))
	}

	if v.Base("/upload/file.bin") != "file.bin" {
		t.Errorf("Base failed: %s", v.Base("/upload/file.bin"))
	}

	if v.Dir("/upload/file.bin") != "/upload" {
		t.Errorf("Dir failed: %s", v.Dir("/upload/file.bin"))
	}

	clone := v.Clone()
	if clone.GetPath() != "/" { // Clones start at root by default in NewNullVFS
		t.Errorf("Clone path mismatch: %s", clone.GetPath())
	}

	if v.ParentVFS() != nil {
		t.Error("NullVFS should not have a parent")
	}
}
