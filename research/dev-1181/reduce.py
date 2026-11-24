#!/usr/bin/env python3
"""Find what `kogo check` accepts inside the files it refuses whole.

reduce.py <kogo binary> <tests dir> <work dir>

A Kotlin test file is mostly refused for its first class or annotation,
which hides the rest of it. For each program check.sh found refused,
this runs `kogo check` and drops,
for each error, the smallest unit of lines around it by indentation: the
line, the lines indented under it, and the closing line that ends them.
It repeats until the program passes or no line that carried a
diagnostic marker is left. A remainder that passes with such a line is
written to <work>/lines/<n>/*.kg with marks.txt (file:line of each kept
marked line), for kotlinc.sh to judge, and listed in
<work>/lines.tsv: n, path, passes and lines kept of all, or
stopped and why.
"""
import json
import os
import re
import subprocess
import sys
from multiprocessing import Pool

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from collect import PACKAGE, parts, strip  # noqa: E402

CLOSER = re.compile(r"^\s*[})\]]")
kogo = tests = work = None


def indent(line):
    s = line.expandtabs(4)
    return len(s) - len(s.lstrip(" "))


def unit(lines, i):
    """The ids of the unit around line i of lines [(id, text)]."""
    blank = lambda k: not lines[k][1].strip()
    # A closing line belongs to the statement it closes.
    s = i
    while s > 0 and (CLOSER.match(lines[s][1]) or lines[s][1].lstrip().startswith(".")) \
            and not blank(s):
        k = s - 1
        while k > 0 and (blank(k) or indent(lines[k][1]) > indent(lines[s][1])):
            k -= 1
        s = k
    ind = indent(lines[s][1])
    e = s + 1
    while e < len(lines) and (blank(e) or indent(lines[e][1]) > ind):
        e += 1
    while e < len(lines) and indent(lines[e][1]) == ind and (
            CLOSER.match(lines[e][1]) or lines[e][1].lstrip().startswith(".")):
        e += 1
        while e < len(lines) and (blank(e) or indent(lines[e][1]) > ind):
            e += 1
    return {lines[k][0] for k in range(s, e)}


def reduce_one(job):
    n, rel = job
    text = open(os.path.join(tests, rel), encoding="utf-8",
                errors="replace").read()
    files, marked, gid = [], set(), 0
    for name, body in parts(text):
        ls = []
        if not PACKAGE.search(body):
            ls.append((None, "package kt"))
        for l in body.split("\n"):
            if "<!" in l and not l.lstrip().startswith("//"):
                marked.add(gid)
            ls.append((gid, strip(l)))
            gid += 1
        files.append((os.path.basename(name)[:-3] + ".kg", ls))
    keep = set(range(gid))
    d = os.path.join(work, "lines", str(n))
    os.makedirs(d, exist_ok=True)
    why = "cap"
    for _ in range(400):
        if not keep & marked:
            why = "no marked line left"
            break
        for f in os.listdir(d):
            os.remove(os.path.join(d, f))
        written = {}
        for name, ls in files:
            shown = [(i, t) for i, t in ls if i is None or i in keep]
            path = os.path.join(d, name)
            open(path, "w", encoding="utf-8").write(
                "\n".join(t for _, t in shown) + "\n")
            written[path] = (shown, ls)
        r = subprocess.run([kogo, "check", d], capture_output=True,
                           text=True, timeout=60)
        if r.returncode == 0:
            with open(os.path.join(d, "marks.txt"), "w") as m:
                for path, (shown, _) in written.items():
                    for k, (i, _) in enumerate(shown):
                        if i in marked:
                            m.write(f"{os.path.basename(path)}:{k + 1}\n")
            return (n, rel, "passes", f"{len(keep)}/{gid}")
        if r.returncode != 1:
            why = f"exit {r.returncode}: {r.stderr[:100]}"
            break
        drop = set()
        for line in r.stdout.splitlines():
            try:
                e = json.loads(line)
            except ValueError:
                continue
            if e.get("file") not in written:
                continue
            shown, ls = written[e["file"]]
            ln = e.get("line", 0)
            if not 1 <= ln <= len(shown) or shown[ln - 1][0] is None:
                continue
            gi = shown[ln - 1][0]
            live = [(i, t) for i, t in ls if i is not None and i in keep]
            k = next(k for k, (i, _) in enumerate(live) if i == gi)
            drop |= unit(live, k)
        if not drop & keep:
            why = "an error not placed: " + r.stdout.splitlines()[0][:160]
            break
        keep -= drop
    for f in os.listdir(d):
        os.remove(os.path.join(d, f))
    os.rmdir(d)
    return (n, rel, "stopped", " ".join(why.split()))


def main():
    global kogo, tests, work
    kogo, tests, work = sys.argv[1:4]
    paths = {}
    for line in open(os.path.join(work, "manifest.tsv")):
        n, rel, why = line.rstrip("\n").split("\t")
        if why == "kept":
            paths[n] = rel
    jobs = [(l.split("\t")[0], paths[l.split("\t")[0]])
            for l in open(os.path.join(work, "check.tsv"))
            if l.split("\t")[1] == "1"]
    with Pool(os.cpu_count()) as p:
        rows = sorted(p.imap_unordered(reduce_one, jobs), key=lambda r: int(r[0]))
    with open(os.path.join(work, "lines.tsv"), "w") as out:
        for r in rows:
            out.write("\t".join(r) + "\n")
    found = [r for r in rows if r[2] == "passes"]
    print(f"refused {len(jobs)}, a marked remainder passes in {len(found)}")


if __name__ == "__main__":
    main()
