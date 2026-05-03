#!/usr/bin/env lua

-- Try to load luarocks paths if available to find MessagePack
pcall(require, "luarocks.loader")

-- Hybrid path resolution:
-- 1. Check script's own directory (for bundled/standalone distribution)
-- 2. Check repo-relative directory (for development inside f4 source tree)
local script_path = debug.getinfo(1).source:match("@?(.*[/\\])") or "./"
package.path = script_path .. "?.lua;" .. script_path .. "../../sdk/lua/?.lua;" .. package.path

-- Let require fail naturally so it prints the exact missing dependency (e.g., MessagePack)
local f4rpc = require('f4rpc')

-- 1. Helper for Host calls
local host = {}
function host.Log(m) f4rpc.call("Host.Log", m) end
function host.Message(m) f4rpc.call("Host.Message", m) end

-- 2. Mock Data
local virtual_files = {
    ["/readme.txt"] = "This filesystem is implemented entirely in Lua.\nIt communicates with f4 core over standard I/O pipes.",
    ["/test.log"] = "Log entry 1\nLog entry 2\n"
}

-- 3. Initialization
f4rpc.register("Plugin.Init", function()
    host.Log("Lua Plugin: Initializing...")
    return { Drives = { "Lua Virtual Drive" } }
end)

-- 4. VFS Implementation
f4rpc.register("VFS.ReadDir", function(req)
    local items = {}
    if req.Path == "/" or req.Path == "" then
        for name, content in pairs(virtual_files) do
            table.insert(items, {
                Name = name:sub(2),
                Size = #content,
                IsDir = false
            })
        end
        table.insert(items, { Name = "subfolder", IsDir = true, Size = 0 })
    end
    return items
end)

f4rpc.register("VFS.Stat", function(req)
    local p = req.Path
    if p == "/" or p == "" or p == "." then
        return { Name = "/", IsDir = true }
    end
    if p == "subfolder" or p == "/subfolder" then
        return { Name = "subfolder", IsDir = true }
    end
    local content = virtual_files["/" .. p] or virtual_files[p]
    if content then
        return { Name = p, IsDir = false, Size = #content }
    end
    error("file not found")
end)

-- 5. File Content Access (Streaming)
local open_handles = {}
local next_handle = 1

f4rpc.register("VFS.Open", function(req)
    local content = virtual_files["/" .. req.Path] or virtual_files[req.Path]
    if not content then error("not found") end

    local h = next_handle
    next_handle = next_handle + 1
    open_handles[h] = content
    return { ID = h, Size = #content }
end)

f4rpc.register("VFS.ReadAt", function(req)
    local data = open_handles[req.ID]
    if not data then error("invalid handle") end

    -- Lua strings are 1-indexed
    local start = req.Off + 1
    local finish = req.Off + req.Len
    return data:sub(start, finish)
end)

f4rpc.register("VFS.CloseFile", function(req)
    open_handles[req.ID] = nil
    return nil
end)

-- 6. Start Loop
f4rpc.serve()