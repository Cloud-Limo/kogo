# Lists: one mechanism

Agreed with Greg in discussion on 17 Nov 2026, beside `doc/gates.md`
and `doc/work.md`, which hold each list's own design. It is not built
and not in `spec.md`: until it is, the spec stands as written.

## What the lists share

The designs have six lists: `api`, `routes`, `commands`, `job`,
`effect` and `work`. Each was designed in its own section, and they
are one idea:

- a list names ordinary top-level functions and gives each a role;
- the checker fits their types end to end: what one stage gives is
  what the next takes;
- the emitter writes a table, and hand-written Go calls the functions
  in order;
- a command prints the list for a reviewer.

Kogo code never calls the runtime: the runtime calls Kogo, through a
table. So spec 1 keeps its count of three things Kogo adds to Kotlin,
with one word changed: the `sql` literal, bindings, and lists.

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
| gate | `api` | a `SerializableTx` or `Tx`, reading only | the request; the input, if it reads one | a proof, or a refusal |
| work | `api` | the proof's transaction | the proof; the input | the answer |
| outside | `api` | nothing (`@Acts`) | the proof; the input | nothing, or the answer where there is no work |
| ungated | `api` | a `SerializableTx`, `Tx` or `Db` | the request; the input | the answer, or a `Reply` |
| route | `routes` | a `Db` | the request | a `Response` |
| command | `commands` | a `Db` | the input | words |
| next | `job` | a `Tx` | | a row, or none |
| run | `job` | that `Tx` | the row | |
| enter | `effect` | a `Tx` | the claim | |
| prepare | `effect` | that `Tx` | the row | a prepared value, or a refusal |
| act | `effect` | nothing (`@Acts`) | the key; the prepared value | an `Act` of the result |
| record | `effect` | a `Tx` | the row; the result | |
| failed | `job`, `effect` | a `Tx` | the row; why | |
| recovery | `job`, `effect` | that `Tx` | the row; why | words, or none |
| alert | `work` | nothing (`@Acts`) | the alert | |

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
- **The spelling has at most two forms** (DEV-894 decides it): a list
  of rows, for many small entries (`api`, `routes`, `commands`), and
  a block of named parts, for one entry (`job`, `effect`, `work`).

No role, shape or rule of the two designs changes but `enter`. The
checker is no larger than the separate rules would have been; the
spec is smaller, by perhaps one section of 60 lines where six would
each have explained shapes.

## Asked for: a deep dive

Greg, 17 Nov 2026, on agreeing to this: he wants a deep-dive
assessment of what the lists are trying to do, a dispassionate look
at how they do it, and other ways of doing the same thing: ways that
are more explicit, so with less magic, and ways with about as much
magic, expressed differently. Until it is done the mechanism stands
as it is here.

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
  was to go unread. cusoon2 has two, and it would take the `routes`
  list, `Response` and `Secret` out of the language.
- **What the list buys that these do not**: the table on one screen,
  the printed file, and that nothing is an operation by accident.
