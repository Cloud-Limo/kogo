# Porting a Kotlin module

A port aims to behave exactly as its Kotlin did (`doc/design.md`,
"Ports"); these rules say how that is reached and shown, and what
happens to a difference.

**Transliterate by default.** Keep the Kotlin's names, order and
structure wherever Kogo has the construct, so the two read side by side.

**Follow a design where one gives the form.** Where a design gives the
port a form `kogo/run.sh` cannot see (the list's rows, a gate, a
step's signature, where a mark sits: cusoon2's `doc/plans/gates.md`),
the port follows the design and the Kotlin is not reshaped first
(`doc/roadmap.md`, "How it moves"). The file that departs says so in
one line naming the design. The side-by-side run is unchanged.

**Mark each departure outside SQL.** Where Kogo lacks the construct
(`spec.md` section 14, or a name section 12 does not list), the port
says why in a one-line comment, as `// generateSequence becomes a while:
spec 14`; adding a name to the spec is not the port's to do ("Add what
the spec lists" says whose it is). The handoff lists each and names the
case of the side-by-side run that reaches it. A service the Kotlin had
by injection (its database context, a clock) becomes a parameter (`tx`,
`db`, `clock`; spec section 7) with no mark.

**Cover SQL rather than mark it.** Every statement departs, since jOOQ's
builder becomes a literal, so its rule is coverage instead: the
side-by-side script runs every statement written in the port at least
once (one execution of a statement in a loop is enough), and one that
can go two ways is run both ways: an upsert as an insert and as an
update; a conditional `update` or `delete` where it changes a row and
where it changes none; a read whose call can return nothing (`query`,
`queryOne`, `exists`) where it finds a row and where it finds none.
`querySingle` is one-way. A statement reached only on a refusal path
counts as run when a case takes that path; its rows roll back (spec
15.3), so the comparison sees the refusal and the rows that remain. The
handoff gives "N of M statements run" and names each not reached.

**The side-by-side run is the gate.** Results are compared exactly and,
where there is SQL, so is every row either side touched, from identical
copies of a database. Ids and times each side makes are paired, not
pinned (DEV-853). A difference is never passed over in silence. It is a
mistake in the port, a fault in Kogo, or a corner where Kogo differs,
and each is fixed, the last by matching the prelude (principle 3) or by
a function of Kogo's own used as a marked departure; or Greg accepts it,
recorded as "Ports" has it, and the comparison names the accepted cases
one by one; or Greg decides it is owed: the port leaves out something
the Kotlin does, because Kogo cannot yet do it. The handoff names the
gap and leaves it to him. Record what is left out, the issue that will
close it and the cases it covers, which the comparison names and
forgives until that issue closes. A port passes at zero differences
beyond those accepted and owed, and a module does not replace its Kotlin
without such a run, nor with a difference owed.

For each newly ported area, plant one bug in the port (a cent off, a
dropped `where`, a swapped share), see the comparison exit 1 and name
the right case, and say so in the handoff: rule 6 applied to the
comparison itself, since nobody has measured what its pairing of ids
and times forgives.

**Add what the spec lists.** A name the port needs that the spec lists
and the prelude lacks is added to the prelude (DEV-875), not written
around. A name the spec does not list is one of two kinds. One Kotlin
has (its standard library, or `java.time` for a type of `kogo.time`),
wanted with Kotlin's meaning, the port reports in its handoff and the
lead approves; it is then added to section 12's list and the prelude
together, with a case in `kotlin/builtins` where that check takes one.
One that would be Kogo's own, or would differ from Kotlin's in ordinary
use, is Greg's. Until either is built the port writes round it as a
marked departure. An operator on a prelude type is such a name: `a < b`
is `compareTo` and `a + b` is `plus` (spec section 6).

**The handoff reports:** the cusoon2 commit the port matches; each
departure with its case; "N of M statements run" and each not reached;
the run's cases, differences, any accepted ones and each one owed with
its issue.
