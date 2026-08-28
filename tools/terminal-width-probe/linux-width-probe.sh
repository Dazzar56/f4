#!/usr/bin/env bash
set -u

LOG="${1:-terminal-width-linux-$(date +%Y%m%d-%H%M%S).log}"
: > "$LOG" || {
    echo "cannot write log: $LOG" >&2
    exit 1
}

# Keep the output visible while preserving a real PTY for commands started by
# script(1). The outer tee does not turn the child's stdout into a pipe.
exec > >(tee -a "$LOG") 2>&1

ROOT=$(mktemp -d "${TMPDIR:-/tmp}/terminal-width-probe.XXXXXX")
trap 'rm -rf "$ROOT"' EXIT
TEST_DIR="$ROOT/files"
GIT_DIR="$ROOT/git"
mkdir -p "$TEST_DIR" "$GIT_DIR"

section() {
    echo "===== $1 ====="
}

line_summary() {
    local file=$1 cols=$2
    LC_ALL=C awk -v cols="$cols" '
        { sub(/\r$/, ""); n=length($0); lines++; if (n > max) max=n; if (n > cols) over++ }
        END { printf "line-count=%d max-line-length=%d lines-over-%d=%d\n", lines, max, cols, over }
    ' "$file"
}

run_pty() {
    local cols=$1 name=$2 command=$3
    local capture="$ROOT/${name}-${cols}.output"
    local status

    section "$name @ ${cols} columns"
    echo "command=$command"
    if command -v script >/dev/null 2>&1; then
        # script(1) gives the command a PTY. Redirecting script's own stdout
        # only captures the resulting bytes; it does not make the child a
        # pipe, so TIOCGWINSZ still reports $cols to the child.
        script -qefc "export COLUMNS=$cols LINES=40; stty cols $cols rows 40 2>/dev/null || true; $command" /dev/null > "$capture" 2>&1
        status=$?
        cat "$capture"
    else
        echo 'script(1) is unavailable; running without a synthetic PTY'
        COLUMNS="$cols" LINES=40 bash -c "$command"
        status=$?
    fi
    echo "exit=$status"
    if [[ -f "$capture" ]]; then
        line_summary "$capture" "$cols"
    fi
}

section META
echo "timestamp=$(date --iso-8601=seconds 2>/dev/null || date)"
echo "host=$(hostname 2>/dev/null || true)"
uname -a || true
echo "tty=$(tty 2>/dev/null || true)"
echo "TERM=${TERM-}"
echo "COLORTERM=${COLORTERM-}"
echo "COLUMNS=${COLUMNS-}"
echo "LINES=${LINES-}"
echo "tput-cols=$(tput cols 2>/dev/null || true)"
echo "tput-lines=$(tput lines 2>/dev/null || true)"
stty -a 2>/dev/null || true
echo "script=$(script --version 2>&1 || true)"
echo "ls=$(ls --version 2>&1 | head -1 || ls -V 2>&1 | head -1 || true)"
echo "git=$(git --version 2>&1 || true)"
if command -v git >/dev/null 2>&1; then
    git config --show-origin --get-regexp '^column\.' 2>&1 || true
fi

section TEST-DATA
for i in $(seq -w 1 120); do
    : > "$TEST_DIR/file-$i-short.txt"
done
for i in $(seq -w 1 20); do
    : > "$TEST_DIR/file-$i-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx.txt"
done
: > "$TEST_DIR/file with spaces 01.txt"
: > "$TEST_DIR/русское-имя-01.txt"
echo "test-directory=$TEST_DIR"
echo "test-file-count=$(find "$TEST_DIR" -maxdepth 1 -type f | wc -l)"

git -C "$GIT_DIR" init -q
git -C "$GIT_DIR" config user.name terminal-width-probe
git -C "$GIT_DIR" config user.email terminal-width-probe@example.invalid
: > "$GIT_DIR/probe.txt"
git -C "$GIT_DIR" add probe.txt
git -C "$GIT_DIR" commit -q -m probe
for i in $(seq -w 1 40); do
    git -C "$GIT_DIR" branch "probe-long-branch-$i-xxxxxxxx"
done
printf 'changed\n' >> "$GIT_DIR/probe.txt"

if ls --color=never "$TEST_DIR" >/dev/null 2>&1; then
    LS_COLOR='--color=never'
else
    LS_COLOR=''
fi

section WIDTH-MATRIX
widths=(80 120 4000)
actual=$(tput cols 2>/dev/null || true)
if [[ "$actual" =~ ^[0-9]+$ && "$actual" -gt 0 ]]; then
    already_present=0
    for cols in "${widths[@]}"; do
        if [[ "$cols" == "$actual" ]]; then
            already_present=1
            break
        fi
    done
    if [[ "$already_present" -eq 0 ]]; then
        widths+=("$actual")
    fi
fi

for cols in "${widths[@]}"; do
    run_pty "$cols" ls-C "ls -C $LS_COLOR '$TEST_DIR'"
    run_pty "$cols" ls-1 "ls -1 $LS_COLOR '$TEST_DIR'"
    run_pty "$cols" git-branch "git -C '$GIT_DIR' branch --column=always"
    run_pty "$cols" git-branch-never "git -C '$GIT_DIR' branch --column=never"
    run_pty "$cols" git-diff-stat "git -C '$GIT_DIR' diff --stat"
    if command -v pwsh >/dev/null 2>&1; then
        run_pty "$cols" pwsh-wide "pwsh -NoLogo -NoProfile -Command \"Get-ChildItem -LiteralPath '$TEST_DIR' | Format-Wide -AutoSize\""
        run_pty "$cols" pwsh-table "pwsh -NoLogo -NoProfile -Command \"Get-ChildItem -LiteralPath '$TEST_DIR' | Format-Table -AutoSize Name,Length,FullName\""
    fi
done

section END
echo "log=$LOG"
echo 'Compare line layout and max-line-length across 80, 120, and 4000 columns.'
echo 'For the key question, focus on ls-C, git-branch, git-diff-stat, and PowerShell formatting.'
