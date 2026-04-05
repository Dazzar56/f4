package vfs

import (
	"context"
	"path/filepath"
	"testing"
)

func TestNetFoxVFS_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "hosts.json")

	nf := NewNetFoxVFS(dbPath)

	// 1. Тест сохранения
	cfg := NetFoxConfig{
		Type: "ftp",
		Host: "example.com",
		User: "anonymous",
	}
	nf.SaveConfig("TestSite", cfg)

	// 2. Тест чтения (создаем новый экземпляр, чтобы проверить диск)
	nf2 := NewNetFoxVFS(dbPath)
	configs := nf2.getConfigs()
	if len(configs) != 1 {
		t.Fatalf("Expected 1 config, got %d", len(configs))
	}

	if configs["TestSite"].Type != "ftp" {
		t.Errorf("Expected type 'ftp', got %q", configs["TestSite"].Type)
	}

	// 3. Тест удаления
	nf2.Remove(context.Background(), "net://TestSite")
	if len(nf2.getConfigs()) != 0 {
		t.Error("Config was not removed")
	}
}

func TestNetFox_ProviderRouting(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "routing.json")
	nf := NewNetFoxVFS(dbPath)

	// Очищаем реестр провайдеров для теста
	providers = nil
	RegisterProvider(&NetFoxProvider{}) // SFTP
	RegisterProvider(&FTPProvider{})

	ctx := context.Background()

	// Кейс 1: SFTP (по умолчанию)
	nf.SaveConfig("sftp_site", NetFoxConfig{Host: "ssh.com", Type: "sftp"})
	p1 := FindProvider(ctx, nf, "net://sftp_site")
	if p1 == nil || p1.Name() != "NetFox" {
		t.Errorf("Expected NetFox (SFTP) provider for sftp_site, got %v", p1)
	}

	// Кейс 2: FTP
	nf.SaveConfig("ftp_site", NetFoxConfig{Host: "ftp.com", Type: "ftp"})
	p2 := FindProvider(ctx, nf, "net://ftp_site")
	if p2 == nil || p2.Name() != "FTP" {
		t.Errorf("Expected FTP provider for ftp_site, got %v", p2)
	}

	// Кейс 3: Корень net:// не должен обрабатываться провайдерами
	p3 := FindProvider(ctx, nf, "net://")
	if p3 != nil {
		t.Error("Root net:// should not be handled by specific protocol providers")
	}
}

func TestNetFoxVFS_ReadDir(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "readdir.json")
	nf := NewNetFoxVFS(dbPath)

	nf.SaveConfig("SiteA", NetFoxConfig{Type: "ftp"})
	nf.SaveConfig("SiteB", NetFoxConfig{Type: "sftp"})

	var items []VFSItem
	err := nf.ReadDir(context.Background(), "net://", func(chunk []VFSItem) {
		items = append(items, chunk...)
	})

	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	if len(items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(items))
	}

	foundA, foundB := false, false
	for _, itm := range items {
		if itm.Name == "SiteA" { foundA = true }
		if itm.Name == "SiteB" { foundB = true }
	}

	if !foundA || !foundB {
		t.Error("ReadDir missed some sites")
	}
}