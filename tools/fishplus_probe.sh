#!/bin/sh
# f4 FISH+ remote host probe.
#
# Run this on a machine you would like to browse with f4 over FISH+ and
# paste the whole output into the FISH+ compatibility issue. It only reads:
# nothing is created, changed or removed.
#
#   sh fishplus_probe.sh 2>&1 | tee fishplus-probe.txt

echo "=== system"
uname -a 2>&1
echo "=== shell"
ls -l /bin/sh 2>&1
echo "=== tools"
for t in sh dash ash bash ksh base64 openssl stat find ls dd head tail cat \
         readlink du grep sed awk wc expr tr sha256sum md5sum; do
	p=`command -v $t 2>/dev/null` || p=MISSING
	echo "$t: $p"
done
echo "=== base64 decode"
printf aGk= | base64 -d 2>&1
printf aGk= | base64 -D 2>&1
printf aGk= | openssl base64 -d 2>&1
echo "=== find -printf"
find -H . -mindepth 0 -maxdepth 0 -printf '%y %Y %s %T@ %A@ %C@ %m %U %G %f\n' 2>&1
echo "=== gnu stat"
stat -c '%f %s %Y %X %Z %u %g %n' . 2>&1
echo "=== bsd stat"
stat -f '%p %z %m %a %c %u %g %N' . 2>&1
echo "=== ls -l"
ls -l -A . 2>&1 | head -5
echo "=== ls -f"
ls -f -l -A . 2>&1 | head -3
echo "=== ls date format"
LC_ALL=C ls -l -A / 2>&1 | head -5
echo "=== ls color"
ls --color=never -d . 2>&1
echo "=== dd"
dd iflag=fullblock skip=1 count=1 bs=16 if=/dev/zero of=/dev/null 2>&1
echo "=== head -c"
printf helloworld | head -c 5 2>&1
echo
printf helloworld | ( head -c 5 >/dev/null 2>&1; cat ) 2>&1
echo
echo "=== read builtin"
printf 'a b  c \n' | ( IFS= read -r line; printf '[%s]\n' "$line" )
echo "=== done"