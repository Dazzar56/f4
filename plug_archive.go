package main

import "github.com/unxed/f4/vfs"

// ArchivePlugin — внутренний плагин для работы с архивами.
type ArchivePlugin struct{}

func (p *ArchivePlugin) Init(api HostAPI) error {
	api.RegisterVFSProvider(&vfs.ArchiveProvider{})
	return nil
}

func (p *ArchivePlugin) Close() error { return nil }
func (p *ArchivePlugin) GetName() string { return "Archive Support" }