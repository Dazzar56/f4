
#### f4 (a far2l clone in go)

**The Core:** Creating a modern cross-platform TUI (Terminal User Interface) file manager that fully replicates the features, UX, data structures, and rendering logic of `far2l`, but implemented in Go.

**Why Go?**
1. High development speed and perfect compatibility with LLM-based code generation.
2. Native concurrency (goroutines) for I/O operations (ensuring no UI freezes while calculating folder sizes).
3. Performance: Thanks to a zero-allocation approach in the rendering loop, Garbage Collector (GC) pauses are kept below 0.5ms, making them unnoticeable compared to terminal I/O latency.

**Plugin Architecture (Hybrid In-Process):**
Total rejection of JSON-RPC due to input lag. Plugins will run within the same address space or host memory:
1. **Lua (`gopher-lua`):** For fast macros, scripting, and UI customization.
2. **WASM (`wazero`):** For heavy system plugins (archivers, VFS, parsers). Provides 100% portability (a single `.wasm` file for Linux, macOS, and Windows) and security (sandboxing).

---

#### vtui Architecture (UI Framework)

We are not using existing UI frameworks. We are building our own `vtui` framework around data structures similar to those used in `far2l`. This provides a familiar environment for C++ version developers and allows for easy algorithm porting.

**1. Base Types (Win32/far2l Port):**
We will use Go equivalents:
```go
type Coord struct { X, Y int16 }
type SmallRect struct { Left, Top, Right, Bottom int16 }

// far2l uses DWORD64 for attributes (including RGB) and COMP_CHAR
type CharInfo struct {
    Char       uint64 // Corresponds to union (WCHAR/COMP_CHAR)
    Attributes uint64 // Corresponds to DWORD64 Attributes
}
```

**2. Attribute Concept (from `far2l`):**
*   Lower 16 bits: Classic Win32 colors (FOREGROUND_BLUE, etc.) and flags.
*   Bits 16-39: 24-bit RGB text color (`FOREGROUND_TRUECOLOR`).
*   Bits 40-63: 24-bit RGB background color (`BACKGROUND_TRUECOLOR`).

**3. Virtual Screen (ScreenBuf):**
A precise analog of `ScreenBuf` from `scrbuf.cpp`.
*   **Double Buffering:** Contains `Buf` (current logic state) and `Shadow` (what is currently physically on the terminal screen).
*   **Diff & Flush:** The `Flush()` method compares `Buf` and `Shadow`. When differences are found, it generates and outputs the minimum necessary set of ANSI escape sequences (cursor positioning, color changes, character output) to `stdout`, then copies the changes to `Shadow`. Rendering is performed without allocations.

**4. Object Hierarchy (ScreenObject):**
A base interface/struct `ScreenObject` (analogous to `scrobj.hpp`), implemented by all UI elements (Panels, Dialogs, Viewer).
*   Properties: `X1, Y1, X2, Y2`, Visibility flags.
*   Methods: `Show()`, `Hide()`, `ProcessKey()`, `ProcessMouse()`.

---

#### Roadmap

**Phase 1: Foundation (`vtui` package)**
1. Port `CharInfo`, `Coord`, `SmallRect` structures, color constants, and color-handling macros (GET_RGB_FORE, SET_RGB_FORE, etc.).
2. Create the `ScreenBuf` class (struct) with `AllocBuf`, `Write(x, y, text)`, and `ApplyColor` methods.
3. Write the `Flush()` algorithm that translates the difference between `Buf` and `Shadow` into raw VT (ANSI) sequences and outputs them to the terminal.
4. Integrate `vtinput` into a test loop that renders keyboard/mouse reactions onto the `ScreenBuf`.

**Phase 2: Base UI Primitives (`vtui/views` package)**
1. Base `ScreenObject` (coordinate management, focus, background preservation via `SaveScreen`).
2. `Frame/Box` (border rendering).
3. `VMenu` (vertical menu) with scrolling and mouse support.

**Phase 3: f4 Core Preparation (`f4` package)**

---

Project folder structure: `f4_project` -> `f4`, `vtui`, `vtinput`.
Development is carried out via small patches in the `ap` format (https://github.com/unxed/ap).

