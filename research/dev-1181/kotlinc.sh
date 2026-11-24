#!/usr/bin/env bash
# [FROM=lines] kotlinc.sh <work dir> <n>...: compile each program n
# under <work>/$FROM (progs by default) that `kogo check` accepted, its
# .kg files given to kotlinc under .kt names, with the compiler Gradle
# bundles (2.4.0). Writes <work>/kotlinc/<from>-<n>.out and prints n,
# kotlinc's exit, and its first error line.
set -euo pipefail
work=$1; shift
from=${FROM:-progs}
L=$(dirname "$(find ~/.gradle/wrapper/dists -name 'kotlin-compiler-embeddable-*.jar' | head -1)")
JAVA=$(mise -C ~/Dev/cusoon2 which java)
STD=$(ls "$L"/kotlin-stdlib-2*.jar | head -1)
mkdir -p "$work/kotlinc"
for n in "$@"; do
    src="$work/kotlinc/$from-$n.src" out="$work/kotlinc/$from-$n.classes"
    rm -rf "$src" "$out" && mkdir -p "$src" "$out"
    for f in "$work/$from/$n"/*.kg; do cp "$f" "$src/$(basename "$f" .kg).kt"; done
    "$JAVA" -cp "$L/*" org.jetbrains.kotlin.cli.jvm.K2JVMCompiler -no-stdlib -cp "$STD" \
        -d "$out" "$src"/*.kt > "$work/kotlinc/$from-$n.out" 2>&1 && rc=0 || rc=$?
    printf '%s\t%s\t%s\n' "$n" "$rc" "$(grep -m1 ": error:" "$work/kotlinc/$from-$n.out" || true)"
done
