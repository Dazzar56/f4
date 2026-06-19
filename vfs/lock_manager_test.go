package vfs

import (
	"sync"
	"testing"
	"time"
)

func TestArchiveLockManager(t *testing.T) {
	mgr := &ArchiveLockManager{
		conds: make(map[string]*sync.Cond),
		busy:  make(map[string]bool),
	}

	path := "/path/to/archive.zip"

	// 1. First lock should succeed immediately
	if !mgr.TryLock(path) {
		t.Fatal("expected first TryLock to succeed")
	}

	// 2. Second lock should fail
	if mgr.TryLock(path) {
		t.Fatal("expected second TryLock to fail")
	}

	// 3. Start a goroutine to wait for the lock
	var wg sync.WaitGroup
	wg.Add(1)
	lockAcquired := false

	go func() {
		defer wg.Done()
		mgr.Lock(path)
		lockAcquired = true
		mgr.Unlock(path)
	}()

	// Sleep to let the goroutine block on Lock()
	time.Sleep(50 * time.Millisecond)
	if lockAcquired {
		t.Fatal("expected goroutine to be blocked")
	}

	// 4. Unlock the first one, which should release the goroutine
	mgr.Unlock(path)
	wg.Wait()

	if !lockAcquired {
		t.Fatal("expected goroutine to have acquired and released the lock")
	}
}