package main

import (
	"context"
	"fmt"

	"github.com/unxed/f4/sdk/f4rpc"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
	"github.com/vmihailenco/msgpack/v5"
)

// PluginTransport is the one call shape every plugin transport implements.
// Out of process it is a MessagePack request over a pipe; embedded it is a
// direct dispatch into an interpreter. Everything above this interface, the
// VFS proxy and the host API included, is written once and works for both.
type PluginTransport interface {
	Call(method string, params any, result any) error
}

// newHostMethods builds the host side of the F4-RPC surface: the methods a
// plugin may call on f4.
//
// back is how the host reaches the plugin, for the calls that are answers to
// something the plugin asked for (a hotkey it registered, a progress task it
// started). Keeping it an interface is what makes this set of methods shared
// between transports instead of copied per transport.
func newHostMethods(api vfs.HostAPI, back PluginTransport, name string) map[string]f4rpc.Handler {
	methods := make(map[string]f4rpc.Handler)

	methods["Host.Log"] = func(data msgpack.RawMessage) (any, error) {
		var msg string
		msgpack.Unmarshal(data, &msg)
		api.Log(msg)
		return nil, nil
	}

	methods["Host.Message"] = func(data msgpack.RawMessage) (any, error) {
		var msg string
		msgpack.Unmarshal(data, &msg)
		api.Message(msg)
		return nil, nil
	}

	methods["Host.GetVersion"] = func(data msgpack.RawMessage) (any, error) {
		return api.GetVersion(), nil
	}

	methods["Host.RegisterHighlighter"] = func(data msgpack.RawMessage) (any, error) {
		api.RegisterHighlighter(&rpcHighlighterProvider{transport: back, name: name})
		return nil, nil
	}

	methods["Host.RegisterGlobalHotkey"] = func(data msgpack.RawMessage) (any, error) {
		var req HotkeyReq
		msgpack.Unmarshal(data, &req)
		api.RegisterGlobalHotkey(req.VK, vtinput.ControlKeyState(req.Mods), func(app vfs.App) {
			_ = back.Call("Plugin.OnHotkey", req, nil)
		})
		return nil, nil
	}

	// State of the progress task currently owned by this plugin.
	var taskUpdate func(string, int)
	var taskCtx context.Context
	var taskAnchor vtui.Frame

	methods["Host.RunProgressTask"] = func(data msgpack.RawMessage) (any, error) {
		var req ProgressTaskReq
		msgpack.Unmarshal(data, &req)
		vtui.FrameManager.PostTask(func() {
			pf := findPanelsFrame()
			if pf == nil {
				return
			}

			pf.RunProgressTask(req.Title, req.StartMsg, req.Forked, func(ctx context.Context, update func(msg string, percent int)) error {
				taskUpdate = update
				taskCtx = ctx
				taskAnchor = vtui.FrameManager.GetTopFrame()
				return back.Call("Plugin.OnProgressTask", nil, nil)
			}, func(err error) {
				taskUpdate = nil
				taskCtx = nil
				taskAnchor = nil
			})
		})
		return nil, nil
	}

	methods["Host.UpdateProgress"] = func(data msgpack.RawMessage) (any, error) {
		var req ProgressUpdateReq
		msgpack.Unmarshal(data, &req)
		if taskUpdate != nil {
			taskUpdate(req.Msg, req.Percent)
		}
		return nil, nil
	}

	methods["Host.IsProgressCancelled"] = func(data msgpack.RawMessage) (any, error) {
		if taskCtx != nil {
			return taskCtx.Err() != nil, nil
		}
		return false, nil
	}

	methods["Host.AskOverwrite"] = func(data msgpack.RawMessage) (any, error) {
		var req AskOverwriteReq
		msgpack.Unmarshal(data, &req)
		ctx := taskCtx
		if ctx == nil {
			ctx = context.Background()
		}
		choice, remember := AskOverwrite(ctx, req.Path, req.Src, req.Dst, taskAnchor)
		return AskOverwriteRes{Choice: choice, Remember: remember}, nil
	}

	methods["Host.AskError"] = func(data msgpack.RawMessage) (any, error) {
		var req AskErrorReq
		msgpack.Unmarshal(data, &req)
		ctx := taskCtx
		if ctx == nil {
			ctx = context.Background()
		}
		choice := AskError(ctx, req.Op, fmt.Errorf("%s", req.Err), taskAnchor)
		return choice, nil
	}

	methods["Host.InputBox"] = func(data msgpack.RawMessage) (any, error) {
		var req InputBoxReq
		msgpack.Unmarshal(data, &req)
		resChan := make(chan string, 1)
		vtui.FrameManager.PostTask(func() {
			vtui.InputBox(req.Title, req.Prompt, req.Default, func(s string) {
				resChan <- s
			})
		})
		return <-resChan, nil
	}

	methods["Host.Menu"] = func(data msgpack.RawMessage) (any, error) {
		var req MenuReq
		msgpack.Unmarshal(data, &req)
		resChan := make(chan int, 1)
		vtui.FrameManager.PostTask(func() {
			pf := findPanelsFrame()
			if pf != nil {
				pf.Menu(req.Title, req.Items, func(idx int) { resChan <- idx })
			} else {
				resChan <- -1
			}
		})
		return <-resChan, nil
	}

	return methods
}

// findPanelsFrame locates the panels frame of the active screen, if any.
func findPanelsFrame() *PanelsFrame {
	if vtui.FrameManager == nil || len(vtui.FrameManager.Screens) == 0 {
		return nil
	}
	for _, f := range vtui.FrameManager.Screens[vtui.FrameManager.ActiveIdx].Frames {
		if pf, ok := f.(*PanelsFrame); ok {
			return pf
		}
	}
	return nil
}

type rpcHighlighterProvider struct {
	transport PluginTransport
	name      string
}

func (r *rpcHighlighterProvider) Name() string           { return r.name }
func (r *rpcHighlighterProvider) Match(f, c string) bool { return true }
func (r *rpcHighlighterProvider) Create(f, c string) vtui.Highlighter {
	return &rpcHighlighter{transport: r.transport}
}

type rpcHighlighter struct {
	transport PluginTransport
}

func (h *rpcHighlighter) Highlight(line string, prev any, base uint64) ([]uint64, any) {
	var res HighlightRes
	err := h.transport.Call("VFS.Highlight", HighlightReq{Line: line, Prev: prev, Base: base}, &res)
	if err != nil {
		return nil, nil
	}
	return res.Attrs, res.Next
}
