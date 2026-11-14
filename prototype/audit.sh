#!/bin/bash
# Flags constructs outside spec.md in .kg files. Heuristic; every hit is read by hand.
cd "$1" || exit 1
chk() { local label=$1 re=$2; local out; out=$(grep -nE -- "$re" *.kg | grep -vE '^\S+:[0-9]+:\s*(//|\*|/\*)'); [ -n "$out" ] && { echo "## $label"; echo "$out"; }; }
chk '!!' '!!'
chk 'try/catch/throw' '\b(try|catch|finally|throw|runCatching)\b'
chk 'class forms' '^\s*((private|internal|open|abstract) )*(class|object|companion)\b|\bobject\s*:'
chk 'inheritance' '\b(open|abstract|override) (class)|\)\s*:\s*\w+\('
chk 'apply/run/with/takeUnless' '\.(apply|run|takeUnless)\s*\{|\bwith\s*\('
chk 'labelled return' 'return@|@\w+\s*\{'
chk 'lateinit/var top-level' '\blateinit\b|^(private |internal )?var\b'
chk 'coroutines/seq/mutable' '\b(suspend|launch|async|Flow|Sequence|generateSequence|asSequence|sequence|mutableListOf|MutableList|toMutableList|mutableMapOf|mutableSetOf|arrayOf|Array<)\b'
chk 'java/jOOQ' '\bjava\.|\borg\.jooq|\bDSL\.|\bctx\b|::class\.java|\.store\(\)|\.into\('
chk 'jvm time/ids' '\b(OffsetDateTime|LocalDateTime|UUID|BigDecimal|Locale)\b|String\.format'
chk 'reflection/annotations' '::class\b|^\s*@\w+'
chk 'inline/reified/operator/infix/vararg' '\b(inline|reified|operator|infix|vararg|tailrec|crossinline|noinline)\b'
chk 'receiver lambdas' '\w+\.\(\)\s*->'
chk 'Arrow outside subset' '\b(Option|Some|None|NonEmptyList|zipOrAccumulate|recover|nullable\s*\{|Ior|Validated)\b'
chk 'println/readln' '\b(println|print|readln|readlnOrNull)\b'
chk 'string concat into sql / sql stored' '(val|var) \w+\s*=\s*sql"|sql"[^"]*"\s*\+'
chk 'wildcard/alias import' '^import .*(\*| as )'
chk 'data class body' '^data class .*\{\s*$'
chk '=== / is not' '==='
