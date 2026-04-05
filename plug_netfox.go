package main

import "github.com/unxed/f4/vfs"

type NetFoxPlugin struct{}

func (p *NetFoxPlugin) Init(api HostAPI) error {
	api.RegisterVFSProvider(&vfs.NetFoxProvider{})
	return nil
}

func (p *NetFoxPlugin) Close() error { return nil }
func (p *NetFoxPlugin) GetName() string { return "NetFox (SFTP) Support" }