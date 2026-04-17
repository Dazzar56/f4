package main

import (
	"bufio"
	"fmt"
	"os/exec"

	"github.com/unxed/f4/sdk/f4rpc"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
	"github.com/vmihailenco/msgpack/v5"
)

// RPCPlugin manages the lifecycle of an external process plugin.
type RPCPlugin struct {
	path string
	cmd  *exec.Cmd
	sess *f4rpc.Session
	api  vfs.HostAPI
}

func NewRPCPlugin(path string) *RPCPlugin {
	return &RPCPlugin{path: path}
}

func (p *RPCPlugin) GetName() string {
	return p.path + " (RPC)"
}

func (p *RPCPlugin) Init(api vfs.HostAPI) error {
	p.api = api
	p.cmd = exec.Command(p.path)

	stdin, err := p.cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := p.cmd.StderrPipe()
	if err != nil {
		return err
	}

	// Forward stderr to global debug log
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			vtui.DebugLog("PLUGIN_STDERR [%s]: %s", p.path, scanner.Text())
		}
	}()

	if err := p.cmd.Start(); err != nil {
		return err
	}

	p.sess = f4rpc.NewSession(stdout, stdin)

	// Register Host API methods for the plugin to call
	p.sess.Register("Host.Log", func(data msgpack.RawMessage) (any, error) {
		var msg string
		msgpack.Unmarshal(data, &msg)
		api.Log(msg)
		return nil, nil
	})
	p.sess.Register("Host.Message", func(data msgpack.RawMessage) (any, error) {
		var msg string
		msgpack.Unmarshal(data, &msg)
		api.Message(msg)
		return nil, nil
	})
	p.sess.Register("Host.GetVersion", func(data msgpack.RawMessage) (any, error) {
		return api.GetVersion(), nil
	})

	go func() {
		err := p.sess.Serve()
		vtui.DebugLog("RPC Plugin %q exited: %v", p.path, err)
	}()

	// Query plugin for its capabilities (drives)
	type PluginInitRes struct {
		Drives []string
	}
	var res PluginInitRes
	if err := p.sess.Call("Plugin.Init", nil, &res); err != nil {
		return fmt.Errorf("Plugin.Init failed: %v", err)
	}

	for _, drive := range res.Drives {
		driveName := drive // closure capture
		api.RegisterDrive(driveName, func() vfs.VFS {
			return NewRPCVFS(p.sess, driveName)
		})
	}

	return nil
}

func (p *RPCPlugin) Close() error {
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
		p.cmd.Wait()
	}
	return nil
}