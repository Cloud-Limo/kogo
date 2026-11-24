#!/usr/bin/env python3
"""summary.py <work dir>: the counts run.sh's steps left, and each file
kotlinc confirms, with its first error, for grouping by hand."""
import collections
import os
import sys

work = sys.argv[1]


def rows(name):
    p = os.path.join(work, name)
    if not os.path.exists(p):
        return []
    return [l.rstrip("\n").split("\t") for l in open(p) if l.strip()]


manifest = rows("manifest.tsv")
path = {r[0]: r[1] for r in manifest if r[0]}
reasons = collections.Counter(r[2] for r in manifest)
check = rows("check.tsv")
accepted = [r[0] for r in check if r[1] == "0"]
refused = [r for r in check if r[1] == "1"]
other = [r for r in check if r[1] not in ("0", "1")]
kprogs = rows("kotlinc-progs.tsv")
lines = rows("lines.tsv")
passes = [r for r in lines if r[2] == "passes"]
klines = rows("kotlinc-lines.tsv")

print(f"files read: {len(manifest)}")
for why, k in reasons.most_common():
    if why != "kept":
        print(f"  skipped, {why}: {k}")
print(f"kept: {reasons['kept']}")
print(f"refused by kogo check: {len(refused)}")
if other:
    print(f"kogo check could not run (exit 2, a crash): {len(other)}: "
          + ", ".join(path[r[0]] for r in other))
print(f"accepted by kogo check: {len(accepted)}")
print(f"  of those, refused by kotlinc: {sum(r[1] != '0' for r in kprogs)}")
print(f"reduced: a marked remainder passes kogo check in {len(passes)}")
print(f"  of those, refused by kotlinc: {sum(r[1] != '0' for r in klines)}")
crash = [r for r in lines if r[3].startswith("exit 2")]
if crash:
    print("  kogo check crashed while reducing: "
          + ", ".join(path[r[0]] for r in crash))
print()
for label, ks in (("whole", kprogs), ("reduced", klines)):
    for r in ks:
        if r[1] != "0":
            err = r[2].split(": error: ", 1)[-1] if len(r) > 2 else ""
            print(f"{label}\t{r[0]}\t{path[r[0]]}\t{err}")
