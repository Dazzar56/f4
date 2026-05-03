package dummy_internal

import (
	"fmt"
	"github.com/unxed/f4/vfs"
)

type InternalDummyPlugin struct{}

func (p *InternalDummyPlugin) Init(api vfs.HostAPI) error {
	ver := api.GetVersion()
	api.Log(fmt.Sprintf("Internal dummy plugin initialized! Host Version: %s", ver))
	return nil
}

func (p *InternalDummyPlugin) Close() error    { return nil }
func (p *InternalDummyPlugin) GetName() string { return "Internal Hello World" }
