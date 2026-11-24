#!/usr/bin/env bash
# run.sh <tests dir> <work dir>: the whole count for one directory of
# Kotlin's diagnostic test files (README.md says what each step does).
# The work dir, outside any worktree, is made afresh.
set -euo pipefail
export PYTHONDONTWRITEBYTECODE=1
here=$(cd "$(dirname "$0")" && pwd)
tests=$1 work=$2
rm -rf "$work" && mkdir -p "$work"
export DATABASE_URL=${DATABASE_URL:-postgres://kogo:kogo@127.0.0.1/kogo_dev1181}
(cd "$here/../.." && mise exec -- go build -o "$work/kogo" ./cmd/kogo)
python3 -I "$here/collect.py" "$tests" "$work"
"$here/check.sh" "$work/kogo" "$work" > /dev/null
python3 -I "$here/reduce.py" "$work/kogo" "$tests" "$work" > /dev/null
awk -F'\t' '$2==0{print $1}' "$work/check.tsv" |
    xargs -r -P4 -n1 "$here/kotlinc.sh" "$work" | sort -n > "$work/kotlinc-progs.tsv"
awk -F'\t' '$3=="passes"{print $1}' "$work/lines.tsv" |
    xargs -r -P4 -n1 env FROM=lines "$here/kotlinc.sh" "$work" | sort -n > "$work/kotlinc-lines.tsv"
python3 -I "$here/summary.py" "$work"
