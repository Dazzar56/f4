package main

import (
	"context"
	"io"
	"testing"

	"github.com/unxed/f4/sdk/f4rpc"
	"github.com/unxed/f4/vfs"
	"github.com/vmihailenco/msgpack/v5"
)

func TestRPCVFS_ReadDir(t *testing.T) {
	// Используем пайпы для эмуляции связи "ядро <-> плагин"
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()

	clientSess := f4rpc.NewSession(s2cR, c2sW) // Для ядра f4
	serverSess := f4rpc.NewSession(c2sR, s2cW) // Для "плагина"

	// Мокаем ответ от плагина
	serverSess.Register("VFS.ReadDir", func(data msgpack.RawMessage) (any, error) {
		var req map[string]string
		msgpack.Unmarshal(data, &req)

		// Проверяем, что запрос пришел в правильный драйв и путь
		if req["Drive"] == "dummy_drive" && req["Path"] == "/test" {
			return []vfs.VFSItem{
				{Name: "virtual_file.txt", Size: 4096, IsDir: false},
			}, nil
		}
		return []vfs.VFSItem{}, nil
	})

	go serverSess.Serve()
	go clientSess.Serve()

	// Инициализируем VFS-адаптер на стороне ядра
	v := NewRPCVFS(clientSess, "dummy_drive")

	var received []vfs.VFSItem
	err := v.ReadDir(context.Background(), "/test", func(chunk []vfs.VFSItem) {
		received = append(received, chunk...)
	})

	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	if len(received) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(received))
	}
	if received[0].Name != "virtual_file.txt" || received[0].Size != 4096 {
		t.Errorf("Unexpected item data: %+v", received[0])
	}
}

func TestRPCVFS_PathResolution(t *testing.T) {
	// Проверяем корректность работы встроенных методов (без RPC)
	v := NewRPCVFS(nil, "dummy")

	if !v.IsAtRoot() {
		t.Error("Should be at root initially")
	}

	v.SetPath("/folder/sub")

	if v.IsAtRoot() {
		t.Error("Should not be at root after SetPath")
	}

	if v.GetPath() != "/folder/sub" {
		t.Errorf("Expected path '/folder/sub', got %q", v.GetPath())
	}

	abs, _ := v.Abs("file.txt")
	if abs != "/folder/sub/file.txt" {
		t.Errorf("Abs failed: expected '/folder/sub/file.txt', got %q", abs)
	}

	if v.Base("/folder/sub/file.txt") != "file.txt" {
		t.Errorf("Base failed: expected 'file.txt', got %q", v.Base("/folder/sub/file.txt"))
	}
}