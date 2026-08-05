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

f4_end_rv() {
 if [ "$1" -eq 0 ]; then
  f4_end ok
 else
  f4_end err "$(f4_flat "$2")"
 fi
}

f4_do() {
 F4OUT=$("$@" 2>&1)
 f4_end_rv $? "$F4OUT"
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
# Writing needs the opposite of reading: a consumer that stops after exactly
# n bytes, because whatever it leaves in the stream is read as the next
# request. Only GNU dd stops on a byte boundary while still reading full
# blocks (iflag=fullblock,count_bytes); a plain dd is exact only with bs=1,
# which costs a syscall per byte, so elsewhere base64 -- consumed by the
# shell itself with read, and therefore exact by construction -- is the
# faster of the two even with a third more traffic on the wire.
f4_try_wmode() {
 case $1 in
  ddbytes ) f4_have dd && dd if=/dev/null of=/dev/null bs=1 count=0 iflag=fullblock,count_bytes oflag=seek_bytes 2>/dev/null ;;
  b64 ) f4_have dd && [ -n "$F4DEC" ] ;;
  ddbs1 ) f4_have dd ;;
  * ) false ;;
 esac
}

F4WR=
for f4c in ddbytes b64; do
 if f4_try_wmode $f4c; then
  F4WR=$f4c
  break
 fi
done

F4FEATS=
for f4c in dd base64 readlink du grep sed awk wc head tail stty truncate chown touch date sha256sum; do
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
[ -n "$F4WR" ] && F4FEATS="$F4FEATS write:$F4WR"

f4_list() {
 case $F4MODE in
  find ) find -H "$F4PATH" -mindepth 1 -maxdepth 1 -printf "$F4FMT_FIND" 2>/dev/null ;;
  stat ) stat -c "$F4FMT_STAT" -- "$F4PATH"/* "$F4PATH"/.* 2>/dev/null ;;
  statbsd ) stat -f "$F4FMT_BSD" -- "$F4PATH"/* "$F4PATH"/.* 2>/dev/null ;;
 esac
}
# ffind <limit> <nmasks> <grep mode>: walk a whole tree on this side and
# report every file whose name matches one of the masks, in the same format
# a listing uses, with the full path in place of the name. With a grep mode
# other than "-" a pattern line follows the masks and only files containing
# it are reported, so one request replaces a directory round trip per level
# plus a download per candidate.
F4FMT_FINDP='%y %Y %s %T@ %A@ %C@ %m %U %G %p\n'

f4_cmd_ffind() {
 f4_lim=$1
 f4_nm=$2
 f4_gm=$3
 f4_path
 f4_dir=$F4PATH
 if [ -z "$F4MODE" ]; then
  f4_end err "no supported listing tool on remote host"
  return
 fi
 if ! f4_num "$f4_lim" || ! f4_num "$f4_nm" || [ "$f4_nm" -lt 1 ]; then
  f4_end err "bad search request"
  return
 fi
 set --
 f4_i=0
 while [ "$f4_i" -lt "$f4_nm" ]; do
  f4_path
  if [ "$f4_i" -eq 0 ]; then
   set -- -name "$F4PATH"
  else
   set -- "$@" -o -name "$F4PATH"
  fi
  f4_i=$(( f4_i + 1 ))
 done
 f4_pat=
 f4_go=
 case $f4_gm in
  - ) ;;
  *[!fie]* ) f4_end err "bad grep mode"; return ;;
  * )
   f4_path
   f4_pat=$F4PATH
   if [ -z "$f4_pat" ]; then
    f4_end err "empty search pattern"
    return
   fi
   if ! f4_have grep; then
    f4_end err "no grep on remote host"
    return
   fi
   case $f4_gm in *f* ) f4_go="$f4_go -F" ;; esac
   case $f4_gm in *i* ) f4_go="$f4_go -i" ;; esac
   ;;
 esac
 if [ ! -d "$f4_dir" ]; then
  f4_end err "not a directory"
  return
 fi
 echo "M $F4MODE"
 if [ -n "$f4_pat" ]; then
  case $F4MODE in
   find ) find -H "$f4_dir" ! -type d \( "$@" \) -exec grep $f4_go -q -e "$f4_pat" {} \; -printf "$F4FMT_FINDP" 2>/dev/null ;;
   stat ) find -H "$f4_dir" ! -type d \( "$@" \) -exec grep $f4_go -q -e "$f4_pat" {} \; -exec stat -c "$F4FMT_STAT" -- {} + 2>/dev/null ;;
   statbsd ) find -H "$f4_dir" ! -type d \( "$@" \) -exec grep $f4_go -q -e "$f4_pat" {} \; -exec stat -f "$F4FMT_BSD" -- {} + 2>/dev/null ;;
  esac
 else
  case $F4MODE in
   find ) find -H "$f4_dir" ! -type d \( "$@" \) -printf "$F4FMT_FINDP" 2>/dev/null ;;
   stat ) find -H "$f4_dir" ! -type d \( "$@" \) -exec stat -c "$F4FMT_STAT" -- {} + 2>/dev/null ;;
   statbsd ) find -H "$f4_dir" ! -type d \( "$@" \) -exec stat -f "$F4FMT_BSD" -- {} + 2>/dev/null ;;
  esac
 fi | head -n "$f4_lim"
 f4_end ok
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

f4_write_raw() {
 case $F4WR in
  ddbytes ) dd bs=1048576 iflag=fullblock,count_bytes count="$3" of="$1" oflag=seek_bytes seek="$2" conv=notrunc ;;
  * ) dd bs=1 count="$3" of="$1" seek="$2" conv=notrunc ;;
 esac
}

f4_write_b64() {
 f4_bs "$2" "$3"
 printf '%s\n' "$F4B64" | $F4DEC | dd of="$1" bs=$F4BS seek=$(( $2 / F4BS )) conv=notrunc
}

# Everything the payload was supposed to become, when the request cannot be
# carried out after all. Reading it anyway is what keeps the next request
# from being parsed out of the middle of a file.
f4_drain() {
 [ "$1" -gt 0 ] || return 0
 case $F4WR in
  ddbytes ) dd bs=1048576 iflag=fullblock,count_bytes count="$1" of=/dev/null 2>/dev/null ;;
  * ) dd bs=1 count="$1" of=/dev/null 2>/dev/null ;;
 esac
}

# A "D" line means the payload is off the wire for certain, so the session is
# still usable even though the request failed. Its absence is what tells the
# client that a write died halfway and the stream can no longer be trusted.
f4_write_end() {
 [ "$1" -eq 0 ] && echo D
 f4_end_rv "$1" "$2"
}

f4_write_run() {
 F4OUT=$("$@" 2>&1)
 f4_write_end $? "$F4OUT"
}

# write <offset> <length> raw|b64, the payload following the path line: raw
# is exactly <length> bytes, b64 is one line of base64 that decodes to them.
f4_cmd_write() {
 f4_path
 F4WERR=
 F4WDIR=${F4PATH%/*}
 [ -n "$F4WDIR" ] || F4WDIR=/
 if ! f4_num "$1" || ! f4_num "$2"; then
  F4WERR="bad range"
 elif [ -z "$F4WR" ]; then
  F4WERR="no way to write files on remote host"
 elif ! f4_safe_target "$F4PATH"; then
  F4WERR="unsafe path: must be absolute and free of .. components"
 elif [ -d "$F4PATH" ]; then
  F4WERR="is a directory"
 elif [ -e "$F4PATH" ] && [ ! -w "$F4PATH" ]; then
  F4WERR="permission denied"
 elif [ ! -e "$F4PATH" ] && [ ! -w "$F4WDIR" ]; then
  F4WERR="permission denied"
 fi
 case $3 in
  b64 )
   IFS= read -r F4B64 || exit
   if [ -n "$F4WERR" ]; then
    echo D
    f4_end err "$F4WERR"
   else
    f4_write_run f4_write_b64 "$F4PATH" "$1" "$2"
   fi
   ;;
  raw )
   if [ -n "$F4WERR" ]; then
    if [ -z "$F4WR" ]; then
     f4_end err "$F4WERR"
    else
     f4_drain "$2"
     echo D
     f4_end err "$F4WERR"
    fi
   else
    f4_write_run f4_write_raw "$F4PATH" "$1" "$2"
   fi
   ;;
  * )
   f4_end err "bad payload encoding"
   ;;
 esac
}

# patch <nsegs> raw|b64: build <dst> out of pieces of <src> and of new bytes.
# Each segment is a line of its own: "S <off> <len>" copies from the original
# on this side, where it costs no network at all, and "D <len>" is followed by
# that many new bytes -- one base64 line in b64 encoding. Saving a one byte
# change in a hundred megabyte file therefore puts one byte on the wire.
f4_pcopy() {
 case $F4WR in
  ddbytes ) dd bs=1048576 iflag=fullblock,count_bytes count="$1" 2>/dev/null >> "$f4_pdst" ;;
  * ) dd bs=1 count="$1" 2>/dev/null >> "$f4_pdst" ;;
 esac
}

f4_cmd_patch() {
 f4_path
 f4_psrc=$F4PATH
 f4_path
 f4_pdst=$F4PATH
 case $2 in
  raw | b64 ) ;;
  * ) f4_end err "bad payload encoding"; return ;;
 esac
 if ! f4_num "$1"; then
  f4_end err "bad segment count"
  return
 fi
 f4_perr=
 f4_pmade=
 if [ -z "$F4WR" ]; then
  f4_perr="no way to write files on remote host"
 elif ! f4_safe_target "$f4_pdst"; then
  f4_perr="unsafe path: must be absolute and free of .. components"
 elif [ -d "$f4_pdst" ]; then
  f4_perr="is a directory"
 elif [ ! -f "$f4_psrc" ] || [ ! -r "$f4_psrc" ]; then
  f4_perr="cannot read the original file"
 elif F4OUT=$({ : > "$f4_pdst"; } 2>&1); then
  f4_pmade=1
 else
  f4_perr=$(f4_flat "$F4OUT")
 fi
 f4_i=0
 while [ "$f4_i" -lt "$1" ]; do
  IFS=' ' read -r f4_k f4_a f4_b || exit
  f4_i=$(( f4_i + 1 ))
  case $f4_k in
   S )
    if ! f4_num "$f4_a" || ! f4_num "$f4_b"; then
     f4_perr=${f4_perr:-bad segment}
     continue
    fi
    [ -n "$f4_perr" ] && continue
    f4_bs "$f4_a" "$f4_b"
    dd if="$f4_psrc" bs=$F4BS skip=$(( f4_a / F4BS )) count=$(( f4_b / F4BS )) 2>/dev/null >> "$f4_pdst" || f4_perr="copying from the original failed"
    ;;
   D )
    if ! f4_num "$f4_a"; then
     [ -n "$f4_pmade" ] && rm -f "$f4_pdst" 2>/dev/null
     f4_end err "bad segment length"
     return
    fi
    if [ "$2" = b64 ]; then
     IFS= read -r F4B64 || exit
     if [ -z "$f4_perr" ]; then
      printf '%s\n' "$F4B64" | $F4DEC >> "$f4_pdst" || f4_perr="writing failed"
     fi
    elif [ -n "$f4_perr" ]; then
     f4_drain "$f4_a"
    else
     f4_pcopy "$f4_a" || f4_perr="writing failed"
    fi
    ;;
   * )
    [ -n "$f4_pmade" ] && rm -f "$f4_pdst" 2>/dev/null
    f4_end err "bad segment"
    return
    ;;
  esac
 done
 echo D
 if [ -n "$f4_perr" ]; then
  [ -n "$f4_pmade" ] && rm -f "$f4_pdst" 2>/dev/null
  f4_end err "$f4_perr"
 else
  f4_end ok
 fi
}
# trunc <size>: size zero is a plain shell redirection and therefore works
# everywhere, including on a file that is not there yet; any other size needs
# the truncate utility, which is what the "truncate" feature stands for.
f4_cmd_trunc() {
 f4_path
 f4_guard "$F4PATH" || return
 if ! f4_num "$1"; then
  f4_end err "bad size"
  return
 fi
 if [ -d "$F4PATH" ]; then
  f4_end err "is a directory"
  return
 fi
 if [ "$1" -eq 0 ]; then
  F4OUT=$({ : > "$F4PATH"; } 2>&1)
  f4_end_rv $? "$F4OUT"
  return
 fi
 if f4_have truncate; then
  f4_do truncate -s "$1" "$F4PATH"
 else
  f4_end err "no truncate utility on remote host"
 fi
}

# A timestamp is where remote hosts disagree most quietly. GNU touch takes an
# epoch directly; BSD and macOS want a local time string, so the epoch has to
# be converted on the remote host itself -- only it knows its own time zone,
# and touch -t reads local time. The guard on date -r is there because GNU
# date reads -r as "reference file": without it, a file whose name happens to
# be the epoch number would silently supply the wrong timestamp.
f4_epoch_fmt() {
 F4TS=
 if [ ! -e "$1" ]; then
  F4TS=`date -r "$1" +%Y%m%d%H%M.%S 2>/dev/null`
  case $F4TS in
   '' | *[!0-9.]* ) F4TS= ;;
  esac
 fi
 if [ -z "$F4TS" ]; then
  F4TS=`date -d "@$1" +%Y%m%d%H%M.%S 2>/dev/null`
  case $F4TS in
   '' | *[!0-9.]* ) F4TS= ;;
  esac
 fi
}

f4_touch() {
 touch $1 -d "@$2" -- "$3" 2>/dev/null && return 0
 f4_epoch_fmt "$2"
 if [ -z "$F4TS" ]; then
  echo "cannot express timestamp $2 on this host" >&2
  return 1
 fi
 touch $1 -t "$F4TS" -- "$3"
}

f4_utime_ok() {
 [ "$1" = - ] && return 0
 f4_num "$1"
}

f4_cmd_utime() {
 f4_path
 f4_guard "$F4PATH" || return
 if ! f4_have touch; then
  f4_end err "no touch utility on remote host"
  return
 fi
 if ! f4_utime_ok "$1" || ! f4_utime_ok "$2"; then
  f4_end err "bad timestamp"
  return
 fi
 if [ "$1" = - ] && [ "$2" = - ]; then
  f4_end err "nothing to change"
  return
 fi
 F4RV=0
 F4OUT=
 if [ "$1" = "$2" ]; then
  F4OUT=$(f4_touch '' "$1" "$F4PATH" 2>&1)
  F4RV=$?
 else
  if [ "$1" != - ]; then
   F4OUT=$(f4_touch -m "$1" "$F4PATH" 2>&1)
   F4RV=$?
  fi
  if [ $F4RV -eq 0 ] && [ "$2" != - ]; then
   F4OUT=$(f4_touch -a "$2" "$F4PATH" 2>&1)
   F4RV=$?
  fi
 fi
 f4_end_rv $F4RV "$F4OUT"
}

f4_cmd_chown() {
 f4_path
 f4_guard "$F4PATH" || return
 if ! f4_have chown; then
  f4_end err "no chown utility on remote host"
  return
 fi
 F4SPEC=
 case $1 in
  - ) ;;
  '' | *[!0-9]* ) f4_end err "bad uid"; return ;;
  * ) F4SPEC=$1 ;;
 esac
 case $2 in
  - ) ;;
  '' | *[!0-9]* ) f4_end err "bad gid"; return ;;
  * ) F4SPEC=$F4SPEC:$2 ;;
 esac
 if [ -z "$F4SPEC" ]; then
  f4_end err "nothing to change"
  return
 fi
 f4_do chown "$F4SPEC" -- "$F4PATH"
}
# grep <mode><i?> <limit>, then a pattern line and a path line. The reply is
# one byte offset per match: the point of doing this remotely is that a
# gigabyte of log never has to cross the network to find three lines in it.
# awk does the trimming and the limiting, so neither the matched text nor a
# runaway match count ever reaches the wire, and it leaves grep on a broken
# pipe once the limit is reached instead of letting it read on.
f4_cmd_grep() {
 f4_path
 F4PAT=$F4PATH
 f4_path
 if ! f4_have grep || ! f4_have awk; then
  f4_end err "no grep and awk on remote host"
  return
 fi
 if ! f4_num "$2"; then
  f4_end err "bad limit"
  return
 fi
 F4GO=
 case $1 in
  f* ) F4GO=-F ;;
  e* ) F4GO=-E ;;
  * ) f4_end err "bad grep mode"; return ;;
 esac
 case $1 in
  *i ) F4GO="$F4GO -i" ;;
 esac
 if [ ! -f "$F4PATH" ]; then
  f4_end err "not a regular file"
  return
 fi
 if [ ! -r "$F4PATH" ]; then
  f4_end err "permission denied"
  return
 fi
 grep -a -b -o $F4GO -e "$F4PAT" -- "$F4PATH" 2>/dev/null | awk -F: -v n="$2" '{ print $1 } n > 0 && NR >= n { exit }'
 f4_end ok
}
# lidx <first> <count>: the byte offsets of lines first..first+count-1, one
# per line, followed by "T <total>". One awk pass over the remote file, and
# not a byte of it on the wire -- which is the difference between jumping to
# the end of a gigabyte log over ssh and waiting for a gigabyte to arrive.
# LC_ALL=C is exported at the top of this script, so length() counts bytes
# rather than characters, which is what an offset has to be.
f4_cmd_lidx() {
 f4_path
 if ! f4_have awk; then
  f4_end err "no awk on remote host"
  return
 fi
 if ! f4_num "$1" || ! f4_num "$2" || [ "$1" -lt 1 ]; then
  f4_end err "bad line range"
  return
 fi
 if [ ! -f "$F4PATH" ]; then
  f4_end err "not a regular file"
  return
 fi
 if [ ! -r "$F4PATH" ]; then
  f4_end err "permission denied"
  return
 fi
 awk -v f="$1" -v n="$2" '{ if (NR >= f && NR < f + n) printf "%d\n", off; off += length($0) + 1 } END { printf "T %d\n", NR }' "$F4PATH" 2>/dev/null
 f4_end ok
}
f4_cmd_wmode() {
 if f4_try_wmode "$1"; then
  F4WR=$1
  f4_end ok
 else
  f4_end err "write mode not available"
 fi
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
  write )
   f4_cmd_write "$F4A1" "$F4A2" "$F4A3"
   ;;
  trunc )
   f4_cmd_trunc "$F4A1"
   ;;
  patch )
   f4_cmd_patch "$F4A1" "$F4A2"
   ;;
  utime )
   f4_cmd_utime "$F4A1" "$F4A2"
   ;;
  grep )
   f4_cmd_grep "$F4A1" "$F4A2"
   ;;
  lidx )
   f4_cmd_lidx "$F4A1" "$F4A2"
   ;;
  ffind )
   f4_cmd_ffind "$F4A1" "$F4A2" "$F4A3"
   ;;
  chown )
   f4_cmd_chown "$F4A1" "$F4A2"
   ;;
  wmode )
   f4_cmd_wmode "$F4A1"
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
