# Lists: one mechanism

Agreed with Greg in discussion on 17 Nov 2026, beside `doc/gates.md`
and `doc/work.md`, which hold each list's own design, and spelt on
18 Nov 2026 (DEV-894, "How a list is written" below). The `api`
list's head and rows are parsed and checked, and in spec 15.1 and
15.2 (DEV-950), and served, spec 15.3 (DEV-961); the other lists
are not built, and for them the spec stands as written.

## What the lists share

The designs have five lists: `api`, `commands`, `job`, `effect` and
`work`. Each was designed in its own section, and they
are one idea:

- a list names ordinary top-level functions and gives each a role;
- the checker fits their types end to end: what one stage gives is
  what the next takes;
- the emitter writes a table, and hand-written Go calls the functions
  in order;
- a command prints the list for a reviewer.

Kogo code never calls the runtime: the runtime calls Kogo, through a
table. So the lists are one of spec 1's four deliberate deviations
from Kotlin: the `sql` literal, bindings, the lists and `action fun`.

## One rule for every shape

A function the runtime calls takes, in this order:

1. the handle it runs in, if it has one: a `SerializableTx`, a `Tx`
   or a `Db`;
2. the environment;
3. what it is about: the request, the proof, the row, the key, the
   claim, the alert;
4. what it works on: the input, the prepared value, the result, the
   reason.

## The roles

| Role | List | Runs in | After the environment it takes | Gives |
| --- | --- | --- | --- | --- |
| check | `api` | a `Tx`, reading only, rolled back | the request | nothing, or a refusal |
| gate | `api` | a `SerializableTx` or `Tx`, reading only | the request; the input, if it reads one | a proof, or a refusal |
| work | `api` | the proof's transaction | the proof; the input | the answer |
| outside | `api` | nothing (an `action fun`) | the proof; the input | nothing, or the answer where there is no work |
| ungated | `api` | a `SerializableTx`, `Tx` or `Db` | the request; the input | the answer, or a `Reply` |
| command | `commands` | a `Db` | the input | words |
| next | `job` | a `Tx` | | a row, or none |
| run | `job` | that `Tx` | the row | |
| enter | `effect` | a `Tx` | the claim | |
| prepare | `effect` | that `Tx` | the row | a prepared value, or a refusal |
| act | `effect` | nothing (an `action fun`) | the key; the prepared value | an `Act` of the result |
| record | `effect` | a `Tx` | the row; the result | nothing, or a reason it could not record |
| failed | `job`, `effect` | a `Tx` | the row; why | |
| recovery | `job`, `effect` | that `Tx` | the row; why | words, or none |
| alert | `work` | nothing (an `action fun`) | the alert | |

A row here is the whole of a role's shape. What is a list's own stays
in its own design: that a gate only reads, the runner's columns, what
is owed.

## What follows from it

- **`enter` takes the environment.** It was the one role that runs in
  a transaction and was not handed it:
  `bindTenantOf(tx: Tx, env: Env, claim: Claim)`.
- **The spec states lists once**: one section with the rule and the
  table, and in each list's section only what is its own.
- **The checker is driven by the table**: one piece of code that fits
  stages, with the fifteen rows as data, and no rule written by hand
  for each list.
- **The next list is a row**, or a few, and not a section.
- **The spelling has at most two forms**: a list of rows, for many
  small entries (`api`, `commands`), and a block of named parts, for
  one entry (`job`, `effect`, `work`).

No role, shape or rule of the two designs changes but `enter`. The
checker is no larger than the separate rules would have been; the
spec is smaller, by perhaps one section of 60 lines where five would
each have explained shapes.

## How a list is written

Agreed with Greg on 18 Nov 2026 (DEV-894; `decisions.md`). A list is
bare words, with no `::` or colons, and kotlinc refuses it, so no
Kotlin habit applies to one (`doc/design.md`, principle 3).

```kotlin
api booker checkedBy fromOurPage {
    setBookingNotes           gatedBy setBookingNotesGate              // a gate of its own
    drivers                   gatedBy admin                            // a shared gate
    updateBooker              gatedBy updateBookerGate after renameLogins
    startPasskeyRegistration  gatedBy booker                           // an action fun: no transaction
    whoAmI                    ungated
}

api hooks unchecked {
    recordDeliveryFailure  ungated
}

commands admin {
    recordAbn
    setPayid
}

job notifyBooker {
    next    nextBookerNotice
    run     sendBookerNotice
    failed  bookerNoticeFailed
}

work daemon alertedBy opsAlert {
    chargeCard
    notifyBooker
}
```

- **The outside step is spelt `after`**, which says the order, and
  is an `action fun`. Not taken: the chain as the list (`gate -> step
  -> work`), since the words carry the checker's rules and a chain of
  one would be an unmarked ungated row. The outside step is out since
  21 Nov 2026 (DEV-1039, `decisions.md`).
- **There is no bare `outside`.** A gated operation that is an
  `action fun` runs with no transaction and its gate once, as a `Db`
  means no transaction in spec 15.3: the list only labels the gates.
- **A list with no check says `unchecked`**, as a row says `ungated`.
  Its Go type cannot be mounted for a browser.
- **A command's name is its function's name**, run as `cusoon2
  recordAbn --gstRegistered true`. The list's name is what `main`
  calls it by.
- **Entries are separated by a comma, a new line or both**, as
  Kotlin separates a collection's, with a comma allowed after the
  last (spec 15.1; Greg, 19 Nov 2026, DEV-947). A row of more than
  one word stays on one line. The work list names its alert in its
  head, `alertedBy`, and its queues as a list of names. A job's and
  an effect's parts are not a list: they stand one a line, with no
  commas.
- **An effect's and a job's lines are in the order they run**, and
  the checker refuses any other: settings `find`, `fair`, `soonest`,
  `attempts`; steps `enter`, `prepare`, `act`, `record`; on failure
  `failed`, `recovery`; a job `next`, `run`, `failed`, `recovery`.
- **Each form gets one example in `examples/`**, compiled by the
  tests, when the lists are built: the examples are the agents'
  cheat-sheet, and a role's shape stays in the spec and the
  checker's table.

## Asked for: a deep dive

Greg, 17 Nov 2026, on agreeing to this: he wants a deep-dive
assessment of what the lists are trying to do, a dispassionate look
at how they do it, and other ways of doing the same thing: ways that
are more explicit, so with less magic, and ways with about as much
magic, expressed differently. Done: `doc/lists-assessment.md`
(DEV-901). Greg chose on 17 Nov 2026 to keep the lists
(`decisions.md`), and routes went to Go, which made six lists five.

Starting points for it, none of them weighed:

- **The application writes the pipeline as ordinary code**, calling
  the gate and then the work itself, with the runtime as a library it
  calls and not a framework that calls it.
- **A role as a function of an interface**, each operation a
  `data object` that implements it, so that Kotlin's own types fit
  the stages and nothing but the list is Kogo's.
- **A marker on each function** that says its role, with no list.
- **Routes in Go, in the application's `main`**, each calling an
  ungated operation, as `doc/gates.md` weighed and refused while Go
  was to go unread. cusoon2 has two. Taken on 17 Nov 2026
  (`doc/gates.md`, "Routes that are not operations").
- **What the list buys that these do not**: the table on one screen,
  the printed file, and that nothing is an operation by accident.
