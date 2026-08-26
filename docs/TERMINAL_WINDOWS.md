# Terminal under Windows — umbrella analysis (issue #425)

Companion to `TERMINAL.md`, which describes the architecture, and to
`TERMINAL_REFLOW.md`, which surveys how other terminals reflow text and what
`f4` should copy from them. This file
describes what is currently broken in it and in what order it is meant to be
fixed. It covers issues #165, #362, #409 and the terminal half of #424.

## 0. The shared root cause

f4 drives `cmd.exe` by typing service text into the visible shell session and
then trying to un-type it with a textual excision in the parser. That cannot be
made reliable on ConPTY, which (see `TERMINAL.md`, Appendix A) echoes
everything written to its input, fragments output at arbitrary offsets, and
prints the next prompt before the finished command is fully drained.

## 1. Where the code lives

| Subject | Location |
| --- | --- |
| cwd sync into the shell | `panels_frame.go`, `syncShellPath` area (`rem f4_sync`) |
| Excision of the echoed sync command | `ansi_parser.go`, top of `AnsiParser.Process` |
| Command started from the command line | `panels_frame.go`, Enter handling |
| File started with Enter on the panel | `actions.go`, `actionExecute` |
| `PROMPT=$E]133;D$E\$P$G` | `panels_frame.go`, `initPTY` |
| OSC 133 handling | `terminal_view.go`, `HandleOSC133` |
| Busy state | `panels_frame.go`, `isPtyBusy`; `pty_windows.go`, `PTY.IsBusy` |
| Key to PTY byte translation | `input_translation.go`, `TranslateInput` |
| Key events in GUI mode | `vtui/gogpu_host.go` |
| Ctrl+C / Ctrl+Break | `console_ctrl_handler_windows.go` |
| Workspace clone | `panels_frame.go`, `PanelsFrame.Clone`; `terminal_view.go`, `CloneStateFrom` |

## 2. Issue #165 — output scrolls up, sync command becomes visible

Changing the panel directory writes `cd /d "<path>" & rem f4_sync` into the
PTY. The parser excises the echo and writes `\r\x1b[2K` in its place.

Two independent defects:

1. The excision erases the text of the line but not the newline that already
   happened, nor the prompt reprinted after it. Every directory change costs at
   least one line, so the terminal creeps upwards. No textual excision can fix
   this.
2. The excision runs on a single `Read` chunk with no carry buffer. ConPTY
   splits output arbitrarily, so a long enough path pushes the marker over a
   chunk boundary, `bytes.Index` misses, and the user sees the raw command.
   This is why the reporter saw it appear "at some nesting depth".

Additionally the fallback branch that excises `cd /d "..." & ` without the
`rem f4_sync` marker also mangles a `cd /d "X" & dir` a human typed.

Fix, in order of increasing cost:

* Stop sending the sync on a plain directory change. The working directory is
  imposed anyway when a command is launched (`cd /d "%s" & %s`), so nothing is
  lost, and the whole class of symptoms disappears.
* Later: track the shell's cwd from the shell (OSC 7 / OSC 9;9) instead of
  imposing it, and suppress echo by position against what we wrote, with a
  carry buffer across chunks.

## 3. Issue #409 — bat/cmd appear to run in the background

Root cause, established from cmd.exe's documented behaviour: with ECHO on
(the default), cmd prints the *prompt* in front of every line of a batch file
it runs. f4's injected `PROMPT` therefore emitted its OSC 133 "command done"
mark on the first line of `foo.bat`, `pf.executing` was cleared, and
`PTY.IsBusy()` could not object because cmd interprets batch files
in-process and creates no child. The old integration test used `@echo off`
plus `ping.exe` (a child), which is exactly the combination that never
reproduces it.

Fix: `cmd_session.go`. `PROMPT` now marks the prompt start (`133;A`) and end
(`133;B`, printed after `$P$G`); no mark claims completion. A typed line is
finished only when a prompt end mark has arrived *after* the line was typed,
the cursor has rested right after that prompt's text for
`cmdPromptSettleDelay` (150 ms — an echoed batch line is followed by the
command text and a line break within a frame, a real prompt by nothing), the
shell has no child process (a nested `cmd`/`python` prints the same prompt),
and the console title is not in cmd's "<title> - <command>" running form,
which ConPTY forwards. A vetoed prompt is re-examined every
`cmdPromptRecheckDelay`, so a nested shell that exits later still hands the
panels back. The directory sync goes through the same accounting: `Show()`
does not type a second sync while the previous line has no settled prompt.

### 3.1. Field results, 2026-08-26 (commit 49d388b)

Tested on Windows 10 19045 against the checklist in #425.

**Passing:** a batch file with ECHO on no longer returns the panels early, and
neither does one that ends in `pause`; ordinary commands, `notepad` and
directory navigation behave. So the settled-prompt rule does what it was meant
to do — an echoed batch prompt no longer forges completion.

**Failing — nested `cmd` deadlocks the terminal.** Type `cmd`, and from then on
Enter does nothing; `dir` cannot be run and the session never comes back. The
log shows the mechanism exactly:

    CMD_SESSION: prompt 3 settled (sent=2 pending=true)
    CMD_SESSION: prompt 3 vetoed (child or title), rechecking

repeating every 250 ms forever, while the frame stack stays on
`Terminal (executing)`.

The child-process veto is the culprit. A nested `cmd` *is* a child of the outer
`cmd`, so `IsBusy()` is true for as long as the user stays inside it — which is
the whole point of running it. The veto was written for a child that finishes on
its own (`ping`, a compiler), where re-checking is right; for an interactive
child it is a permanent no. Meanwhile `pf.executing` stays true, so keystrokes
keep going down the executing path and Enter never reaches the nested shell's
input.

The fix has to distinguish "a child is running and owns the terminal" from
"a child is running and f4 should wait": an interactive child *is* the shell now,
and its prompt is the one that matters. The outer prompt cannot tell them apart —
the nested `cmd` prints the same `$P$G`, and it inherits our injected `PROMPT`,
so it emits our marks too.

Two candidate signals were considered; a process probe run by a tester
(`f4-probe.ps1`, Windows 11 26200) decided between them:

* **Console title** — dead. cmd's "<title> - <command>" form is not readable
  from outside: the conhost behind a pseudoconsole has no window, so the
  title column came back empty for every process, running or not. The title
  veto in `cmdShellSession.titleSaysRunning` can never fire and should go.
* **Image name of the child** — works. While the nested shell sat at its
  prompt, the child of f4's cmd was `cmd.exe`; while real commands ran it was
  `PING.EXE` and `timeout.exe`. So: keep the child veto, but do not apply it
  when the child is itself a shell (`cmd.exe`, `powershell.exe`, `pwsh.exe`,
  and the interactive interpreters people nest — `python.exe`, `wsl.exe`,
  `bash.exe`). For those, the settled prompt is the nested shell's own and
  ends the outer command's wait: keystrokes then go to the nested shell as
  they do to any busy child, and its `exit` brings the outer prompt back,
  which settles in turn.

Also observed by the tester: `Ctrl+O` while stuck brings the panels back and
input works again. That confirms the diagnosis — nothing is wrong with the
input path, only with `executing` never clearing.

Implemented in `cmd_session.go`: `childHoldsTerminal` ignores children in
`nestedShellImages` (only `cmd.exe` — PowerShell rejects `cd /d`, so it must
stay a raw-mode child like ssh or python) and ignores GUI children, whose
subsystem is read from the PE header of the image (`pe_subsystem.go`,
`PTY.ChildProcesses`). cmd does not wait for a GUI program, so `notepad` typed
at the prompt leaves cmd idle; the second field report confirmed that it was
only the five-second fallback (§3.3) that had been returning the panels in
that case. A console child that is not a shell holds the terminal for as long
as it runs — that wait is deliberately unbounded, `ping -t` is forever.

Open: with the panels hidden and a nested `cmd` at its prompt, Enter typed in
raw mode reportedly did nothing while `dir` typed after `Ctrl+O` (through f4's
command line) worked. The fix above sidesteps it by returning the panels at
the nested prompt; why raw-mode Enter fails there has not been explained.

Also: the local PTY read loop ending (shell died) ends any execution instead
of leaving the panels hidden, and `PTY.SetSize` ignores 0x0 (TERMINAL.md rule
4 was documented but not enforced).

Not done yet, in the same session layer: making the directory sync creep-free
by typing a short self-erasing `echo %F4E%[nA%F4E%[J...` cleanup line after
the new prompt arrives (F4E holding ESC, like `$E` in PROMPT), so that the
erase happens in conhost's own buffer and survives ConPTY repaints; and
reading the shell's cwd back from the text between the A and B marks. A
tester confirmed the visible `cd /d "C:\F4" & rem f4_sync` now appears only
once, at startup — the first sync goes out before the first prompt has
settled, which is the one case the session accounting cannot yet hide.

### 3.2. The stranded wait, and why it looked like "Esc is broken"

Reported after 3.1: commands finish, but afterwards `Ctrl+O` works while `Esc`
does not.

That asymmetry names the cause precisely. `Esc` is bound with the `EscToggle`
condition, which ends in `!pf.isPtyBusy()`; `Ctrl+O` is bound with
`NoAltScreenApp`, which does not look at busy at all. `isPtyBusy` returns
`pf.executing` when no child process is running, so anything that leaves
`executing` set disables `Esc` and everything else gated on a quiet terminal,
while leaving `Ctrl+O` alive.

What left it set: `cmdShellSession.settle` checked that the cursor still rested
on the prompt and, if it did not, returned — with no timer and no retry. The
check runs 150 ms after the mark arrives, and in that window the parser may
well have moved the cursor: it excises the sync command from the stream, and a
ConPTY repaint rewrites the screen wholesale. One transient mismatch and the
session waited forever.

Now the check retries, and after `cmdPromptMaxAttempts` it *releases* the wait
instead of holding it. The direction matters: a terminal that calls a command
finished slightly too early is a smaller problem than one the user cannot get
out of. Any future veto added to this path must fail the same way.

Note also that `titleSaysRunning` can never fire (§3.1: the title is unreadable
behind a pseudoconsole), so the title veto is now dead weight in this code.

### 3.3. Five seconds on every `dir`: the mark arrives before its prompt

Second field report (10.0.19045): every command, `dir` included, brought the
panels back only after 5–6 seconds. Users read that as "the panels are not
coming back" and started pressing keys.

Five seconds is `cmdPromptMaxAttempts × cmdPromptRecheckDelay` — the release
fallback from §3.2. So the settle check itself was failing on every plain
command and the fallback was doing all the work. The check compared the screen
against a snapshot taken when the prompt-end mark was parsed, and on this build
that snapshot is empty: ConPTY passes the OSC 133 mark through to the pipe
*before* it has rendered the prompt text that precedes it in cmd's output, so
at the mark the cursor sits at the start of an empty row and the text
`C:\work>` arrives in a later read. On 10.0.26200 the first tester's commands
ended at once, so there the text comes first. Both orders are now in the test
model (`windowsBuilds` in `cmd_session_test.go`) and every session test runs
under each.

Fix: the snapshot is gone. `settle` looks at what is in front of the cursor
*now*, requires it to be prompt-shaped (`promptShaped`: a drive path then `>`,
which an echoed batch line, a `set /p` prompt and program output that ends in
`>` all fail), and requires it to be unchanged since the previous look. On the
late-text build that costs one extra re-look, about 400 ms in total instead of
five seconds.

The title veto was removed in the same change: §3.1 showed the title is
unreadable behind a pseudoconsole, and `cmdShellSession.titleSaysRunning` could
never have fired.

## 5. Plan, in order

| # | Step | Status |
| --- | --- | --- |
| 1 | Settled-prompt completion (#409) | shipped, field-tested |
| 2 | Nested shell: skip the child veto for `cmd.exe`; remove the dead title veto | shipped |
| 3 | GUI children (`notepad`) do not count as busy | shipped (PE subsystem) |
| 4 | Examine the screen at settle time, not a snapshot at the mark (§3.3) | shipped |
| 5 | Self-erasing directory sync cleanup (section 3) | next |
| 6 | Startup sync typed before the first prompt settles | with 5 |
| 7 | ConPTY reflow experiments (`TERMINAL_REFLOW.md` §3) | Go probe built, results pending |

Steps 2–4 need a field run: expected is `dir` ending well under a second on
19045, `notepad` and nested `cmd` returning the panels at once, and `ping -t`
holding them until Ctrl+C. The PowerShell probe for step 7 failed twice on
P/Invoke details (`err=87` for both flags, i.e. before the flag mattered);
`tools/conptyprobe` is the same experiment written with the calls f4 itself
uses, cross-compiled here.

## 4. Issue #362 — Ctrl+C does not interrupt in f4-gui

`TranslateInput` only produces a control byte when `e.Char != 0`; with
`Char == 0` it falls through and returns an empty string, so nothing is
written to the PTY.

In the console build the Windows console API delivers Ctrl+C with
`UnicodeChar == 0x03`, so it works — matching the report that the tty version
is fine. In GUI mode `vtui/gogpu_host.go` treats any key held with Ctrl as a
"special or modified" key and emits it immediately with `Char == 0`, and no
text-input event follows for a control combination.

The gap covers the whole family: Ctrl+C, Ctrl+D, Ctrl+Z, Ctrl+\, Ctrl+], Ctrl+@.

Fix: in `TranslateInput`, when Ctrl is held and `Char == 0`, map the virtual
key code (`'A'`–`'Z'` to 1–26, `VK_OEM_4/5/6` to 27/28/29, `VK_6` to 30,
`VK_OEM_MINUS` to 31, `VK_2`/`VK_SPACE` to 0). Check whether the x11 and
wayland hosts behave the same way.

Ctrl+Break closing f4 (from the same issue's comment) is not reproduced from
the code: `console_ctrl_handler_windows.go` swallows both events and is
installed from `main`, i.e. after the Go runtime's own handler, and Windows
calls handlers in reverse order of registration. Log the entry of
`consoleCtrlHandlerRoutine` and re-test both builds before assuming anything.

## 5. Issue #424 — the terminal half

This was fixed: `TerminalView.CloneStateFrom` copies terminal display state but
leaves `pty` owned by the destination `PanelsFrame`. The clone's own
`initPTY` goroutine therefore cannot race with a source workspace's PTY, which
was the mechanism behind "a command run in one workspace shows up in another".

The routing half of #424 (actions landing in the wrong workspace) was a
separate defect in `findPanelsFrameAnyScreen` and is covered by the active-
workspace routing tests.

Archive and FTP VFS clones now also keep independent path state and lifecycle;
their regressions are covered in the corresponding plugin tests.

## 6. Target shape

One shell-session object per PTY owning: the working directory (read from the
shell, imposed only on launch), the construction of the wire command with OSC
133 framing identical on both platforms, echo suppression matched against what
we wrote with a carry buffer, and the busy state derived from `C`/`D` parity.
Today the command construction is duplicated between `panels_frame.go` and
`actions.go` and differs between platforms.

## 7. Order of work

| # | Task | Issue | Risk |
| --- | --- | --- | --- |
| 1 | VK fallback for Ctrl+letter in `TranslateInput` | #362 | low |
| 2 | Drop the cwd sync on directory change | #165 | low |
| 3 | OSC 133 parity gate | #409 | low |
| 4 | Stop copying `pty` in `CloneStateFrom` | #424 | done |
| 5 | Logging patch for OSC 133 | #409 | none |
| 6 | Frame Windows commands with `%F4E%` markers | #409 | medium |
| 7 | `IsBusy` from the pseudoconsole process list | #409 | medium |
| 8 | Shell-session layer, echo suppression with carry buffer | all | high |
| 9 | Diagnose Ctrl+Break | #362 | none |

## 8. Still missing

* A debug log for the #409 scenario, after step 5.
* Confirmation that the Ctrl+Break report still reproduces on master, and in
  which build.
* A decision on whether Ctrl+N should start a new shell or share one, which
  determines the shape of the remaining terminal work in section 5.
