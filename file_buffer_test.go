package main

import (
	"context"
	"testing"
	"time"
	"os"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
	"github.com/unxed/vtui/piecetable"
)

func TestAsyncBuffer_LoadingCycle(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewScreenBuf())

	content := []byte("This is a test file content for async buffer.")
	tmp := t.TempDir() + "/test.txt"
	v := vfs.NewOSVFS(t.TempDir())
	wc, _ := v.Create(context.Background(), tmp)
	wc.Write(content)
	wc.Close()

	f, _ := v.Open(context.Background(), tmp)
	// Create buffer with very small chunks (10 bytes) to trigger multi-chunk logic
	buf := NewAsyncBuffer(context.Background(), f)
	buf.chunkSize = 10
	defer buf.Close()

	// 1. Initial read should return ErrLoading
	data, err := buf.Read(0, 5)
	if err != piecetable.ErrLoading {
		t.Errorf("Expected ErrLoading, got %v", err)
	}
	if data != nil {
		t.Error("Data should be nil when loading")
	}

	// 2. Process tasks (the fetch goroutine should have posted a task)
	timeout := time.After(1 * time.Second)
	select {
	case task := <-vtui.FrameManager.TaskChan:
		task()
	case <-timeout:
		t.Fatal("Timeout waiting for fetch task")
	}

	// 3. Second read should succeed
	data, err = buf.Read(0, 5)
	if err != nil {
		t.Errorf("Read failed after fetch: %v", err)
	}
	if string(data) != "This " {
		t.Errorf("Wrong data: expected 'This ', got %q", string(data))
	}
}

func TestAsyncBuffer_BoundaryRead(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewScreenBuf())

	// Content: 0123456789ABCDEFGHIJ (20 bytes)
	content := []byte("0123456789ABCDEFGHIJ")
	tmp := t.TempDir() + "/boundary.txt"
	os.WriteFile(tmp, content, 0644)

	v := vfs.NewOSVFS(t.TempDir())
	f, _ := v.Open(context.Background(), tmp)

	// Chunk size 10.
	buf := NewAsyncBuffer(context.Background(), f)
	buf.chunkSize = 10
	defer buf.Close()

	// 1. Read spanning across chunk 0 and chunk 1: "89AB"
	// Indices 8, 9 (Chunk 0) and 10, 11 (Chunk 1)
	for {
		_, err := buf.Read(8, 4)
		if err == piecetable.ErrLoading {
			task := <-vtui.FrameManager.TaskChan
			task()
			continue
		}
		break
	}

	data, _ := buf.Read(8, 4)
	if string(data) != "89AB" {
		t.Errorf("Boundary read failed: expected '89AB', got %q", string(data))
	}
}

func TestAsyncBuffer_PartialChunkAtEOF(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewScreenBuf())
	content := []byte("Short") // 5 bytes
	tmp := t.TempDir() + "/eof.txt"
	os.WriteFile(tmp, content, 0644)

	v := vfs.NewOSVFS(t.TempDir())
	f, _ := v.Open(context.Background(), tmp)

	buf := NewAsyncBuffer(context.Background(), f)
	buf.chunkSize = 100 // Chunk is larger than file
	defer buf.Close()

	for {
		_, err := buf.Read(0, 5)
		if err == piecetable.ErrLoading {
			task := <-vtui.FrameManager.TaskChan
			task()
			continue
		}
		break
	}

	data, _ := buf.Read(0, 5)
	if string(data) != "Short" {
		t.Errorf("EOF chunk failed: expected 'Short', got %q", string(data))
	}
}
