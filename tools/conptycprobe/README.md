# conptycprobe — Idea C

This is the standalone measurement for alternative C in
`docs/CONPTY_RESEARCH.md`:

> Keep ConPTY very wide (4000 columns), so it never answers the line-wrap
> question; let f4 cut the rows to the real terminal width.

The probe creates its own hidden `cmd.exe` pseudoconsole. It prints two known
ASCII lines, one longer than the detected terminal width and one almost 4000
columns wide. It then changes only the ConPTY height four times and checks the
raw VT stream with a small cursor model. It passes only when every marker is
still present on exactly one virtual row after every repaint. It also prints a
new marker after the resize sequence to prove that the session remains usable.

Nothing is written to the user's console buffer or profile. The probe owns and
terminates its child `cmd.exe`.

Build from the repository root:

```sh
GOOS=windows GOARCH=amd64 go build -o conptycprobe.exe ./tools/conptycprobe
```

Copy `conptycprobe.exe` to Windows and run it from a writable directory:

```text
conptycprobe.exe
```

The default is a 4000-column ConPTY whose height is detected from the terminal
where the probe was started. To make the measurement explicit, use the actual
terminal row count:

```text
conptycprobe.exe -width 4000 -height 30 -log conptyc-22000.log
```

The process prints `PASS` or `FAIL` and writes the raw, escaped VT frames to
the log. `PASS` means only the narrow claim tested here: a wide ConPTY can
deliver these lines intact through height-only repaints. It does not prove that
f4's integration should be implemented, nor that other Windows builds behave
the same way. A nonzero exit status is expected for `FAIL` (exit code 2); send
the complete log when reporting it.
