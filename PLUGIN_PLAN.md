# Plugin system overhaul: plan and rationale

This document exists so the work can be resumed from the repository alone. It
records what is being built, which decisions were made and why, and where the
work currently stands.

## Why

f4 exists partly because plugins in Far3, far2l and far2m are painful:

- they are not portable (the Lua plugin interfaces of Far3 and far2m are not
  fully compatible, and the DLLs are bound to the Windows API);
- binary plugins on Linux are bound to a libc version, which turns
  distribution into a mess, against a single f4 binary;
- far2l has no Lua at all;
- writing C or C++ is a high barrier to entry.

Nobody writes f4 plugins yet either. The likely reasons: f4 already ships with
batteries included, the barrier to entry is high and there is no getting
started guide, portability is unsolved (write in Lua and the target system may
not have Lua), and the API differs from Far3 so porting is a chore.

## The architecture

**One protocol, three transports.**

f4 already had an out-of-process plugin protocol: F4-RPC, MessagePack over
stdin/stdout, with method names like `Plugin.Init`, `VFS.ReadDir` and
`Host.Log`. Rather than growing a second and third plugin API for embedded
Lua and embedded wasm, all three transports carry that same protocol:

| transport | where the plugin runs | why it exists |
| --- | --- | --- |
| subprocess | its own process | any language, full OS access |
| embedded Lua | in f4, on gopher-lua | zero install, ships as one file |
| embedded wasm | in f4, on wazero | zero install, real sandbox, any source language |

The wasm guest is a WASI command, not a module of exported functions: it reads
F4-RPC from stdin and writes it to stdout, exactly as a subprocess plugin does.
So the same plugin source builds either to a native binary or to a `.wasm`
with no changes, and the transport costs a pair of pipes rather than a third
plugin ABI.

The consequences are the point: one SDK surface, one set of documentation, one
set of host methods, and a plugin that moves between transports without being
rewritten. `PluginTransport` in `plughost.go` is the whole seam, a single
`Call(method, params, result)`; `newHostMethods` is the host side, shared by
every transport.

An earlier version of `PLUGINS.md` argued against embedded interpreters on
three grounds. They are answered rather than ignored: binary bloat is handled
with build tags, the sandbox's inability to reach native APIs is handled by the
FFI bridge, and language lock-in does not apply because the subprocess
transport never went away.

## Decisions and their rationale

1. **FFI is projected into the sandbox instead of wrapping host APIs.** Writing
   a wrapper for every Windows API function is not a plan. The plugin describes
   the ABI it wants and the broker performs the call. See `FFI.md`.

2. **A signature mini-language, not a C declaration parser.** `i64(str)` rather
   than `size_t strlen(const char *)`. A cdef-style front end can be layered on
   later without disturbing anything beneath it, and it is needed only for
   porting existing LuaJIT code.

3. **The Lua FFI module is named `f4ffi`, not `ffi`.** Ours is signature
   strings and raw addresses; LuaJIT's is `cdef` and `ffi.new`. Taking the name
   would make ported code fail late and confusingly rather than immediately and
   clearly. A future cdef front end can take the name honestly.

4. **The embedded runtime preloads `f4rpc`.** A plugin written against the
   subprocess SDK runs embedded without modification, because `require('f4rpc')`
   finds the preloaded module and never reaches the file that would drag in a
   MessagePack rock.

5. **Values cross the Lua boundary through MessagePack.** It costs a round trip
   but guarantees both transports agree on field naming, which is precisely
   where the older Far plugin APIs drifted apart.

6. **`print` is redirected into the host log, and `os`/`io` stay closed.** An
   in-process plugin writing to stdout would corrupt the screen f4 is drawing.

7. **One goroutine per Lua state, with an inline path for re-entry.** A native
   callback invoked on the runtime's own worker goroutine must not be queued or
   the worker deadlocks against itself.

8. **far2m's Lua API is the compatibility target, before Far3's.** The two
   share an ancestor in luafar, but far2m's is already free of Windows
   specifics. Implementing it gets most of Far3 for free; the remainder is a
   `winapi` shim later.

9. **No cgo, anywhere.** Portability is the reason f4 exists.

10. **The permission model is deliberately last.** This is alpha, and something
    working end to end is worth more than a model that is right the first time.
    Every dangerous operation already funnels through one hook, so adding it
    later is not invasive.

## Status

Done:

- **Step 1: `ffibridge`.** The FFI broker over pureffi. Signature parsing,
  dynamic dispatch through a runtime-built `reflect.FuncOf` type, a memory
  arena, callbacks, raw peek and poke, and a permission hook that is currently
  left open. Degrades to a stub under the `noffi` build tag.
- **Step 2: `luaplug`.** The embedded gopher-lua runtime: the `f4rpc` module,
  the `f4ffi` module, the sandbox, a worker goroutine per state and a deadline
  on every entry into the interpreter.
- **Step 2b: transports.** `PluginTransport` and shared `newHostMethods`;
  `LuaPlugin` mounts a Lua script as an in-process plugin. `plugins/dummy_lua`
  now runs without a system Lua.
- **Step 3: Far-compatible Lua macros.** `Macro{}`, `Keys()`, `akey()`,
  `Area`, `APanel`/`PPanel`, `CmdLine`, `Far`, `mf.*` and `bit.*`, reading
  `Macros/scripts` under the config directory. The dialect is far2m's. Macros
  run off the UI goroutine so that they can ask the UI for panel state without
  deadlocking it; `MacroHost` is the seam that enforces this in one place, and
  is what lets the engine be tested without a terminal. Recorded macros keep
  working and take precedence, as in Far.
  Documented for users in `MACROS.md`.
- **Step 4: embedded wasm on wazero.** The guest is a WASI command over stdio,
  so `startPluginSession` in `plughost.go` now holds everything a transport
  does once it has two byte streams, and the wasm transport is just the
  streams. The guest gets no filesystem, making this the first transport that
  is actually a sandbox. `Plugin.Init` gained a timeout along the way: a valid
  but silent module would otherwise hang startup forever, and so would a
  broken subprocess.
- **Step 4b: FFI over the protocol, as `Host.FFI.*`.** The earlier worry about
  guest offsets not being host addresses turned out to be the wrong frame: the
  broker deals only in integers and strings, so it projects onto the existing
  protocol directly. A wasm guest gets real native calls and real C callbacks
  without ever holding a host pointer. Subprocess plugins are not given these
  methods, having no need of them. Zero-copy over guest linear memory remains
  available as a later optimisation for heavy data rather than a precondition.

Next, in order:
- **Step 4c: choosing where `Ctrl+.` records to.** Both macro backends already
  coexist, recorded ones in `key_macros.ini` and scripted ones in
  `Macros/scripts`, with recorded winning a shared key as in Far. What is
  missing is a choice about where a newly recorded macro is stored. The export
  itself is done: `macro_export.go` turns a recorded sequence into a `Macro{}`
  declaration built from `EventToFarString`, names the file `<area>_<key>.lua`
  the way Far does, and is covered by a round trip test that loads the result
  back into the engine and checks it replays the same keys. Writing the file
  into `Macros/scripts` and handing it to the running engine with `LoadString`
  is done too, in `MacroManager.SaveRecordedMacro`, so a macro takes effect in
  the session that recorded it. The remaining cost is
  elsewhere: a configuration option and its place in the settings dialog, and
  deciding what editing or reassigning a recorded macro means once a macro can
  be either kind. Worth doing; not worth rushing.

- **Step 5: onboarding.** A scaffolder for new plugins, a five minute hello
  world, a PlugRing submission walkthrough, and a rewrite of `PLUGINS.md` and
  `LUA.md` to match reality.
- **Step 6: the permission model.** Manifest permissions with the author's own
  justification text, asked for on first real use, remembered in the config.
  Covers FFI, unsafe stdlib and running a native binary. Wires into
  `ffibridge.Options.Allow` and `luaplug.Options.AllowUnsafeStdlib`.
- **Later: a Far3/far2l API compatibility layer.** Explicitly deferred, but the
  namespacing choices above are made so it can arrive without a rewrite.

## Known issues

- A Lua plugin currently gets an FFI bridge with an open permission hook, so it
  can do anything f4 can. This is the alpha tradeoff recorded in decision 10.
- `Host.InputBox` and `Host.Menu` block until the UI answers. A plugin that
  calls them from inside a VFS request served on the UI goroutine will
  deadlock. This predates the embedded transports and affects the subprocess
  one identically.
- A runtime that has hit its call deadline was interrupted at an arbitrary
  instruction and should be discarded rather than reused.
- `Keys()` is not synchronous the way Far's is. Keys queued by a macro are
  injected as one batch once the macro returns, so a macro that inspects panel
  state between two `Keys()` calls sees the state from before either of them.
  Making it synchronous means re-entering the input loop from inside the
  interpreter.
- A macro's `condition` is evaluated after the key has already been consumed,
  because evaluating it on the UI goroutine could deadlock. When a condition
  declines, the original key is replayed, which costs one extra trip through
  the input queue.
- `Event{}`, `MenuItem{}` and `CommandLine{}` declarations are accepted and
  ignored, so that scripts using them still contribute their `Macro{}` entries.
