# FISH+ remote helper, protocol version 1.
#
# This script is fed into a plain POSIX shell (usually via an ssh session)
# on the remote host. After initialization it keeps reading commands from
# the very same stdin, one request per line:
#
#   <id> <cmd> [<short arg> ...]
#
# A command that works on a path reads one extra line per path, taken
# verbatim; only a path that cannot survive a line based channel (one that
# contains a newline, or starts with a tilde) is base64 encoded and marked
# with a leading tilde. Replies are zero or more payload lines followed by
# a terminator:
#
#   .<token> <id> ok|err [message]
#
# The token is substituted by the client, so a terminator can never be
# confused with payload produced by ls/stat/dd and friends.
#
# Binary payload travels in frames: a line "#<n>" followed by exactly n
# raw bytes, which is why the line discipline has to be tamed first when
# the shell happens to sit on a pseudo terminal.

F4TOKEN=__F4_TOKEN__
F4PROTO=1

export LANG=C
export LC_ALL=C
export LS_COLORS=
export PS1=
export PS2=
export PS3=
export PS4=
export PROMPT_COMMAND=

f4_end() {
 echo ".$F4TOKEN $F4ID $1 $2"
}

f4_flat() {
 printf '%s' "$1" | tr '\n\r\t' '   '
}

f4_have() {
 command -v "$1" >/dev/null 2>&1 || which "$1" >/dev/null 2>&1
}

f4_num() {
 case $1 in
  '' | *[!0-9]* ) return 1 ;;
 esac
 return 0
}

# A shell started on a pseudo terminal echoes back everything it is fed and
# turns every \n of the payload into \r\n, which destroys binary frames and
# truncates long request lines at the canonical buffer limit. Where there is
# a terminal, put it into a transparent mode; on a plain pipe (the normal
# case) nothing happens. Only POSIX stty operands are used here.
F4TTY=
if [ -t 0 ] && f4_have stty; then
 if stty -echo -icanon -inlcr -icrnl -onlcr -ixon -ixoff min 1 time 0 2>/dev/null; then
  F4TTY=1
 fi
fi

# The trailing newline matters: "openssl base64 -d" silently produces
# nothing for input that does not end with one, which is what made it a
# useless fallback on hosts whose base64 speaks neither -d nor -D. -A lets
# it accept a single long line.
F4DEC=
for f4c in 'base64 -d' 'base64 -D' 'base64 --decode' 'openssl base64 -A -d'; do
 if [ "`printf 'aGk=\n' | $f4c 2>/dev/null`" = hi ]; then
  F4DEC=$f4c
  break
 fi
done

f4_dec() {
 printf '%s\n' "$1" | $F4DEC 2>/dev/null
}

f4_path() {
 IFS= read -r F4PATH || exit
 case $F4PATH in
  '~'* ) F4PATH=`f4_dec "${F4PATH#\~}"` ;;
 esac
}
f4_paths2() {
 f4_path
 F4SRC=$F4PATH
 f4_path
 F4DST=$F4PATH
}

f4_do() {
 F4OUT=$("$@" 2>&1)
 F4RV=$?
 if [ $F4RV -eq 0 ]; then
  f4_end ok
 else
  f4_end err "$(f4_flat "$F4OUT")"
 fi
}

f4_safe_target() {
 case $1 in
  /* ) ;;
  * ) return 1 ;;
 esac
 case $1 in
  '/' | */.. | */../* ) return 1 ;;
 esac
 return 0
}

f4_guard() {
 if f4_safe_target "$1"; then
  return 0
 fi
 f4_end err "unsafe path: must be absolute and free of .. components"
 return 1
}

f4_rm() {
 f4_guard "$F4PATH" && f4_do "$@" -- "$F4PATH"
}

F4FMT_FIND='%y %Y %s %T@ %A@ %C@ %m %U %G %f\n'
F4FMT_STAT='%f %s %Y %X %Z %u %g %n'
F4FMT_BSD='%p %z %m %a %c %u %g %N'

f4_try_mode() {
 case $1 in
  find ) find -H . -mindepth 0 -maxdepth 0 -printf "$F4FMT_FIND" >/dev/null 2>&1 ;;
  stat ) stat -c "$F4FMT_STAT" . >/dev/null 2>&1 ;;
  statbsd ) stat -f "$F4FMT_BSD" . >/dev/null 2>&1 ;;
  * ) false ;;
 esac
}

F4MODE=
for f4c in find stat statbsd; do
 if f4_try_mode $f4c; then
  F4MODE=$f4c
  break
 fi
done

# How "head -c" behaves decides more than it looks: on macOS it swallows the
# whole pipe instead of stopping after n bytes, so it can never be used on a
# stream someone else still has to read from. One probe answers both
# questions, whether -c is supported at all and whether it stops in time.
F4HEADC=
F4HEADSAFE=
if f4_have head; then
 case "`printf 12345 | { head -c 2 2>/dev/null; printf '|'; cat; }`" in
  '12|345' ) F4HEADC=1; F4HEADSAFE=1 ;;
  '12|' ) F4HEADC=1 ;;
 esac
fi

F4TAILC=
if f4_have tail && [ "`printf 12345 | tail -c +3 2>/dev/null`" = 345 ]; then
 F4TAILC=1
fi

f4_try_rmode() {
 case $1 in
  ddbytes ) f4_have dd && dd if=/dev/null of=/dev/null bs=1 count=0 iflag=skip_bytes,count_bytes 2>/dev/null ;;
  dd ) f4_have dd && dd if=/dev/null of=/dev/null count=0 2>/dev/null ;;
  tailc ) [ -n "$F4TAILC" ] && [ -n "$F4HEADC" ] ;;
  cat ) f4_have cat ;;
  * ) false ;;
 esac
}

F4RD=
for f4c in ddbytes dd tailc cat; do
 if f4_try_rmode $f4c; then
  F4RD=$f4c
  break
 fi
done

F4FEATS=
for f4c in dd base64 readlink du grep sed awk wc head tail stty sha256sum; do
 f4_have $f4c && F4FEATS="$F4FEATS $f4c"
done
for f4c in find stat statbsd; do
 f4_try_mode $f4c && F4FEATS="$F4FEATS $f4c"
done
[ -n "$F4HEADC" ] && F4FEATS="$F4FEATS headc"
[ -n "$F4HEADSAFE" ] && F4FEATS="$F4FEATS headsafe"
[ -n "$F4TAILC" ] && F4FEATS="$F4FEATS tailc"
f4_try_rmode ddbytes && F4FEATS="$F4FEATS ddbytes"
[ -n "$F4TTY" ] && F4FEATS="$F4FEATS tty"
[ -n "$F4MODE" ] && F4FEATS="$F4FEATS mode:$F4MODE"
[ -n "$F4RD" ] && F4FEATS="$F4FEATS read:$F4RD"

f4_list() {
 case $F4MODE in
  find ) find -H "$F4PATH" -mindepth 1 -maxdepth 1 -printf "$F4FMT_FIND" 2>/dev/null ;;
  stat ) stat -c "$F4FMT_STAT" -- "$F4PATH"/* "$F4PATH"/.* 2>/dev/null ;;
  statbsd ) stat -f "$F4FMT_BSD" -- "$F4PATH"/* "$F4PATH"/.* 2>/dev/null ;;
 esac
}

# f4_size follows symlinks, like the read command it serves, and falls back
# to counting bytes when there is no metadata backend at all.
f4_size() {
 F4SZ=
 case $F4MODE in
  find ) F4SZ=`find -H "$1" -mindepth 0 -maxdepth 0 -printf '%s\n' 2>/dev/null` ;;
  stat ) F4SZ=`stat -c '%s' -- "$1" 2>/dev/null` ;;
  statbsd ) F4SZ=`stat -f '%z' -- "$1" 2>/dev/null` ;;
 esac
 f4_num "$F4SZ" || F4SZ=
 if [ -z "$F4SZ" ] && f4_have wc; then
  F4SZ=`wc -c < "$1" 2>/dev/null | tr -d ' '`
  f4_num "$F4SZ" || F4SZ=
 fi
}

# The largest power of two up to 64k that divides both the offset and the
# length. It lets plain dd position itself with one lseek and read whole
# blocks, without the GNU only iflag=skip_bytes; a client that asks for
# aligned chunks therefore gets full speed on every host.
f4_bs() {
 F4BS=65536
 while [ "$F4BS" -gt 1 ]; do
  if [ $(( $1 % F4BS )) -eq 0 ] && [ $(( $2 % F4BS )) -eq 0 ]; then
   break
  fi
  F4BS=$(( F4BS / 2 ))
 done
}

f4_read_range() {
 case $F4RD in
  ddbytes )
   dd if="$1" bs=1048576 iflag=skip_bytes,count_bytes skip="$2" count="$3" 2>/dev/null
   ;;
  dd )
   f4_bs "$2" "$3"
   dd if="$1" bs=$F4BS skip=$(( $2 / F4BS )) count=$(( $3 / F4BS )) 2>/dev/null
   ;;
  tailc )
   tail -c +$(( $2 + 1 )) < "$1" 2>/dev/null | head -c "$3" 2>/dev/null
   ;;
  cat )
   cat < "$1" 2>/dev/null
   ;;
 esac
}

f4_cmd_enum() {
 f4_path
 if [ -z "$F4MODE" ]; then
  f4_end err "no supported listing tool on remote host"
  return
 fi
 if [ ! -d "$F4PATH" ]; then
  f4_end err "not a directory"
  return
 fi
 if [ ! -r "$F4PATH" ] || [ ! -x "$F4PATH" ]; then
  f4_end err "permission denied"
  return
 fi
 echo "M $F4MODE"
 f4_list
 f4_end ok
}

f4_cmd_info() {
 f4_path
 if [ -z "$F4MODE" ]; then
  f4_end err "no supported stat tool on remote host"
  return
 fi
 case $F4MODE in
  find ) F4OUT=`find $1 "$F4PATH" -mindepth 0 -maxdepth 0 -printf "$F4FMT_FIND" 2>&1`; F4RV=$? ;;
  stat ) F4OUT=`stat $2 -c "$F4FMT_STAT" -- "$F4PATH" 2>&1`; F4RV=$? ;;
  statbsd ) F4OUT=`stat $2 -f "$F4FMT_BSD" -- "$F4PATH" 2>&1`; F4RV=$? ;;
 esac
 if [ $F4RV -eq 0 ] && [ -n "$F4OUT" ]; then
  echo "M $F4MODE"
  echo "$F4OUT"
  f4_end ok
 else
  f4_end err "$(f4_flat "$F4OUT")"
 fi
}

f4_cmd_rdlink() {
 f4_path
 if f4_have readlink; then
  F4OUT=`readlink -- "$F4PATH" 2>&1`
  F4RV=$?
 elif f4_have sed; then
  F4OUT=`ls -ld -- "$F4PATH" 2>&1 | sed -n 's/^.* -> //p'`
  F4RV=$?
 else
  f4_end err "no way to read symlinks on remote host"
  return
 fi
 if [ $F4RV -eq 0 ] && [ -n "$F4OUT" ]; then
  echo "$F4OUT"
  f4_end ok
 else
  f4_end err "$(f4_flat "$F4OUT")"
 fi
}

# read <offset> <length>, length 0 meaning "to the end of the file".
# The reply carries the size the helper saw as a text line, then one binary
# frame with the bytes that were actually available in that range.
f4_cmd_read() {
 f4_path
 if ! f4_num "$1" || ! f4_num "$2"; then
  f4_end err "bad range"
  return
 fi
 if [ -z "$F4RD" ]; then
  f4_end err "no way to read files on remote host"
  return
 fi
 if [ ! -e "$F4PATH" ]; then
  f4_end err "no such file"
  return
 fi
 if [ -d "$F4PATH" ]; then
  f4_end err "is a directory"
  return
 fi
 if [ ! -r "$F4PATH" ]; then
  f4_end err "permission denied"
  return
 fi
 f4_size "$F4PATH"
 if [ -z "$F4SZ" ]; then
  f4_end err "cannot determine file size"
  return
 fi
 F4N=0
 if [ "$1" -lt "$F4SZ" ]; then
  F4N=$(( F4SZ - $1 ))
  if [ "$2" -gt 0 ] && [ "$2" -lt "$F4N" ]; then
   F4N=$2
  fi
 fi
 if [ "$F4RD" = cat ] && [ "$F4N" -gt 0 ] && { [ "$1" -ne 0 ] || [ "$F4N" -ne "$F4SZ" ]; }; then
  f4_end err "no way to read a byte range on remote host"
  return
 fi
 echo "S $F4SZ"
 echo "#$F4N"
 if [ "$F4N" -gt 0 ]; then
  f4_read_range "$F4PATH" "$1" "$F4N"
 fi
 f4_end ok
}

f4_cmd_mode() {
 if f4_try_mode "$1"; then
  F4MODE=$1
  f4_end ok
 else
  f4_end err "mode not available"
 fi
}

f4_cmd_rmode() {
 if f4_try_rmode "$1"; then
  F4RD=$1
  f4_end ok
 else
  f4_end err "read mode not available"
 fi
}

F4ID=0
# A login banner or the echo of this very script may end without a newline,
# and the terminator has to start a line of its own to be recognizable.
printf '\n'
if [ -n "$F4DEC" ]; then
 f4_end ok "FISHPLUS $F4PROTO$F4FEATS"
else
 f4_end err "no base64 decoder found on remote host"
fi

while :; do
 IFS=' ' read -r F4ID F4CMD F4A1 F4A2 F4A3 || break
 [ -n "$F4ID" ] || continue
 case "$F4CMD" in
  noop )
   f4_end ok
   ;;
  pwd )
   pwd
   f4_end ok
   ;;
  ping )
   f4_path
   printf '%s\n' "$F4PATH"
   f4_end ok
   ;;
  enum )
   f4_cmd_enum
   ;;
  info )
   f4_cmd_info -H -L
   ;;
  linfo )
   f4_cmd_info -P ''
   ;;
  rdlink )
   f4_cmd_rdlink
   ;;
  mkdir )
   f4_path
   f4_guard "$F4PATH" && f4_do mkdir -p -- "$F4PATH"
   ;;
  rm )
   f4_path
   f4_rm rm -f
   ;;
  rmdir )
   f4_path
   f4_rm rmdir
   ;;
  rmtree )
   f4_path
   f4_rm rm -rf
   ;;
  mv )
   f4_paths2
   f4_guard "$F4SRC" && f4_guard "$F4DST" && f4_do mv -f -- "$F4SRC" "$F4DST"
   ;;
  chmod )
   f4_path
   case $F4A1 in
    '' | *[!0-7]* ) f4_end err "bad mode" ;;
    * ) f4_guard "$F4PATH" && f4_do chmod -- "$F4A1" "$F4PATH" ;;
   esac
   ;;
  read )
   f4_cmd_read "$F4A1" "$F4A2"
   ;;
  mode )
   f4_cmd_mode "$F4A1"
   ;;
  rmode )
   f4_cmd_rmode "$F4A1"
   ;;
  feats )
   echo "$F4PROTO$F4FEATS"
   f4_end ok
   ;;
  exit )
   f4_end ok
   break
   ;;
  * )
   f4_end err "unknown command"
   ;;
 esac
done
