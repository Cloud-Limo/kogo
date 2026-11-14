#!/usr/bin/env python3
"""Stand-in for Kogo's SQL pass: PREPARE every sql literal in .kg files against a database.
usage: sqlcheck.py DIR DBNAME   (runs psql as postgres via sudo)"""
import re, subprocess, sys, glob, os
d, db = sys.argv[1], sys.argv[2]
lit = re.compile(r'sql"""(.*?)"""|sql"((?:[^"\\]|\\.)*)"', re.S)
par = re.compile(r'\$\{([^}]*)\}|\$([A-Za-z_]\w*)')
n_ok = n_bad = 0
for f in sorted(glob.glob(os.path.join(d, '*.kg'))):
    src = open(f).read()
    for m in lit.finditer(src):
        sql = m.group(1) if m.group(1) is not None else m.group(2)
        line = src[:m.start()].count('\n') + 1
        names = []
        def rep(mm):
            name = mm.group(1) or mm.group(2)
            if name not in names: names.append(name)
            return '$%d' % (names.index(name) + 1)
        stmt = par.sub(rep, sql).strip()
        script = "set search_path = cusoon, public;\nprepare q as %s;\nselect parameter_types, result_types from pg_prepared_statements where name='q';\n" % stmt
        r = subprocess.run(['sudo','-n','-u','postgres','psql','-d',db,'-X','-At','-v','ON_ERROR_STOP=1'], input=script, capture_output=True, text=True)
        loc = '%s:%d' % (os.path.basename(f), line)
        if r.returncode == 0:
            n_ok += 1
            print('ok   %-18s params(%s) -> %s' % (loc, ', '.join(names), r.stdout.strip().splitlines()[-1]))
        else:
            n_bad += 1
            print('FAIL %-18s %s' % (loc, ' | '.join(l for l in r.stderr.strip().splitlines() if l.strip())[:300]))
print('%d ok, %d failed' % (n_ok, n_bad))
