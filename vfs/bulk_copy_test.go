package vfs

import (
	"context"
	"testing"
)

type mockBulkCopier struct {
	VFS
	called bool
}

func (m *mockBulkCopier) CopyBulk(ctx context.Context, srcPaths []string, dstVfs VFS, dstDir string, reporter TaskReporter) error {
	m.called = true
	return nil
}

func TestBulkCopierInterface(t *testing.T) {
	var v VFS = &mockBulkCopier{}
	bc, ok := v.(BulkCopier)
	if !ok {
		t.Fatal("expected VFS to implement BulkCopier")
	}
	err := bc.CopyBulk(context.Background(), []string{"file.txt"}, nil, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !v.(*mockBulkCopier).called {
		t.Fatal("expected CopyBulk to be called")
	}
}
