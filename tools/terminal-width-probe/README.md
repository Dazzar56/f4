# Terminal-width probe

These scripts compare human-oriented command output at normal and artificially
wide terminal sizes. They are intended to be run on a real host, especially
inside the f4 ConPTY implementation.

## Windows

Run `windows-width-probe.bat` from the directory containing the two Windows
files. It starts PowerShell with a transcript, so the commands keep the
current console/ConPTY as their stdout while their output is recorded:

```text
windows-width-probe.bat
```

The log contains `mode con`, PowerShell `RawUI`/`Console` dimensions,
`cmd.exe dir /w`, `dir /d`, `dir /b`, and PowerShell table/wide formatting.
Run it once in an ordinary Windows terminal and once from inside f4.

## Linux

Run:

```sh
bash linux-width-probe.sh terminal-width-linux.log
```

The script uses `script(1)` to give each command a real PTY and changes that
PTY to 80, 120, and 4000 columns. It tests GNU/BSD `ls`, Git column output,
Git diff statistics, and PowerShell when `pwsh` is installed. `script(1)` is
normally provided by util-linux; without it the script falls back to ordinary
execution and says so in the log.
