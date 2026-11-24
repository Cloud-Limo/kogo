# Porting a Kotlin module

A port aims to behave exactly as its Kotlin did (`doc/design.md`,
"Ports"); these rules say how that is reached and shown, and what
happens to a difference.

**Transliterate by default.** Keep the Kotlin's names, order and
structure wherever Kogo has the construct, so the two read side by side.

**Keep the Kotlin's shape.** A port is no longer than its Kotlin and
reads no worse. Kogo has most of Kotlin's short forms (`?.let`, `?:`,
`also`, an extension fun or val, a `when` on a value, a `for` over a
range), so a ladder of statements where the Kotlin had one expression is
a mistake in the port. The handoff lists every declaration not the
Kotlin's by name, kind and receiver (a helper added, an extension or a
property made a fun, a named val written in) and every function three or
more lines longer than its Kotlin, each with one cause: a row of
`spec.md` section 14, a name section 12 lacks, or a design. The Code
review reads each ported function beside its Kotlin, checks each cause
against the spec, and tries the Kotlin's form with `kogo check` where
the port's is longer or shaped otherwise; a cause the spec does not bear
out, or a shorter form that passes and means the same, is a Fix. The
list does not stand in for the reading: of the eleven such places an
audit of cusoon2's ports found (22 Nov 2026), it would have named nine,
and missed a ladder inside a function no longer than its Kotlin. SQL is
read so against the jOOQ: a value the Kotlin computed stays computed
where section 12 has the name, so `now.minusDays(28)` is
`${now.minus(Duration.ofDays(28L))}`, not `($now::timestamptz at time
zone 'UTC' - interval '28 days') at time zone 'UTC'`. The port keeps the
Kotlin's comments and adds beside the code only its marks and the
records a Ruling asks for; what the port went through (the comparison, a
DEV number, how the Java client did it) goes in the handoff. A comment
beyond these is a Fix. A port's Budget counts its code and imports, not
its comments; the handoff gives its comment lines on a line of their
own, and how many of them are the Kotlin's.

For one, the Kotlin

```kotlin
model.cachedGooglePlace(placeId)?.let {
    return cachedPlace(it)
}
return super.getPlace(placeId).also { model.storeGoogleAnswer(placeId, it) }
```

was ported as an `if`/`else` of eight lines inside `either { }`, where
this passes `kogo check` in two:

```kotlin
cachedGooglePlace(tx, placeId)?.let { cachedPlace(it) }
    ?: getPlace(google, placeId).bind().also { storeGoogleAnswer(tx, clock, placeId, it) }
```

**Follow a design where one gives the form.** Where a design gives the
port a form `kogo/run.sh` cannot see (the list's rows, a gate, a
step's signature, where a mark sits: cusoon2's `doc/plans/gates.md`),
the port follows the design and the Kotlin is not reshaped first
(`doc/roadmap.md`, "How it moves"). The file that departs says so in
one line naming the design. The side-by-side run is unchanged.

**Mend the Kotlin first, never the port.** Not assuming the Kotlin
perfect bites before the port; porting it accurately, in it. Of an
oddity the port asks what a person meets, against how the code encodes
it: an encoding may change, what a person meets is ported. Where what
the Kotlin answers would force something weaker or uglier into the
port (a gate that never refuses, an `ungated` row, a hack, a new name
in Kogo), the handoff's Next step proposes a change to the Kotlin, or
the port stops and says so where it cannot go on without it, each with
the change's size and who would see it. The change is made in the
Kotlin first, as its own small piece with its test, and then ported as
it stands; never in the port alone, since the side-by-side run is the
check and a port that differs on purpose has nothing beside it. Any
other oddity the port keeps, and moves on.

For one, the storefront's `bookerProfile` answers a visitor null, and
the page's only use of the null is "No booker. Please log in.": ported
as it is, its row sits under a gate that never refuses, so the change
is proposed in the Kotlin. `pastPassengers` answers a visitor an empty
list, which is what the wizard really does, and is ported as it is.

**Read each pattern as Go's.** A `Regex`'s pattern is Go's (`spec.md`
section 12), so the port reads each one against that section's list and
writes it in a form Go reads as the Kotlin meant: where a vertical tab
can come, Kotlin's `\s` is `[\s\v]`. The Code review reads each pattern
so.

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
beyond those accepted and owed, accepted by Greg or, for one only the
comparison sees, by the port's Decided under "Weigh a write-around";
a module does not replace its Kotlin without such a run, nor with a
difference owed.

A newly ported area's cases come from a generated world, beside those
written by hand, since each set sees bugs the other misses (DEV-1162):
cusoon2's `go/gen` writes them from a fixed seed on every run, each
flag the area reads drawn both ways, each list of none, one and two,
each limit on it and a step either side, and a limit drawn with the
state that reaches it; `kogo/run.sh` runs both sets, or one alone by
`KOGO_CASES`. `0 differ` does not say the cases reached anything, so
the handoff gives the reach `go/compare` prints under its count: of
each operation's generated calls, how many got past its door, and of
those drawn for a state, how many reached it. Then plant one bug in
the port (a cent off, a dropped `where`, a swapped share), see the
comparison exit 1 and name the right case, and say so in the handoff:
rule 6 applied to the comparison itself.

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

**Ask Postgres for a moment.** A port builds no moment from a date, a
time and a zone. Where the Kotlin does (`ZonedDateTime.of`,
`withLaterOffsetAtOverlap`), the port asks Postgres, which holds the
one copy of the zone rules the stored rows are read by: `kogo.time`
gains none of those names, and spec 14's row on `LocalDateTime` stands
(DEV-1163).

**Weigh a write-around before writing it.** Before writing round a name
in more lines than the Kotlin took, most often a hand-built copy of a
Java library's function, the port asks who would see the difference if
the short form were written: a customer, an op, a stored row, or the
answer of an outside service. If nobody but the comparison, it writes
the short form, leaves that difference out of the comparison on its
case, and puts it under Decided with the form not taken; rule 6 of
`AGENTS.md` lets Decided loosen the comparison so, and the Code review
checks the claim of who sees it. If somebody, the
handoff names the prelude function that would do it, of either kind
above: the prelude may grow to keep a port short. Until it is built
the port writes the shortest thing that keeps what is seen, under
Decided with its size and who sees it. A copy of a library's function
that Decided does not name is never written. A swap for a form as long
(`requireNotNull` for `!!`) is not weighed.

For one, `LatLng.toUrlValue` is Java's `String.format("%.8f,%.8f")`,
rounding half up to eight places. cusoon2's first port of it (DEV-1052)
was `eight()`, ten lines of digit arithmetic, where one line,
`"${plain(at.lat)},${plain(at.lng)}"`, was the short form: only the
stand-in's lookup by text and the comparison's check of the calls sent
told them apart.

**The handoff reports:** the cusoon2 commit the port matches; each
declaration not the Kotlin's and each function three or more lines
longer, with its cause; each departure with its case, and under Decided
each write-around weighed, with its size, who sees the difference and
the form not taken; "N of M statements run" and each not reached; the
run's cases, differences, any accepted ones and each one owed with its
issue.
