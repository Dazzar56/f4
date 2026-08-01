#!/usr/bin/env lua

-- Try to load luarocks paths if available to find MessagePack
pcall(require, "luarocks.loader")
package.path = "./?.lua;" .. package.path

local f4rpc = require('f4rpc')

f4rpc.register("Plugin.Init", function()
    f4rpc.call("Host.Log", "Hello PlugRing plugin initialized!")

    -- Register global hotkey: Ctrl+Shift+H
    -- VK_H = 0x48 (72). Mods: Shift(1) | Ctrl(4) = 5
    f4rpc.call("Host.RegisterGlobalHotkey", { VK = 72, Mods = 5 })

    return { Drives = {} }
end)

f4rpc.register("Plugin.OnHotkey", function(req)
    f4rpc.call("Host.Message", "Hello from the PlugRing test plugin!\nYou pressed Ctrl+Shift+H.")
    return nil
end)

f4rpc.serve()