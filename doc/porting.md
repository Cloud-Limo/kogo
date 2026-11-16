# Porting a Kotlin module

A port behaves exactly as its Kotlin did (`doc/design.md`, "Two
aims"). These are the rules for how it gets there and how that is shown.

**Transliterate by default.** Keep the Kotlin's names, order and
structure wherever Kogo has the construct, so the two read side by side.
Idiomatic Kogo is not the goal of a port.

**Mark each departure outside SQL.** Where Kogo lacks the construct
(`spec.md` section 14), each departure gets a one-line comment in the
port saying why, as `// generateSequence becomes a while: spec 14`. The
handoff lists each, and names the case of the side-by-side run that
reaches it.

**Cover SQL rather than mark it.** Every statement departs, since
jOOQ's builder becomes a literal, so SQL is not marked. Its rule is
coverage: the side-by-side script runs every statement in the port at
least once, and a statement that can go two ways is run both ways: an
upsert as an insert and as an update, a conditional `update` or
`delete` where it changes a row and where it changes none. The handoff
gives the count, "N of M statements run", and names each not reached.

**The side-by-side run is the gate.** Results are compared exactly, as
"MVP steps" in `doc/design.md` has it, and where there is SQL so is
every row either side touched, from identical copies of a database. Ids
and times each side makes are paired, not pinned (DEV-853). A module
does not replace its Kotlin without a run with zero differences.

**Add what the spec lists.** A name the port needs that the spec lists
and the prelude lacks is added to the prelude (DEV-875), not written
around.

**The handoff reports:** the cusoon2 commit the port matches; each
departure with the case that reaches it; "N of M statements run" and
each statement not reached; the run's result, cases and differences.
