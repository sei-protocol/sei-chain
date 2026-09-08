# Feature Specification: Experimental Configuration

**Feature Branch**: `plt-exp-config`

**Created**: 2026-09-03

**Status**: Draft

**Input**: sei.toml needs an `[experimental]` section. Its values are not a committed
part of the configuration API contract. Controller-managed nodes at the bleeding edge
need somewhere to put knobs that are not promises, and the config SDK has to let a
package register them.

## Semantic Anchors

Named once. Not restated below.

| Anchor | Governs | Does not cover |
|---|---|---|
| EARS | acceptance criteria syntax | whether each template fits the behaviour |
| RFC 2119 | normative keywords | whether the obligation is the right one |
| SemVer | what a version number promises | this schema counter, which counts migrations |
| INVEST | whether a story is a real slice | whether the slice delivers value |

## Glossary

- **Committed key**: a declared key inside the schema contract. The schema counter
  versions it. Changing it needs a migration.
- **Experimental key**: a declared key outside the schema contract, under the
  reserved prefix.
- **Owner**: the name a registering package gives its group. One segment. One package
  per name.
- **Graduation**: an experimental key becoming a committed key.
- **The reserved prefix**: `experimental`, the one top-level name a section cannot claim.

## Boundary Context

- **Sits within**: the configuration registry and the `sei.toml` reader.
- **Owns**: the reserved prefix, the SDK verb that registers into it, how those values
  resolve and reach a reader, and what a binary does with a key under the prefix that
  it does not declare.
- **Does not own**: the schema migration chain, which is decided separately. This spec
  states only that the chain leaves the prefix alone, and records the stability the
  chain reads.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Offer a Knob Without Promising It (Priority: P1)

A package has a setting it wants to expose and cannot stand behind yet. Today its only
option is the declared key space, where every key is a commitment the schema counter
versions.

**Why this priority**: without it the choice is to make a commitment by accident or to
ship no knob. Both cost more than the feature.

**Independent Test**: register a group in a test package, write one of its keys into a
`sei.toml`, start a node, and read the value back through the reader that owns it.

**Acceptance Scenarios**:

1. A package registers a group. Its keys appear in the declared set under the reserved
   prefix and the owner's name.
2. A `sei.toml` states one of those keys. The node runs the written value.
3. A `sei.toml` omits it. The node runs the registered default.

### User Story 2 - Roll a File Ahead of the Binary (Priority: P1)

A controller manages a fleet. It writes one `sei.toml` for a release that adds an
experimental knob, and rolls it to nodes that have not upgraded yet.

**Why this priority**: this is the case the namespace exists for. A file that fails on
every node the rollout has not reached is unusable for a fleet.

**Independent Test**: write a `sei.toml` naming a key under the prefix that this binary
does not declare, then start a node and run the check. The node starts. The check
passes and names the key.

**Acceptance Scenarios**:

1. A node meets a key under the prefix it does not declare. It reports the key and
   applies every other value.
2. The check meets the same file. It reports the key and exits zero.
3. A file names an undeclared key outside the prefix. The check still fails.

### User Story 3 - Keep the Value Across a Regeneration (Priority: P2)

An operator regenerates `sei.toml` on a node that holds an experimental value.

**Why this priority**: below the first two because a node has to be able to hold the
value before losing it matters. It ranks above the rest of the work because the failure
is silent: the value goes, the node reverts to a default, and nothing says so.

**Independent Test**: place a `sei.toml` with an experimental value, regenerate over
it, and read the section back.

**Acceptance Scenarios**:

1. A regeneration over a file holding experimental values writes those values again.
2. A regeneration over a file holding none writes no experimental section.

### Edge Cases

- Two packages claim one owner name. The registry reports a defect and declares
  neither group.
- A section registration claims the reserved prefix as its name.
- An owner name carries a dot, which would place its keys inside another group.
- A committed key and an experimental key carry matching tags.
- A `sei.toml` states a value under the prefix whose shape the reader cannot use.

## Requirements *(mandatory)*

### Requirement 1: The Reserved Prefix and Its Registration

**Objective:** As a package author, I want one verb that registers a group of settings
this binary offers without committing to them, so that a new knob does not join the
schema contract.

**Traces to:** User Story 1

#### Acceptance Criteria

1. **EXP-1**: THE registry SHALL reserve `experimental` as a top-level prefix.
2. **EXP-2**: WHEN a section registration claims the reserved prefix as its name, THE
   registry SHALL report a defect and SHALL derive no key from it.
3. **EXP-3**: THE registry SHALL offer one verb that registers an experimental group,
   taking an owner, a prototype whose tags name the keys, and a function answering the
   group's default for a kind of node.
4. **EXP-4**: WHEN a package registers an experimental group, THE registry SHALL derive
   its keys from the prototype's tags and SHALL name each one
   `experimental.<owner>.<tag>`.
5. **EXP-5**: IF two registrations claim one owner, THEN THE registry SHALL report a
   defect and SHALL declare neither group.
6. **EXP-6**: IF an owner name carries a dot, THEN THE registry SHALL report a defect.
7. **EXP-7**: THE registry SHALL make that verb the only way to declare a key under the
   reserved prefix.

### Requirement 2: Resolution and Delivery

**Objective:** As a node, I want an experimental value to reach its reader the way every
other value does, so that a package reading one calls what it already calls.

**Traces to:** User Story 1

#### Acceptance Criteria

1. **EXP-8**: THE resolution SHALL answer every registered experimental key.
2. **EXP-9**: WHERE `sei.toml` states an experimental key, THE resolution SHALL answer
   with the written value.
3. **EXP-10**: WHERE `sei.toml` omits a registered experimental key, THE resolution
   SHALL answer with the registered default for the node's kind.
4. **EXP-11**: THE delivery SHALL hand an experimental value to its reader by the same
   route its group declares.
5. **EXP-12**: THE resolution SHALL keep the two namespaces separate. A value under the
   reserved prefix SHALL NOT change what a committed key answers.

### Requirement 3: Meeting an Unknown Experimental Key

**Objective:** As a fleet operator, I want one file to work on nodes at two releases, so
that a rollout does not have to reach every node before the file is usable.

**Traces to:** User Story 2

#### Acceptance Criteria

1. **EXP-13**: IF `sei.toml` states a key under the reserved prefix that this binary
   does not declare, THEN THE boot SHALL report that key and SHALL apply every other
   resolved value.
2. **EXP-14**: IF `sei.toml` states a key under the reserved prefix that this binary
   does not declare, THEN THE check command SHALL report that key and SHALL exit zero.
3. **EXP-15**: IF `sei.toml` states a key outside the reserved prefix that this binary
   does not declare, THEN THE check command SHALL exit non-zero.
4. **EXP-16**: THE resolution SHALL report the two kinds of undeclared key separately,
   so a caller tells a file written for a newer binary from a mistake.

### Requirement 4: Carrying the Section Through a Regeneration

**Objective:** As an operator, I want a regeneration to keep the experimental values my
file holds, so that writing the file again does not revert a setting I chose.

**Traces to:** User Story 3

#### Acceptance Criteria

1. **EXP-17**: WHERE an existing `sei.toml` states experimental keys, THE generate
   command SHALL write those keys into the file it produces.
2. **EXP-18**: THE generate command SHALL NOT write an experimental key the existing
   file does not state. The legacy configuration files carry no experimental key, so
   nothing else derives one.

### Requirement 5: The Schema Contract Boundary

**Objective:** As a migration author, I want to tell a committed section from an
experimental one, so that a rename never moves a key into the contract by accident.

**Traces to:** User Story 1

#### Acceptance Criteria

1. **EXP-19**: THE registry SHALL record, for each section, whether its keys sit inside
   the schema contract.
2. **EXP-20**: THE registry SHALL report that stability without a caller matching on a
   key's name.
3. **EXP-21**: THE schema counter SHALL NOT govern an experimental key.
4. **EXP-22**: A migration SHALL NOT read, write, rename, or remove a key under the
   reserved prefix.
5. **EXP-23**: WHERE a key graduates, THE change SHALL be a migration that renames it
   into a committed section and raises the schema counter.
6. **EXP-24**: A migration SHALL NOT move a committed key under the reserved prefix.

### Key Entities

- **Experimental group**: one package's set of experimental keys, named by its owner.
  Holds a prototype and a default per kind of node, as a section does.
- **Stability**: whether a section's keys sit inside the schema contract. Two values.

## Success Criteria *(mandatory)*

- **SC-001**: A package registers a group and a node runs a written value from it,
  measured through a real boot rather than through the registry alone.
- **SC-002**: A `sei.toml` naming an undeclared key under the reserved prefix starts a
  node and passes the check, while the same file with an undeclared key outside the
  prefix fails the check.
- **SC-003**: A regeneration over a file holding experimental values produces a file
  holding the same values. Removing the carry-forward makes that measurement fail.
- **SC-004**: Every section reports its stability, and the committed set and the
  experimental set together account for every registered section.
- **SC-005**: Every criterion above is named by a test. A criterion no test names is
  not done.

## Assumptions

Nodes use this namespace rarely. The volume is a handful of keys, not a second key
space of comparable size.

An experimental group answers per kind of node, for consistency with a section. A group
whose value does not vary answers the same for each kind.

The reader that owns an experimental key holds its own fallback, as readers of committed
keys do. This spec does not change how a reader treats a value it cannot use.

## Out of scope

The schema migration chain. EXP-21 through EXP-24 state the boundary the chain honours,
and EXP-19 and EXP-20 give it something to read. Nothing here builds it.

Graduation tooling. A graduation is a migration, written when a key graduates.

Any change to what a committed key resolves to.

Making the namespace safe to misspell. A key under the prefix that no binary declares
reaches nothing, and EXP-13 and EXP-14 require a report rather than a refusal.
