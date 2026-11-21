# Packages and names: today's rules, cusoon2's Kotlin, a tighter spec

DEV-1068, 22 Nov 2026. Research and advice: no code, and `spec.md` is
not changed here. Each proposal in part 3 changes what the language
is, so each is Greg's to answer yes or no.

Read at: Kogo `origin/main` 34c4a47; cusoon2 `origin/renew` 4906409ba
(88 Kotlin files, 25 packages; the port, 24 `.kg` files in 8 packages)
and `kogo/port/model/google.kg` on PR 1576's branch. Every Kogo claim
was run with `kogo check` on a scratch package of a few lines, and
every Kotlin claim with kotlinc 2.4.0. DEV-1067 (PR 127) edits spec 5,
12, 13 and 14 beside this: the two meet only in section 14's table,
where both add rows.

## 1. The rules today

"Agree" compares the spec with the checker. **Bold** rows are the holes.

| # | Rule, as the checker has it | Spec | Enforced | Agree |
| --- | --- | --- | --- | --- |
| 1 | No wildcard import, no `import ... as` | 2 | parser, `syntax` | yes |
| 2 | An import names a top-level name and brings every declaration of it: the type and the funs named for it | 2 says "one declaration" | `importAll`, by name | wording |
| 3 | A nested name is not imported: `import q.a.O.Ok` is `unknown-import`, "there is no package q.a.O" | unstated | `importAll` | **unstated**, and the message misleads |
| 4 | **One name is not imported from two packages (`duplicate`), two extension funs on two receivers included, which Kotlin accepts. Nothing lets a file use both** | unstated | `importAll` | **unstated** |
| 5 | **A name is written bare. `q.a.P` as a type and `q.a.make()` are `unknown-name`, and the message does not say why** | unstated | `missing` | **unstated** |
| 6 | **A type, typealias or val is looked up in the import, then the file's package, then `kogo`, then the built-ins, and the first wins without a word. So an import hides the package's own `P`, two files of one package can read `P` as two types, and a program's own `List`, `String` or `typealias Int = Long` hides Kogo's.** Kotlin does the same | 2 says only that `kogo` is in scope | `homes`, `findType` | **unstated** |
| 7 | A fun is looked up in all of those at once: each is a candidate, and a call two of them fit is `ambiguous-call`, where Kotlin picks the import | 5.2, but for where candidates come from (`decisions.md`) | `funcs` | partly |
| 8 | **In a package a type, a typealias, a marker and a top-level val share one table of names, across files and whatever their visibility: two files' `private data class P` are `duplicate`.** Kotlin refuses the same two classes and typealiases ("redeclaration") and accepts two `private val`s | unstated | `declare` | **unstated** |
| 9 | Funs and extension vals share names; two of one shape are an error; two `private` ones in two files do not meet | 5.2 | `overloads` | yes |
| 10 | **Any type may nest in a plain or a sealed interface, variant or not, named in full (`G.Place`); it checks, emits and builds in Go. A nested typealias is parsed and dropped.** A data class and an enum hold none | 3.2 has variants only; 3.4 "declares abstract vals and funs" | parser, `declare` | **no**: the checker takes more |
| 11 | A nested type is named in full everywhere | 3.2 | `findType` | yes |
| 12 | Packages do not import each other | 2, 14 | `cycles.go` | yes |
| 13 | `private` is the file, `internal` the package; an import of what the file may not name is `not-visible` | 2 | `visible`, `see` | yes |
| 14 | A binding with a receiver is in scope everywhere and never imported; a program's extension is imported by its name | 13, 2 | `members`, `member-import` | yes; an unimported one is "a P has no function show", with no import named |
| 15 | **A program file may say `package kogo` or `package kogo.time`: its names are then in scope in every file** | 1 and 13 imply not | nothing | **no** |
| 16 | A local hides anything outside; an inner block's local hides an outer one and a type parameter hides a type, without a word, as in Kotlin | 5.2, the first only | `scope` | partly |
| 17 | **Two declarations whose Go names meet (`foo` and `Foo`, both types) pass `kogo check` and are refused by `kogo emit`, `go-name`** | unstated | emitter | **no**: check is to be the only gate |
| 18 | **A typealias names its type only where a type is written: `PRow(1)`, `KindRow.A` and `is ORow.Ok` are errors, though Kotlin takes all three** | 3.5, "transparent, as in Kotlin" | `qualifier`, `call` | **no** |
| 19 | An import of the file's own package, a repeated one and an unused one are accepted | unstated | nothing | harmless |

The issue's list said a plain interface holds no types. That is the
spec; the checker and the emitter take them (row 10). No file in this
repo nests anything but a variant, so no test holds either reading.

Row 8 is Kotlin's own rule, so a private type and a private fun differ
in Kotlin exactly as in Kogo. Only two `private val`s are stricter here.

## 2. cusoon2's Kotlin against them

Commands are in the appendix, by letter.

| Rule | What the Kotlin has | Count | In Kogo | Fits |
| --- | --- | --- | --- | --- |
| 1, 4 | `import ... as` (B) | 26: 19 jOOQ rows, 2 Stripe's types, 2 `time.BookingStatus`, 1 nested `StripeModel.Config`, 2 funs | 13 only read better, the name being free in the file: the port writes the plain name. 13 are forced: 10 because the file's own package declares the name, 1 by a type nested in the file, 2 funs by a property or local of the name | **10 do not** |
| 4, 6 | Table rows whose name is also an application type's (G) | 17 of the 40 imported: `Booker`, `Booking`, `Charge`, `Journey`, `Place`, `PlaceType`... | The port's `cusoon.schema` gives a row its table's name ("a stand-in for what `kogo schema` will write"), and a wire type keeps the frontend's (`kogo/types.sh` compares them word for word). Neither is free to rename | **no**, wherever a file maps the row to the wire type: 9 of the 10 above, in 6 files of `rpc.storefront` |
| 6 | An import that hides the package's own type | 1: `rpc/storefront/call.kt` reads `Booker` as the row, beside `lists.kt`'s `Booker` | compiles, the same way | yes, silently |
| 4 | One name imported twice without `as` | 0 | | yes |
| 1 | Wildcard import (C) | 1, using 7 of the package's 42 names | 7 imports | yes |
| 4 | Type names declared in two packages (E) | 9 (the issue's 11 was a rougher grep) | Only `BookingStatus` meets its namesake in a file, counted above | yes |
| 5 | Qualified names in code (D) | 2 lines, both `java.util.UUID` | `Uuid` | yes |
| 10 | Nested types (F) | 34, and 6 companions: 15 variants of a sealed type, 4 in an interface (`GoogleMaps.Place`), 15 in a class. Written `Outer.Inner` on 40 lines | A variant stays. The rest go to the top level; the port took `GoogleMapsPlace`, `Place` being the row | yes |
| 10 | Top-level objects (H) | 15, with 140 members: 10 hold operations, 3 are pricing regimes, 2 hold state and constants | an `api` list (`OpsApi` is ported so), a `data object`, parameters and vals | yes |
| 12 | Packages on an import cycle (K) | 19 of 25 on one; 459 names and 629 import lines inside it | at least 52 of the 459 must move for the packages to be layers. The port's 8 packages have none so far: rows went to a package of their own under `model`, state arrives by parameter, `discountedCents` moved to `pricing` | yes, by moving |
| 8, 9 | `private` top-level names (I, J) | 85 funs, 33 vals, 10 types; none twice in a package, in the port neither | | yes |
| 6, 7 | Names the prelude has (M) | no type; the fun `randomString`; 2 extensions on other receivers, `Area.contains` and `PDPageContentStream.list` | the prelude's; overloads | yes |
| 17 | Names of a package apart only by the first letter's case (L) | 28 pairs, each a type beside a fun or a val, which the emitter spells apart | | yes |

The port so far (N; PR 1576's branch, 27 files): no type name in two
packages, 4 nested types, all variants, and 3 imports of a fun beside
the package's own fun of that name, which are overloads (rule 7):
`cusoon.authentication.passkeys` in `rpc/ops/passkeys.kg` and
`logins.kg`, and `cusoon.model.setDriverShare` in `rpc/ops/ops.kg`.

So one thing does not fit, and it is the issue's own question. It is
not `GoogleMaps.Place`, which a joined name answers. It is a row and a
wire type of one name in one file: 9 times in `rpc.storefront`, which
is not ported yet. The tenth is `time.BookingStatus` beside the wire
type of that name.

## 3. Proposals

Sizes are code / tests / doc lines. "Live" says whether cusoon2 or its
port meets the hole today.

**1. `import ... as`, for two declarations of one name in one file.**
Live: 10 files-and-names, section 2. Recommended.

Spec 2, in place of "no `import ... as`":

> `import cusoon.schema.Booker as BookerRow` gives the declaration
> another name in this file, as in Kotlin: the file writes `BookerRow`,
> also for its constructor, constants and variants. No name comes into
> a file twice: one name imported from two packages is an error at the
> second, two funs included, though Kotlin overloads those. Give one
> `as`.

| | `import ... as` | Qualified name | Today's rule, "rename one" |
| --- | --- | --- | --- |
| The author writes | the Kotlin's own line, once a file: 10 lines | `cusoon.schema.Booker` at each use: 17 uses | a suffix on every row type or wire type, for good |
| A reader | meets a name no declaration has; the file's imports answer. One declaration may have two names in two files, as `Area` is `AreaRow` and `AreaPojo` in cusoon2 | sees the package at the use; but the imports no longer list all a file takes, and `a.b` may be a package or a value | reads one name everywhere |
| Go from `kogo emit` | no change: it writes every other package's name as `p1.Booker` from the declaration the checker found, and reads no import | no change | no change |
| TypeScript from `kogo ts` | no change: the declared name. Two wire types of one name in a list stay an error (15.4) | no change | the wire names change, and the frontend with them, unless the rows take the suffix |
| Size | 45 / 30 / 12 | 70 / 40 / 12; the cycle check must also read uses | 0 / 0 / 6 |

A typealias beside the declaration (`typealias BookerRow = Booker`) is
not a way to rename today: rule 18. cusoon2's own habit is 26 `as`
against 2 qualified names. Not taken: `as` allowed only where the name
is taken (6 / 4 more, and an import would turn wrong when another file
changes). Left to the build: whether an `api` list may name a fun by
its alias; the name on the wire is the declared one either way (15.1).

**2. Refuse an import that hides the package's own declaration.**
Live: 1 in the Kotlin. Recommended, and it leans on 1 for its fix.

Spec 2: "An import does not name what the file's own package declares
as a type, a typealias or a top-level `val`, though in Kotlin the
import wins: give it another name with `as`, or rename one. A fun of
that name is one more candidate (5.2)."

Guards against: reading `Booker` as the package's own under an import
that made it the row, and two files of one package meaning two types
by one name. Not taken: keep Kotlin's rule and state it (0 / 0 / 2).
Size 8 / 4 / 3, and a row in 14.

**3. State that a name is written bare, and what an import names.**
Not live: nothing in cusoon2 depends on it. Recommended unless 1 is
answered with the qualified name.

Spec 2: "A name is written as it is declared or imported, never with
its package: `cusoon.model.Booking` in code is an error. An import
names a top-level name and brings every declaration of it; a nested
type comes with its outer one, `import shop.Outcome` and then
`Outcome.Ok`. An extension is imported by its own name."

Size 0 / 2 / 6, and two rows in 14. The messages are proposal 8.

**4. State the one table of names for each package, and keep it.**
Not live: no private name is declared twice. Recommended.

Spec 2: "In a package, a type, a typealias, a marker and a top-level
`val` each take a name no other of them has, in any file: `private`
limits who may name a declaration, not where its name is taken. Kotlin
has this rule too, but for two `private val`s in two files. Funs share
names (5.2), and a fun may be named for a type or a val."

Not taken: private types and vals per file, as private funs are. For
types Kotlin itself refuses it; for vals alone it is 15 / 6 and the
emitter already numbers private names by file. Size 0 / 2 / 5, and a
row in 14.

**5. Only a sealed interface holds types, and only its variants.**
Not live: the port nests variants alone. Recommended. "No" means the
alternative below.

Spec 3.2: "Only a sealed interface holds types, and each implements
it." Spec 3.4: "An interface holds no types: declare each at the top
level." Size 8 / 4 / 4, and a row in 14.

Guards against: a second way to group names beside the package, in a
form the spec never gave. Not taken: state what the checker does, "an
interface may hold types, each named in full" (3 / 3 / 4). It would let
the port keep `GoogleMaps.Place`, in an interface nothing implements.

**6. `kogo` and `kogo.` packages are the prelude's.** Not live.
Recommended.

Spec 2: "A program's package is not `kogo` or under it." Guards
against a program making its own names ambient in every file, which is
what imports exist to prevent. Size 4 / 2 / 2.

**7. Accept four holes, each stated in a sentence, with no code.**
None is live; each needs a declaration nobody has written. One audit
issue holds them until one bites.

- Rule 6 against the prelude, spec 2: "A package's own type or val is
  meant before one of package `kogo` or a built-in type, as in Kotlin."
  Refusing instead is 5 / 2, and then each name added to the prelude
  can break a program.
- Rule 17, spec 2: "Two names of a package that differ only in the
  case of the first letter may be one name in Go; `kogo emit` refuses
  them." It belongs in `kogo check` by the only-gate decision: 15 / 4
  when wanted.
- Rule 18, spec 3.5: "An alias stands where a type is written; a value
  is built, and a constant or a variant named, by the type's own name."
- Rules 16 and 19, spec 6: "A local hides a name declared outside it,
  an outer block's local and a type's name included, as in Kotlin."
  Nothing on the imports of rule 19.

Size 0 / 0 / 8.

**8. Five messages that do not say what to change.** No language
change, so one issue once 1 and 3 are ruled. Size 25 / 10 / 0.

- `q.a.P` in code: "import q.a.P and write P" (rule 5).
- `import q.a.O.Ok`: "import q.a.O and write O.Ok" (rule 3).
- An extension not imported: "add import q.a.show" (rule 14).
- Two types of one name: "this is a String where a String is wanted"
  becomes each with its package (rule 6).
- A constructor through an alias: "PRow is not in scope: add import
  q.a.PRow", though it is imported (rule 18).

**No proposal on import cycles.** Rule 12 is stated, enforced and met
by the port. What it costs the migration is in section 2.

**Rows for section 14**, one for each "yes":

| Kotlin habit | In Kogo | From |
| --- | --- | --- |
| two funs of one name imported from two packages | `import ... as` on one | 1 |
| an import of a name the file's own package declares | `import ... as`, or rename one | 2 |
| `a.b.Name` written in code | import it and write `Name` | 3 |
| `import a.b.Outer.Inner` | `import a.b.Outer`, and write `Outer.Inner` | 3 |
| two `private val`s of one name in two files of a package | rename one | 4 |
| a type nested in an interface as a namespace, `GoogleMaps.Place` | a top-level type, `GoogleMapsPlace` | 5 |

If 1 is answered no, its row is "`import ... as` | rename one of the
two". All eight answered yes come to about 90 / 54 / 40.

## Appendix: the commands

In `~/Dev/cusoon2-tasks`, after `git fetch`, with `R=origin/renew` and
`K=backend/src/main/kotlin`. `T` matches a top-level type:

```
T='^(@\w+(\([^)]*\))? )?((public|private|internal|data|sealed|enum|value|abstract|open|annotation|fun) )*(class|interface|object|typealias) \w+'
```

- A, 88 and 25: `git ls-tree -r --name-only $R $K | grep -c '\.kt$'`;
  `git grep -h '^package ' $R -- $K | sort -u | wc -l`
- B, 26 and 19: `git grep -hE '^import .+ as ' $R -- $K | wc -l`; the
  same through `grep -c '\.jooq\.'`. Forced or not: each of the 26
  read in its file, with `git grep -n '\b<Name>\b'` there.
- C, 1: `git grep -hE '^import .*\*' $R -- $K | wc -l`
- D, 2: `git grep -nE '\b(net\.gtod|java|javax|kotlin|kotlinx|org|com|io|arrow)\.[a-z]\w*(\.[a-z_]\w*)*\.[A-Z]\w*' $R -- $K | grep -vE ':[0-9]+:\s*(import|package|//|\*|/\*)'`
- E, 9: `git grep -E "$T" $R -- $K | sed -E 's#^[^:]+:(.*)/[^/]+\.kt:.*\b(class|interface|object|typealias) (\w+).*#\3 \1#' | sort -u | cut -d' ' -f1 | uniq -d`
- F, 34 and 6: `git grep -hE '^\s+(@\w+(\([^)]*\))? )?((private|internal|data|sealed|enum|abstract|open) )*(class|interface|object) \w+' $R -- $K | wc -l`;
  `git grep -hE '^\s+((private|internal) )?companion object' $R -- $K | wc -l`.
  Each of the 34 was then read for what holds it.
- G, 40 and 17: `git grep -hoE '^import net\.gtod\.cusoon\.jooq\.cusoon\.(tables\.pojos|enums|udt\.pojos)\.\w+' $R -- $K | sed 's/.*\.//' | sort -u > rows`;
  `git grep -hE "$T" $R -- $K | sed -E 's#.*\b(class|interface|object|typealias) (\w+).*#\2#' | sort -u > types`;
  `comm -12 rows types | wc -l`
- H, 15: `git grep -hE '^((internal|private|data) )*object \w+' $R -- $K | wc -l`
- I, 85, 33 and 10: `git grep -hE '^private (suspend |inline )*fun ' $R -- $K | wc -l`;
  with `'^private (const )?val '`; with
  `'^private ((data|sealed|enum|value|abstract|open) )*(class|interface|object|typealias) '`
- J, none: `git grep -E '^private ((const|data|sealed|enum|value|abstract|open|suspend|inline) )*(class|interface|object|typealias|val|fun) ' $R -- $K | sed -E 's#^[^:]+:(.*)/([^/]+)\.kt:private ((const|data|sealed|enum|value|abstract|open|suspend|inline) )*(class|interface|object|typealias|val|fun) (<[^>]*> )?([A-Za-z0-9_<>?, ]+\.)?(\w+).*#\1 \5 \8 \2#' | sort -u | cut -d' ' -f1-3 | uniq -d`
- K, the cycle: `git grep -E '^(package|import) ' $R -- $K | python3 cycle.py`,
  which prints "25 packages; 19 on one cycle; 459 names and 629 import
  lines inside it; 52 names point up at best". jOOQ's generated
  packages are left out. `cycle.py` is below.
- L, 28: E's `git grep` and `sed`, with `\1 \3` for `\3 \1`, through
  `sort -u` into `t`; then
  `{ git grep -E '^([a-z]+ )*fun [^(]*\w+\(' $R -- $K | sed -E 's#^[^:]+:(.*)/[^/]+\.kt:([a-z]+ )*fun [^(]*\b(\w+)\(.*#\1 \u\3#'; git grep -E '^([a-z]+ )*val ' $R -- $K | sed -E 's#^[^:]+:(.*)/[^/]+\.kt:([a-z]+ )*val ([A-Za-z<>?, ]+\.)?(\w+).*#\1 \u\4#'; } | sort -u > f`;
  `comm -12 t f | wc -l`. Two of one kind, none:
  `awk '{print $1, tolower($2)}' t | sort | uniq -d`, and the same of `f`.
- M, none: in Kogo,
  `grep -hoE '^(external |sealed |data |enum )*(class|object|interface) \w+' prelude/kogo.kg prelude/either.kg check/core.kg | awk '{print $NF}' | sort -u > pre`,
  the nine built-in types of spec 3 added; `comm -12 pre types`. The fun
  and the two extensions: `git grep -nE '^fun (Area\.contains|PDPageContentStream\.list|randomString)\(' $R -- $K`
- N, with `P` PR 1576's branch: `git ls-tree -r --name-only $P kogo/port | grep -c '\.kg$'`;
  `git grep -hoE '^((private|internal) )?(data class|sealed interface|enum class|interface|typealias|data object) \w+' $P -- kogo/port | awk '{print $NF}' | sort | uniq -d`;
  `git grep -hE '^\s+(data |sealed |enum )*(class|object|interface) \w+' $P -- kogo/port | wc -l`;
  `git grep -nE '^import cusoon\.(authentication\.passkeys|model\.setDriverShare)$' $P -- kogo/port/rpc/ops`

`cycle.py`:

```python
import sys, re, collections
pkg, deps, lines = {}, collections.defaultdict(set), collections.Counter()
rows = [re.match(r'[^:]+:([^:]+):(package|import) ([\w.]+)', l).groups() for l in sys.stdin]
for f, kind, name in rows:
    if kind == 'package': pkg[f] = name
ps = set(pkg.values())
for f, kind, name in rows:
    if kind == 'import' and '.jooq.' not in name:
        to = max((p for p in ps if name.startswith(p + '.')), key=len, default=None)
        if to and to != pkg[f]: deps[pkg[f], to].add(name); lines[pkg[f], to] += 1
def reach(p, seen):
    for a, b in deps:
        if a == p and b not in seen: seen.add(b); reach(b, seen)
    return seen
up = {p: reach(p, set()) for p in ps}
g = sorted(max(({q for q in up[p] if p in up[q]} for p in ps), key=len))
w = [[len(deps.get((a, b), ())) for b in g] for a in g]
best = {0: 0}  # the packages laid below -> the fewest names that point up
for s in range(1 << len(g)):
    for v in range(len(g)):
        if not s >> v & 1:
            c = best[s] + sum(w[v][u] for u in range(len(g)) if u != v and not s >> u & 1)
            best[s | 1 << v] = min(best.get(s | 1 << v, c), c)
print(len(ps), 'packages;', len(g), 'on one cycle;', sum(map(sum, w)), 'names and',
      sum(lines[a, b] for a in g for b in g), 'import lines inside it;', best[(1 << len(g)) - 1], 'names point up at best')
```
