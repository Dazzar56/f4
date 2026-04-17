package f4plugin

import (
	"os"
	"time"

	"github.com/unxed/f4/sdk/f4rpc"
	"github.com/vmihailenco/msgpack/v5"
)

// Host provides methods for the plugin to interact with the main f4 application.
type Host struct {
	sess *f4rpc.Session
}

func (h *Host) Log(msg string) {
	_ = h.sess.Call("Host.Log", msg, nil)
}

func (h *Host) Message(msg string) {
	_ = h.sess.Call("Host.Message", msg, nil)
}

func (h *Host) GetVersion() string {
	var ver string
	_ = h.sess.Call("Host.GetVersion", nil, &ver)
	return ver
}

// VFSItem mirrors the core's vfs.VFSItem format.
type VFSItem struct {
	Name         string
	Size         int64
	IsDir        bool
	MTime        time.Time
	Mode         string
	IsExecutable bool
	IsHidden     bool
}

// Plugin is the primary interface a plugin developer implements.
type Plugin interface {
	Init(host *Host) ([]string, error)
	ReadDir(drive, path string) ([]VFSItem, error)
	Stat(drive, path string) (VFSItem, error)
}

// Run attaches the plugin to stdin/stdout and starts the RPC server loop.
func Run(p Plugin) {
	sess := f4rpc.NewSession(os.Stdin, os.Stdout)
	host := &Host{sess: sess}

	sess.Register("Plugin.Init", func(data msgpack.RawMessage) (any, error) {
		drives, err := p.Init(host)
		return map[string]any{"Drives": drives}, err
	})

	sess.Register("VFS.ReadDir", func(data msgpack.RawMessage) (any, error) {
		var req map[string]string
		if err := msgpack.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		items, err := p.ReadDir(req["Drive"], req["Path"])
		return items, err
	})

	sess.Register("VFS.Stat", func(data msgpack.RawMessage) (any, error) {
		var req map[string]string
		if err := msgpack.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		item, err := p.Stat(req["Drive"], req["Path"])
		return item, err
	})

	_ = sess.Serve()
}