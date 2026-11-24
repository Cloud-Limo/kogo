#!/usr/bin/env python3
"""Lay out Kotlin's diagnostic test files as programs for `kogo check`.

collect.py <tests dir> <work dir>

For each .kt under <tests dir> it either counts a skip by its reason
or writes <work>/progs/<n>/*.kg, the file's parts with every
diagnostic marker stripped and `package kt` added to a part that has
no package line. It writes <work>/manifest.tsv: n, path, verdict
(a skip reason, or "kept"). The clone is only read, never run.
"""
import os
import re
import sys


# A directive that changes the language, the compiler's options or the
# target: the file's verdict is not plain kotlinc's.
LANGUAGE = {"LANGUAGE", "LANGUAGE_VERSION", "API_VERSION",
            "ALLOW_DANGEROUS_LANGUAGE_VERSION_TESTING"}
OPTIONS = {"OPT_IN", "RETURN_VALUE_CHECKER_MODE", "WITH_EXTRA_CHECKERS",
           "WITH_EXPERIMENTAL_CHECKERS", "ALLOW_KOTLIN_PACKAGE",
           "LENIENT_MODE", "EXPLICIT_API_MODE", "JSPECIFY_STATE",
           "JSR305_GLOBAL_REPORT", "IDE_MODE", "SKIP_JAVAC_CHECKS",
           "ENABLE_JVM_PREVIEW", "JVM_DEFAULT_MODE", "PLUGIN",
           "ENABLE_PLUGIN", "STRICT_JAVA_NULLABILITY_ASSERTIONS",
           "SAM_CONVERSIONS", "INHERIT_MULTIFILE_PARTS", "COMPILER_ARGUMENTS"}
TARGET = {"TARGET_BACKEND", "TARGET_PLATFORM", "JVM_TARGET", "JDK_KIND",
          "JDK_RELEASE", "PLATFORM"}
MODULE = {"MODULE", "DEPENDS_ON"}

DIRECTIVE = re.compile(r"^// ?!?([A-Z][A-Z0-9_]+)\b(.*)$", re.M)
FILEPART = re.compile(r"^// ?FILE: ?(\S+).*$", re.M)
# <!NAME!>, <!A, B!>, <!NAME("arg", ...)!>: an opening marker ends at
# the first !> after its name.
MARKER = re.compile(r"<![A-Z][A-Z0-9_]*.*?!>")
CLOSE = "<!>"
PACKAGE = re.compile(r"^\s*package\s", re.M)


def skip_reason(text, dirs):
    names = {d for d, _ in dirs}
    if names & MODULE:
        return "several modules"
    if os.environ.get("ANY_DIRECTIVE"):
        pass  # kotlinc's own verdict still decides; see README.md
    elif names & LANGUAGE:
        return "language version or feature"
    elif names & TARGET:
        return "target or JDK"
    elif names & OPTIONS:
        return "compiler option"
    if "<!" not in text:
        return "no marker"
    till = dict(dirs).get("RUN_PIPELINE_TILL", "").strip(" :")
    # The spec's tests (compiler/tests-spec) carry no RUN_PIPELINE_TILL:
    # their header says which expect an error.
    if not till and "SPEC TEST (NEGATIVE)" in text:
        till = "FRONTEND"
    if till != "FRONTEND":
        return "no error expected (pipeline runs past the frontend)"
    return None


def strip(text):
    return MARKER.sub("", text).replace(CLOSE, "")


def parts(text):
    """The file's parts by // FILE:, or None when they don't split cleanly."""
    found = list(FILEPART.finditer(text))
    if not found:
        return [("main.kt", text)]
    head = text[:found[0].start()]
    out = []
    for i, m in enumerate(found):
        end = found[i + 1].start() if i + 1 < len(found) else len(text)
        name = m.group(1)
        if not name.endswith(".kt"):
            return None
        out.append((name, text[m.end():end]))
    if re.search(r"^\s*[^/\s]", head, re.M):  # code before the first FILE:
        return None
    names = [os.path.basename(n) for n, _ in out]
    if len(set(names)) != len(names):
        return None
    return out


def main():
    tests, work = sys.argv[1], sys.argv[2]
    os.makedirs(os.path.join(work, "progs"), exist_ok=True)
    rows = []
    n = 0
    for root, ds, fs in os.walk(tests):
        ds.sort()
        for f in sorted(fs):
            path = os.path.join(root, f)
            rel = os.path.relpath(path, tests)
            if f.endswith(".kts"):
                rows.append((None, rel, "script (.kts)"))
                continue
            if not f.endswith(".kt"):
                continue
            text = open(path, encoding="utf-8", errors="replace").read()
            dirs = [(m.group(1), m.group(2)) for m in DIRECTIVE.finditer(text)]
            why = skip_reason(text, dirs)
            if why is None:
                ps = parts(text)
                if ps is None:
                    why = "FILE parts that don't split cleanly"
            if why:
                rows.append((None, rel, why))
                continue
            n += 1
            d = os.path.join(work, "progs", str(n))
            os.makedirs(d, exist_ok=True)
            for name, body in ps:
                body = strip(body)
                if not PACKAGE.search(body):
                    body = "package kt\n" + body
                base = os.path.basename(name)[:-3] + ".kg"
                open(os.path.join(d, base), "w", encoding="utf-8").write(body)
            rows.append((n, rel, "kept"))
    with open(os.path.join(work, "manifest.tsv"), "w") as out:
        for n_, rel, why in rows:
            out.write(f"{n_ or ''}\t{rel}\t{why}\n")


if __name__ == "__main__":
    main()
