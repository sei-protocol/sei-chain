# Sei Config Registry

The single declaration point for a stable configuration key. A key enters once, as a field on
its owning package's section struct, and everything a node needs in order to resolve it derives
from that one registration.

`doc.go` states the rules as the package's own documentation. This file says what problem the
package exists for, what it deliberately does not do, and how to add a section.

## Three Statements Per Key

A configuration key in this tree needs three things written down, and today they are written
separately:

1. the reader's lookup, a dotted string at the point the value is consumed,
2. a flag binding, if an operator can pass the value on the command line,
3. an environment spelling, derived by upper-casing and substituting separators.

Nothing ties the three together, so they drift independently. A rename can move the reader and
leave the flag behind, and the result is a key an operator sets that reaches nothing. Several
such mismatches already exist and are pinned by the characterization suite in
`testutil/configtest`, which is how they were found rather than reported.

Registering a section states the key once. The dotted identity, the environment spelling and the
read site all come from the struct field and its `mapstructure` tag, so editing the tag moves all
three together and there is no second place to forget.

Nothing here is enforced by the compiler. The tag is a string literal, so a rename is still a rename
of text; what changes is that there is one occurrence of it instead of three. Step 4 of `Adding a
Section` below is what turns that into a failure when the one occurrence and the reader disagree.

## Declaration

A section registers the struct its reader already uses:

```go
func init() {
	registry.RegisterSection(SectionName, &Config{}, default)
}
```

The second argument is a prototype, read for its fields and tags and never for its values. It
is deliberately the reader's own struct rather than a copy: a second struct would be a second
statement of the same key set, and the two would disagree the first time somebody edited one.

Where the upstream type carries fields configuration cannot address, a section may declare
against a struct written for the purpose instead. That is the exception, and it should carry a
comment saying why the reader's own struct could not serve, because a purpose-written struct is
the second statement this package exists to avoid and is only worth it when the first cannot be
used.

`default` answers per node mode, because a validator and a seed node do not default alike.

## Defaults

Defaults are not state. They live in the binary, may change between releases, and never mutate
a configuration file or require a migration.

The consequence worth stating plainly: a written value is a commitment the system never
rewrites, and an absent key tracks whatever default the running binary carries. An operator who
writes a value keeps it across an upgrade. An operator who writes nothing follows the binary.

## Resolution

`Resolve` reduces a set of named layers to one value per declared key, in the order `Precedence`
declares, and records which layer each value came from.

The provenance is the reason this exists rather than a map merge. Merging layers inside one
source produces the right value and loses where it came from, so an operator whose file says one
thing and whose node does another has no way to find out why. Here the winning layer is
recorded, so a diagnostic can name it.

Precedence comes from `Precedence` rather than from the order layers are passed in, so a caller
cannot change the answer by reordering its arguments.

## What This Package Is Not

- **Not the boot.** `Resolve` answers for declared keys only. What a running node reads stays a
  source carrying every resolved key, whether a section declares it or not. This serves a
  diagnostic or an authoring check.
- **Not a file format.** Nothing here reads or writes a configuration file.
- **Not a validator.** A section may state rules about its own values; this package does not
  invent any.
- **Not wired.** No section is registered by this package and no reader is migrated onto it.

## Registration Never Panics

A registration this package cannot use is recorded as a `Defect` and the section is not
registered. `Defects` returns them, and a test turns them into a failure.

Panicking would run during the package initialisation of something every feature imports, so it
would take down every `seid` invocation including `--help`, and it would turn a mistake that a
compiler or a test can catch into a fleet-wide incident.

## Adding a Section

1. Give the section a name, and use it as the first segment of every key it declares.
2. Register the struct the reader already uses, with a per-mode default.
3. Assert the registration produced no `Defect`.
4. Hold the derived key names against the reader, so a key that reaches nothing fails.

Step 4 is the one that earns the rest. A declaration nothing checks against the reader is a
second statement of the key set, which is the problem this package exists to remove.
