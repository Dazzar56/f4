# f4probe 6

`conptyprobe` is the in-repository Windows probe for the ConPTY, shell-session
and console-image questions tracked in the terminal documentation. One launch
runs the complete host matrix; the person collecting the log does not have to
set environment variables or manually repeat the probe under different hosts.

Build from the repository root:

```sh
GOOS=windows GOARCH=amd64 go build -o f4probe.exe ./tools/conptyprobe
```

Place the resulting `f4probe.exe` beside the Windows `f4.exe` being tested (or
rename that companion to `f4-under-test.exe`) and run `f4probe.exe`. The
controller automatically starts:

- an inbox classic-console run through `conhost.exe -ForceNoHandoff`;
- an explicit Windows Terminal run through `wt.exe`;
- a new-console run with inherited WT markers removed, to exercise the
  configured default-terminal handoff;
- ConPTY runs with flags `0`, `0x2` and `0x8`, exact-width, live, repaint,
  scrollback, cmd title/OSC, batch, nested-cmd and GUI-child scenarios;
- the adjacent real f4 in `off`, `hint`, `oracle` and `probe` reflow modes.

The controller supplies `F4_WIN_REFLOW`, `VTUI_DEBUG` and isolated profile
directories to each f4 child itself. It does not change the user's environment
or configuration. If no companion f4 executable is present, only the f4
integration matrix is marked skipped; the host and standalone ConPTY sections
still run.

The individual logs, manifest and `f4probe-results.zip` are written beside the
probe. The ZIP is the single file to return after a field run. Test consoles
and any probe-owned Notepad processes are closed by the probe; an already
running Notepad is never touched.
