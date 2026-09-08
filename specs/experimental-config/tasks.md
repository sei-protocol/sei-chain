# Experimental Configuration Tasks

Each task is a slice that ships on its own. The requirement IDs are what its tests
name.

## T1 — Reserve the Prefix and Refuse a Section Claiming It

**Covers** EXP-1.

The registry refuses a section named `experimental`, the way it already refuses a name
carrying a dot. Reported as a defect rather than a panic, because registration runs
during package initialisation.

**Done when** a registration claiming the name produces a defect naming the reserved
prefix, and no key is derived from it.

## T2 — Register an Experimental Group

**Covers** EXP-2, EXP-3, EXP-4, EXP-5, EXP-6.

`RegisterExperimental(owner, prototype, defaults)`. Keys derive from the prototype's
tags and land at `experimental.<owner>.<tag>`. A second registration claiming the same
owner is a defect. The section verbs cannot reach the prefix, which T1 already
guarantees.

**Done when** a registered group's keys appear in the declared set under the right
names, a duplicate owner is a defect, and an owner carrying a dot is a defect.

## T3 — Resolve and Deliver an Experimental Key

**Covers** EXP-7, EXP-8, EXP-9.

A registered group resolves like a section: the written value where the file states
one, the registered default where it does not. Delivery is the one the group declares.

EXP-9 is measured rather than built. A committed key and an experimental key with
matching tags, and the committed one still answers its own value after the
experimental one is written.

**Done when** a written experimental value reaches a reader through a real boot, an
absent one reads as the registered default, and the committed key beside it is
unmoved.

## T4 — Report an Unknown Experimental Key Without Failing

**Covers** EXP-14, EXP-15, EXP-16.

`Resolved` gains `UnknownExperimental`, split from `UnknownInFile`. The boot reports
those keys and applies everything else. `seid config check` reports them and exits
zero. A key outside the prefix that nothing declares goes on failing.

This is the requirement that lets a controller write a file for a newer binary and
roll it to nodes that have not upgraded.

**Done when** a `sei.toml` carrying an undeclared experimental key boots, the check
exits zero and names the key, and the same file with an undeclared key outside the
prefix exits non-zero.

## T5 — Carry the Section Through a Regeneration

**Covers** EXP-17, EXP-18.

`seid config generate` reads the existing `sei.toml` and re-emits its experimental
section. Nothing else can derive one, because the legacy configuration files carry no
experimental key.

Ordered after T4 rather than T2 only because it needs a key to carry. It is the task
with a silent failure, so it does not slip.

**Done when** a regeneration over a file holding an experimental value writes that
value again, and removing the carry-forward makes the test fail with the value gone.

## T6 — Record Stability on a Section

**Covers** EXP-11.

One accessor on `Section`, so the migration chain can tell a committed section from an
experimental one without matching on the key's name.

**Done when** every section registered through the section verbs reports committed,
every group registered through the experimental verb reports experimental, and a test
measures both sets against the registered set rather than against a written list.

## Not Tasks Yet

**EXP-10, EXP-12, EXP-13.** The migration chain's half of the contract, and the rules
a graduation follows. Nothing can exercise them until the chain exists. T6 is what
leaves the chain something to read.

## Order

T1 and T2 are one change. T3 follows them. T4 and T5 each stand alone and can go in
either order, except that T5 needs a key to carry, so it runs after T2 at the earliest.
T6 is independent of all of them.
