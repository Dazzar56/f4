package main

import (
	"testing"
)

func TestF4FileStateProvider_LRUOrder(t *testing.T) {
	fs := &F4FileStateProvider{
		Limit: 3,
		Data:  make(map[string]*FileState),
	}

	// Заполняем до лимита
	fs.SaveViewerState("file1", 10, true, false)
	fs.SaveViewerState("file2", 20, true, false)
	fs.SaveViewerState("file3", 30, true, false)

	// "Трогаем" первый файл — он должен стать самым "новым"
	fs.SaveViewerState("file1", 100, true, false)

	// Добавляем четвертый файл. Вытесниться должен file2 (самый старый), а не file1.
	fs.SaveViewerState("file4", 40, true, false)

	if fs.GetState("file2") != nil {
		t.Error("file2 should have been evicted as the oldest")
	}
	if fs.GetState("file1") == nil {
		t.Error("file1 should be preserved as it was recently used")
	}
}
