# FISH+ remote helper, protocol version 1.
#
# This script is fed into a plain POSIX shell (usually via an ssh session)
# on the remote host. After initialization it keeps reading commands from
# the very same stdin, one request per line:
#
#   <id> <cmd> [<base64 arg> ...]
#
# and answers with zero or more payload lines followed by a terminator:
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

F4FEATS=
for f4c in dd base64 readlink du grep sed awk wc sha256sum; do
 f4_have $f4c && F4FEATS="$F4FEATS $f4c"
done

if stat --format=%s . >/dev/null 2>&1; then
 F4FEATS="$F4FEATS stat"
elif stat -f %z . >/dev/null 2>&1; then
 F4FEATS="$F4FEATS statbsd"
fi

if find -H . -mindepth 0 -maxdepth 0 -printf '%s\n' >/dev/null 2>&1; then
 F4FEATS="$F4FEATS find"
fi

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
   f4_dec "$F4A1"
   echo
   f4_end ok
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