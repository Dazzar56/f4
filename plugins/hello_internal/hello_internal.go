package hello_internal

import "github.com/unxed/f4/vfs"

type InternalHelloPlugin struct {}

func (p *InternalHelloPlugin) Init(api vfs.HostAPI) error {
//	api.Message("Hello from Internal Go Plugin! F4 version: " + api.GetVersion())
	return nil
}

func (p *InternalHelloPlugin) Close() error { return nil }
func (p *InternalHelloPlugin) GetName() string { return "Internal Hello World" }