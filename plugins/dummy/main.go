package main

import (
	"fmt"

	"github.com/unxed/f4/sdk/f4plugin"
)

type DummyPlugin struct {
	host *f4plugin.Host
}

func (p *DummyPlugin) Init(host *f4plugin.Host) ([]string, error) {
	p.host = host
	ver := host.GetVersion()
	host.Log(fmt.Sprintf("Dummy Plugin initialized via F4-RPC! Host Version: %s", ver))
	// We deliberately log instead of triggering host.Message to avoid
	// annoying popups on application startup.
	return []string{"Dummy RPC VFS"}, nil
}

func (p *DummyPlugin) ReadDir(drive, path string) ([]f4plugin.VFSItem, error) {
	p.host.Log(fmt.Sprintf("ReadDir called for %s", path))

	var items []f4plugin.VFSItem
	if path != "/" && path != "" {
		items = append(items, f4plugin.VFSItem{Name: "..", IsDir: true})
	}

	for i := 1; i <= 10; i++ {
		items = append(items, f4plugin.VFSItem{
			Name:  fmt.Sprintf("rpc_file_%d.txt", i),
			Size:  int64(i * 1024),
			IsDir: false,
		})
	}
	items = append(items, f4plugin.VFSItem{Name: "rpc_folder", IsDir: true})
	return items, nil
}

func (p *DummyPlugin) Stat(drive, path string) (f4plugin.VFSItem, error) {
	p.host.Log(fmt.Sprintf("Stat called for %s", path))
	return f4plugin.VFSItem{Name: "item", Size: 1024, IsDir: false}, nil
}

func main() {
	f4plugin.Run(&DummyPlugin{})
}