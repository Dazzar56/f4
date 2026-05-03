# f4 Lua Dummy Plugin

A reference Virtual File System (VFS) implementation written in Lua using the F4-RPC protocol.

## Setup

1. Install Lua and MessagePack:
   ```bash
   sudo apt install lua5.3 liblua5.3-dev luarocks
   sudo luarocks --lua-version 5.3 install lua-MessagePack
   ```
2. Ensure the plugin is executable: `chmod +x plugin.lua`
3. Note on SDK: To run inside the `f4` repo, this plugin finds `f4rpc.lua` automatically. For standalone use, you would copy `f4rpc.lua` from `sdk/lua/` into this folder.
4. In `f4`, go to `F9` -> Options -> Manage Plugins.
5. Add `plugin.lua` from this directory.
6. Restart `f4` and look for "Lua Virtual Drive" in the Drive Menu (`Alt+F1`/`Alt+F2`).
