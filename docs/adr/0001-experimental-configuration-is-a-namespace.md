# 1. Experimental Configuration Is a Namespace, Not a Marking

Date: 2026-09-03

## Status

Accepted

## Context

A binary needs somewhere to offer a setting it will not stand behind. Every declared
key today is a commitment: `schema_version` versions the declared space, and changing
a key in it is a migration the binary performs. Work at the bleeding edge cannot pay
that per knob, so a knob either goes unwritten or the commitment is made by accident.

Nodes a controller manages are the ones that need it. They take their configuration
from the declaration, so a knob the declaration does not carry cannot be handed to
them at all.

Two shapes were considered.

A **per-key marking** keeps one namespace and records instability in the registry. A
package registers `evm.new_knob` and flags it experimental.

A **separate namespace** puts the setting at `experimental.<owner>.<tag>`, registered
through a verb of its own.

## Decision

Experimental configuration is a separate namespace under a reserved `experimental`
prefix, registered through one SDK verb. Keys in it resolve and reach their readers
like any other key, and they sit outside the schema contract: a key MAY be renamed,
removed, or change meaning between releases, with no migration and no deprecation.

The registry owns the prefix and nests under it on the caller's behalf.

## Consequences

A marking is invisible where it matters. A reader asks for a key by name, and an
operator writes that name into a file. Neither sees a flag held in the registry, so
somebody writes `evm.new_knob`, finds it gone a release later, and nothing in the file
they were looking at said it could be. A prefix is visible in the file itself.

The registry refuses a section name carrying a dot, because a dotted name declares
keys inside another section's subtree, where two sections' defaults land in one map
and whichever renders last silently wins. A package therefore cannot register
`experimental.evm` through the section verbs. The dedicated verb is what makes nesting
safe, because the registry controls the prefix and holds one owner per name.

Two namespaces mean two rules for the migration chain rather than one, and the chain
has to tell them apart. It reads a stability recorded on each section rather than
matching on a key's name, so a renamed prefix does not silently move a key into the
contract.

The declared key space grows by keys that are not commitments. Anything counting
declared keys as the contract now overstates it, and the count of committed keys is
the one that means what it used to.

A key under the prefix that no binary declares is a note rather than a refusal,
because a controller writing a file for a newer binary and rolling it to nodes that
have not upgraded is the ordinary case. The cost is that a misspelled experimental key
is reported and not refused.
