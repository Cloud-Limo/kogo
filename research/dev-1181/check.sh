#!/usr/bin/env bash
# check.sh <kogo binary> <work dir>: run `kogo check` on each program
# collect.py wrote, in parallel, and write <work>/check.tsv: n, exit,
# the first error's code and message.
set -euo pipefail
kogo=$1 work=$2
export kogo
export DATABASE_URL=${DATABASE_URL:-postgres://kogo:kogo@127.0.0.1/kogo_dev1181}
one() {
    out=$(timeout 60 "$kogo" check "$1" 2>&1) && rc=0 || rc=$?
    first=$(head -1 <<<"$out" | python3 -Ic 'import json,sys
l=sys.stdin.read().strip()
try:
    e=json.loads(l); print(e.get("code",""), e.get("message","").replace("\t"," ")[:200], sep="\t")
except Exception:
    print("raw", l.replace("\t"," ")[:200], sep="\t")')
    printf '%s\t%s\t%s\n' "$(basename "$1")" "$rc" "$first"
}
export -f one
ls -d "$work"/progs/* | xargs -P "$(nproc)" -I{} bash -c 'one {}' | sort -n > "$work/check.tsv"
cut -f2 "$work/check.tsv" | sort | uniq -c
