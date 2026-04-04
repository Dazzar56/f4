package main

import (
	"context"
	"testing"
	"time"
	"os"
	"io"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
	"github.com/unxed/vtui/piecetable"
)

func TestAsyncBuffer_LoadingCycle(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

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

	// 2. Process tasks (wait for the fetch goroutine to post and for us to run the task)
	success := false
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		default:
			time.Sleep(10 * time.Millisecond)
		}

		// Check if data is now available
		data, err = buf.Read(0, 5)
		if err == nil {
			success = true
			break
		}
	}

	if !success {
		t.Fatalf("Read failed after fetch: %v", err)
	}

	// 3. Verify data content
	data, err = buf.Read(0, 5)
	if err != nil {
		t.Errorf("Read failed after fetch: %v", err)
	}
	if string(data) != "This " {
		t.Errorf("Wrong data: expected 'This ', got %q", string(data))
	}
}

func TestAsyncBuffer_BoundaryRead(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

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
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
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
func TestAsyncBuffer_ConcurrentAccess(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	// Create a decent sized file
	content := make([]byte, 1024*1024) // 1MB
	for i := range content { content[i] = byte(i % 256) }

	tmp := t.TempDir() + "/concurrent.bin"
	os.WriteFile(tmp, content, 0644)

	v := vfs.NewOSVFS(t.TempDir())
	f, _ := v.Open(context.Background(), tmp)

	buf := NewAsyncBuffer(context.Background(), f)
	buf.chunkSize = 64 * 1024 // 64KB chunks
	defer buf.Close()

	// Spin up a worker to constantly pump the UI task queue (simulating fm.Run)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			select {
			case <-ctx.Done(): return
			case task := <-vtui.FrameManager.TaskChan:
				task()
			}
		}
	}()

	// Fire 50 concurrent reads across different overlapping chunks
	done := make(chan bool)
	for i := 0; i < 50; i++ {
		go func(offset int) {
			for retries := 0; retries < 100; retries++ {
				_, err := buf.Read(offset, 100)
				if err == nil { break }
				if err != piecetable.ErrLoading {
					t.Errorf("Unexpected error: %v", err)
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			done <- true
		}(i * 10000) // Stagger offsets
	}

	// Wait for all goroutines
	timeout := time.After(3 * time.Second)
	for i := 0; i < 50; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("Concurrency test deadlocked or timed out")
		}
	}
}
func TestAsyncBuffer_CancellationMidFetch(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	v := vfs.NewOSVFS(t.TempDir())
	tmp := t.TempDir() + "/cancel.txt"
	os.WriteFile(tmp, []byte("some content"), 0644)
	f, _ := v.Open(context.Background(), tmp)

	ctx, cancel := context.WithCancel(context.Background())
	buf := NewAsyncBuffer(ctx, f)
	buf.chunkSize = 100

	// 1. Trigger fetch
	_, err := buf.Read(0, 5)
	if err != piecetable.ErrLoading { t.Fatal("Expected ErrLoading") }

	// 2. Cancel context while fetch is (presumably) in flight
	cancel()

	// 3. Pump tasks - the fetch result should be ignored because of b.ctx.Err()
	timeout := time.After(100 * time.Millisecond)
Loop:
	for {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-timeout:
			break Loop
		}
	}

	// 4. Verification: data should NOT be in 'loaded' map
	buf.mu.Lock()
	if len(buf.loaded) > 0 {
		t.Error("Data was loaded into buffer after context cancellation")
	}
	buf.mu.Unlock()
}
func TestAsyncBuffer_RedundantFetchPrevention(t *testing.T) {
	// Tests that the 'fetching' map correctly prevents multiple goroutines
	// for the same chunk index.
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	v := vfs.NewOSVFS(t.TempDir())
	tmp := t.TempDir() + "/redundant.txt"
	os.WriteFile(tmp, make([]byte, 1000), 0644)
	f, _ := v.Open(context.Background(), tmp)

	buf := NewAsyncBuffer(context.Background(), f)
	buf.chunkSize = 100
	defer buf.Close()

	// Trigger 10 simultaneous reads for the same offset
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = buf.Read(0, 5)
		}()
	}

	// Give goroutines time to start
	time.Sleep(10 * time.Millisecond)

	buf.mu.Lock()
	if len(buf.fetching) > 1 {
		t.Errorf("Expected at most 1 in-flight fetch for chunk 0, got %d", len(buf.fetching))
	}
	buf.mu.Unlock()
}

func TestAsyncBuffer_ContextRace(t *testing.T) {
	// Simulates the scenario where a context is cancelled exactly when data arrives.
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	
	content := []byte("Race test content")
	v := vfs.NewOSVFS(t.TempDir())
	tmp := t.TempDir() + "/race.txt"
	os.WriteFile(tmp, content, 0644)
	f, _ := v.Open(context.Background(), tmp)

	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		buf := NewAsyncBuffer(ctx, f)
		buf.chunkSize = 5
		
		// Start fetch
		go func() {
			_, _ = buf.Read(0, 5)
		}()
		
		// Immediate cancel to hit the race window in fetchChunk
		cancel()
		
		// Pump tasks
		timeout := time.After(10 * time.Millisecond)
	loop:
		for {
			select {
			case task := <-vtui.FrameManager.TaskChan:
				task()
			case <-timeout:
				break loop
			}
		}
		buf.Close()
	}
	// If no panic or deadlock occurred in 100 iterations, the mutex/PostTask logic is likely sound.
}
type mockErrorFile struct {
	vfs.ReadAtCloser
	errToReturn error
}

func (m *mockErrorFile) ReadAt(ctx context.Context, p []byte, off int64) (int, error) {
	return 0, m.errToReturn
}

func TestAsyncBuffer_ErrorRecovery(t *testing.T) {
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())

	f := &mockErrorFile{errToReturn: io.ErrUnexpectedEOF}
	// Manual Size() for mock
	buf := &AsyncBuffer{
		file:     f,
		size:     100,
		ctx:      context.Background(),
		loaded:   make(map[int][]byte),
		fetching: make(map[int]bool),
		chunkSize: 10,
	}

	// 1. Trigger read that fails
	_, err := buf.Read(0, 5)
	if err != piecetable.ErrLoading { t.Fatal("Should report loading") }

	// 2. Process tasks to handle the failure
	timeout := time.After(200 * time.Millisecond)
Loop:
	for {
		select {
		case task := <-vtui.FrameManager.TaskChan:
			task()
		case <-timeout:
			break Loop
		}
	}

	// 3. Verify 'fetching' state was cleared so we can retry
	buf.mu.Lock()
	isFetching := buf.fetching[0]
	buf.mu.Unlock()

	if isFetching {
		t.Error("Fetching flag was not cleared after read error")
	}

	// 4. Fix the error in mock and retry
	f.errToReturn = nil
	_, err = buf.Read(0, 5)
	if err != piecetable.ErrLoading {
		t.Fatal("Should trigger fetch again after error recovery")
	}
}
