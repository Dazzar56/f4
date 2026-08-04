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

F4DEC=
for f4c in 'base64 -d' 'base64 -D' 'base64 --decode' 'openssl base64 -d'; do
 if [ "`printf aGk= | $f4c 2>/dev/null`" = hi ]; then
  F4DEC=$f4c
  break
 fi
done

f4_dec() {
 printf '%s' "$1" | $F4DEC 2>/dev/null
}

f4_path() {
 IFS= read -r F4PATH || exit
 case $F4PATH in
  '~'* ) F4PATH=`f4_dec "${F4PATH#\~}"` ;;
 esac
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

F4FEATS=
for f4c in dd base64 readlink du grep sed awk wc sha256sum; do
 f4_have $f4c && F4FEATS="$F4FEATS $f4c"
done
for f4c in find stat statbsd; do
 f4_try_mode $f4c && F4FEATS="$F4FEATS $f4c"
done
[ -n "$F4MODE" ] && F4FEATS="$F4FEATS mode:$F4MODE"

f4_list() {
 case $F4MODE in
  find ) find -H "$F4PATH" -mindepth 1 -maxdepth 1 -printf "$F4FMT_FIND" 2>/dev/null ;;
  stat ) stat -c "$F4FMT_STAT" -- "$F4PATH"/* "$F4PATH"/.* 2>/dev/null ;;
  statbsd ) stat -f "$F4FMT_BSD" -- "$F4PATH"/* "$F4PATH"/.* 2>/dev/null ;;
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

f4_cmd_mode() {
 if f4_try_mode "$1"; then
  F4MODE=$1
  f4_end ok
 else
  f4_end err "mode not available"
 fi
}

F4ID=0
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
  mode )
   f4_cmd_mode "$F4A1"
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