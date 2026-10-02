#!/bin/sh
spec=rpm/barkauf.spec
n=$(awk '/^Release:/ { print $2; exit }' "$spec")
next=$((n + 1))
sed "s/^Release:.*/Release:    $next/" "$spec" > "$spec.tmp" && mv "$spec.tmp" "$spec"
echo "Release $n -> $next"
sfosbuild deploy root@172.20.10.3 .