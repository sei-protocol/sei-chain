# Feature Specification: Node Configuration

**Feature Branch**: `plt-node-config-spec`

**Created**: 2026-09-18

**Status**: Draft

**Input**: The canonical specification for how a Sei blockchain client resolves, delivers, reports
and edits its own configuration. seid's implementation is called ConfigManager v2. This document
governs any conforming client, so it states observable behaviour and names no library.

## Semantic Anchors

Named once. Not restated below.

| Anchor | Governs | Does not cover |
|---|---|---|
| EARS | acceptance criteria syntax | whether each template fits the behaviour |
| RFC 2119 | normative keywords | whether the obligation is the right one |
| TOML v1.0.0 | the file's surface syntax | which of its shapes this file carries |
| SemVer | what a release number promises | the schema counter, which counts migrations |
| Diátaxis | this document is reference | whether an operator can follow it as a guide |

## Glossary

- **Client**: a program that runs a Sei node and reads its own configuration.
- **Node kind**: the role a node runs as. A client declares the kinds it knows.
- **Declared key**: a setting the client states it answers for, by a dotted name.
- **Declaration**: the whole set of declared keys, with a default for each node kind.
- **The configuration file**: the one file an operator writes. `sei.toml` today.
- **Schema counter**: the number in that file that counts migrations.
- **Source**: a place a value can come from. The declaration, the file, the environment, the
  command line.
- **Reader**: the code inside the client that consumes one setting.
- **Lookup delivery**: a reader asking a shared source for a key by name.
- **Decode delivery**: a reader taking its values from a document decoded into a structure.
- **Legacy file**: a configuration file an earlier model wrote. `app.toml` and `config.toml`.
- **Operator**: the person or system that writes the configuration file.

## Boundary Context

- **Sits within**: a client's startup, and the commands an operator runs against a node's
  configuration.
- **Owns**: which keys a client answers, which sources answer them and in what order, how a value
  reaches its reader, what a client reports, which shapes the file carries, and the configuration command
  space.
- **Does not own**: what any individual setting means, or which value is correct for it. The
  consensus, storage and networking behaviour those settings control. The experimental namespace,
  which `specs/experimental-config` specifies. The migration chain.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - One File States What a Node Runs (Priority: P1)

An operator wants to know what a node runs. Today they read two generated files. Those files state more than two hundred settings between
them. A value a client wrote and a value the operator chose look alike, and a key absent from both
still has a value somewhere.

**Why this priority**: every other story rests on it. A report, a check and a writer all describe
one model, and without the model they describe nothing.

**Independent Test**: write one file stating three settings, start a node, and read back every
declared key. The three hold what the file said. Every other key holds the client's declared
default for that node's kind.

**Acceptance Scenarios**:

1. The file states a declared key. The node runs the written value.
2. The file omits a declared key. The node runs the declared default for its kind.
3. The file states nothing but the two describing keys. Every declared key runs its default.
4. Two node kinds read one file. Each runs its own kind's defaults for the keys the file omits.

### User Story 2 - A Mistake Costs a Report, Not an Outage (Priority: P1)

An operator mistypes one value. The node restarts.

**Why this priority**: a configuration path that can refuse a start turns a typo into an outage
across a fleet, and a fleet rolls configuration forward to every node before anybody reads a
report.

**Independent Test**: write a value whose type its reader cannot take, start the node, and confirm
the node runs. The report names the key. Every other value applies.

**Acceptance Scenarios**:

1. One value is unusable. The node starts, that key reads as it did before, and the report names it.
2. One section holds an unusable value and another holds a good one. The good one applies.
3. The file is absent, unreadable, or names a kind the client does not know. The node starts.
4. The report names no configuration value, in any of these cases.

### User Story 3 - Answer Before a Restart (Priority: P1)

An operator has edited a file and has not restarted. They want to know what the node will do.

**Why this priority**: a start cannot refuse a file, so every failure at start is a report on a node
that has already restarted. The same questions have exact answers beforehand.

**Independent Test**: run the check command against a file holding one unusable value. It names the
value and exits non-zero. Against a file the client can use in full it exits zero.

**Acceptance Scenarios**:

1. The file holds a value a delivery would refuse. The command reports it and exits non-zero.
2. The file names a key the client does not declare. The command reports it.
3. The file names one kind of node and the node runs as another. The command reports the
   disagreement.
4. The client would not read the file at all. The command says so rather than reporting a pass.

### User Story 4 - Provision a Node That Tracks Its Client (Priority: P2)

An operator, or a system managing a fleet, creates a node and wants it to hold its kind's defaults
and nothing else, so that upgrading the client moves those defaults with it.

**Why this priority**: it is the case a managed fleet needs, and it is the smallest file the model
can produce. Below the first three because a node has to resolve a file before writing one is
useful.

**Independent Test**: run the generate command for a node kind, with no source named. The file it
writes states the two describing keys and nothing else. A node started from it runs that kind's
declared defaults.

**Acceptance Scenarios**:

1. The command writes a file stating only the describing keys.
2. A node started from that file runs the declared default for every declared key.
3. The command does not replace a file that is already there.

### User Story 5 - Move an Existing Node Onto the Model (Priority: P2)

A node has legacy files somebody tuned. Adopting the model means every declared key the new file
omits moves to a declared default, and there are more than two hundred of them.

**Why this priority**: it is the transition, not the destination. It ranks here because the
transition is what every existing node needs, and it becomes unnecessary once no node holds legacy
files.

**Independent Test**: run the generate command against a node's legacy files, asking explicitly for
them. Start the node from the file it writes. Every declared key holds what it held before.

**Acceptance Scenarios**:

1. The operator asks for the legacy files explicitly. The command reads them.
2. The operator does not ask. The command does not read them.
3. The file states every key whose value differs from the declared default, and no others.
4. A node started from that file runs what it ran before, for every declared key.

### Edge Cases

- A key and a section share one name, so no file can hold both.
- Two declared keys collapse onto one environment variable spelling.
- The environment supplies a key nesting under a declared key, and the environment is the only
  source that can.
- A node's kind changes while its file still names the old one.
- The client is rolled back to a release whose schema counter is lower than the file's.
- An operator writes a value for a key the client declares and no reader consumes.
- The command namespace already holds a command that configures the client's own tooling rather
  than the node.

## Requirements *(mandatory)*

### Requirement 1: The Configuration Model

**Objective:** As an operator, I want one file and one client to determine every setting, so that I
can state what a node runs without reading anything else.

**Traces to:** User Story 1

#### Acceptance Criteria

1. **CFG-1**: THE client SHALL declare a set of keys it answers for, and SHALL answer every one of
   them from its resolution.
2. **CFG-2**: THE client SHALL declare, for each declared key, a default that MAY differ by node
   kind.
3. **CFG-3**: THE client SHALL hold its defaults in the client. It SHALL NOT write a default into
   the configuration file.
4. **CFG-4**: WHERE the configuration file states a declared key, THE client SHALL answer with the
   written value.
5. **CFG-5**: WHERE the configuration file omits a declared key, THE client SHALL answer with the
   declared default for the node's kind.
6. **CFG-6**: THE client SHALL treat a written value as a commitment. It SHALL NOT rewrite or
   remove one.
7. **CFG-7**: THE client SHALL decide its own declared set. A report one client makes about a file
   SHALL NOT be read as describing what another client does with it.
8. **CFG-8**: IF a node kind is not one the client declares, THEN THE client SHALL refuse to answer
   for it rather than choose a kind.
9. **CFG-9**: THE client SHALL reserve one namespace for settings outside this contract, specified
   in `specs/experimental-config`.

### Requirement 2: The Configuration File

**Objective:** As an operator, I want to hand-edit the file and keep my own comments, so that the
reason for a setting survives the next edit.

**Traces to:** User Story 1

#### Acceptance Criteria

1. **CFG-10**: THE file SHALL carry a schema counter and a node kind, and THE client SHALL read
   both before it reads any value from the file.
2. **CFG-11**: THE schema counter SHALL count migrations, one for each migration. It SHALL NOT be a
   release number.
3. **CFG-12**: IF the schema counter is absent, is not a whole number, is below the first schema, or
   is ahead of the one the client understands, THEN THE client SHALL refuse the file.
4. **CFG-13**: IF the node kind is absent, is not text, or is empty, THEN THE client SHALL refuse
   the file.
5. **CFG-14**: THE client SHALL NOT report either describing key as a configuration value.
6. **CFG-15**: WHEN the client writes one key, THE client SHALL leave every other line of the file
   as it was, including a comment an operator wrote.
7. **CFG-16**: WHEN the client removes one key, THE client SHALL remove that key's own comment with
   it.
8. **CFG-17**: THE client SHALL write the file in full or not at all. A failed write SHALL leave no
   partial file and no temporary file.
9. **CFG-18**: THE client SHALL write a value back as the type it read.
10. **CFG-19**: THE client SHALL refuse a file shape it cannot write back unchanged. An infinity, a
    value that is not a number, a date, a time, an inline table, a dotted key, and an array of
    tables are such shapes.
11. **CFG-20**: Every segment of a key SHALL be lower case, and SHALL carry only letters, digits,
    underscores and hyphens.
12. **CFG-21**: THE client SHALL put a question about a shape to the same decoder a start uses,
    rather than to a list it keeps of its own.

### Requirement 3: Resolution

**Objective:** As an operator, I want one stated order of precedence, so that I can predict which
source answers a key.

**Traces to:** User Story 1

#### Acceptance Criteria

1. **CFG-22**: THE client SHALL resolve one value for each declared key from these sources, each
   outranking the one before it: its declared defaults, the configuration file, the environment,
   the command line.
2. **CFG-23**: THE client SHALL state that order in one place.
3. **CFG-24**: THE client SHALL report which declared keys a source other than its defaults
   supplied.
4. **CFG-25**: THE client SHALL report which keys a source carried that the client does not
   declare.
5. **CFG-26**: THE client SHALL answer for every declared key, or SHALL report what it could not
   answer for. It SHALL NOT hand a caller a resolution with a key missing.
6. **CFG-27**: THE client SHALL derive a key's environment spelling from the key itself.
7. **CFG-28**: IF a channel cannot carry a key's value, THEN THE client SHALL report that key as
   ignored and SHALL answer from the next source down.
8. **CFG-29**: IF two declared keys derive one environment spelling, THEN THE client SHALL refuse
   the declaration and SHALL name both keys.

### Requirement 4: Delivery

**Objective:** As a reader inside the client, I want the resolved value to arrive where I already
look for it, so that no reader changes to adopt the model.

**Traces to:** User Story 1

#### Acceptance Criteria

1. **CFG-30**: THE client SHALL deliver every resolved value to the reader that owns its key.
2. **CFG-31**: WHERE a reader asks a shared source for a key by name, THE client SHALL place the
   value in that source.
3. **CFG-32**: WHERE a reader takes its values from a document decoded before any lookup, THE
   client SHALL deliver by decoding into that reader's own structure.
4. **CFG-33**: A delivery SHALL NOT change which value any other declared key answers.
5. **CFG-34**: IF two keys occupy one path, so that delivering one makes the other unreadable, THEN
   THE client SHALL refuse that delivery and SHALL name both keys.

### Requirement 5: Reporting and Refusal

**Objective:** As a fleet operator, I want a configuration mistake to cost a report, so that rolling
a change forward cannot take the fleet down.

**Traces to:** User Story 2

#### Acceptance Criteria

1. **CFG-35**: Nothing in the configuration path SHALL stop a node starting. Every failure in it
   SHALL leave each key answering as it did before.
2. **CFG-36**: WHERE the client refuses a value, the refusal SHALL cover one section and SHALL NOT
   cover the whole file.
3. **CFG-37**: THE client SHALL report every value it could not use, and each report SHALL name the
   key.
4. **CFG-38**: WHERE the client refuses a value, the report SHALL state what the node runs instead.
5. **CFG-39**: THE client SHALL NOT write a configuration value into a log line.
6. **CFG-40**: THE client SHALL report a defect in its own declaration separately from a mistake in
   an operator's file.

### Requirement 6: The Configuration Command Space

**Objective:** As an operator, I want to ask what a file reaches and to write one, so that I do not
learn the answer from a restarted node.

**Traces to:** User Story 3, User Story 4, User Story 5

#### Acceptance Criteria

1. **CFG-41**: THE client SHALL offer a command that reports what a node's configuration file
   reaches, without starting the node.
2. **CFG-42**: That command SHALL resolve the file the way a start resolves it, and SHALL write
   nothing.
3. **CFG-43**: That command's exit status SHALL be its answer. Anything that would stop the node
   SHALL exit non-zero.
4. **CFG-44**: IF the client would not read the file at all, THEN that command SHALL report that
   rather than report a pass.
5. **CFG-45**: THE client SHALL offer a command that writes a configuration file for a node kind.
6. **CFG-46**: WHERE no source is named, that command SHALL write a file stating only the
   describing keys, so that every declared key answers from the client's own default.
7. **CFG-47**: That command SHALL NOT replace a configuration file that is already there.
8. **CFG-48**: IF the node kind asked for disagrees with the kind the node runs as, THEN that
   command SHALL refuse rather than write a file a start would ignore.
9. **CFG-49**: THE client SHALL offer a command that reports the value a node answers for one
   declared key, and which source answered it.

### Requirement 7: Transitional Requirements

**Objective:** As a maintainer, I want every requirement that exists only for the transition named
with the condition that removes it, so that none of them outlives its reason.

**Traces to:** User Story 5

#### Acceptance Criteria

1. **CFG-50**: WHILE any client writes legacy configuration files, THE client SHALL read them, and
   SHALL let them refuse a start on their own rules. *Sunset: no client writes them.*
2. **CFG-51**: WHILE legacy files exist, THE client SHALL NOT take a declared key's value from one.
   *Sunset: with CFG-50.*
3. **CFG-52**: WHILE this model is not the only one a client offers, THE client SHALL select it by
   an explicit signal, and SHALL use the earlier model when the signal is absent. *Sunset: the model
   is the only one.*
4. **CFG-53**: WHERE an operator asks the writing command to derive a file from the legacy files,
   THE client SHALL take that instruction explicitly. It SHALL NOT read them unless asked. *Sunset:
   no node holds legacy files.*
5. **CFG-54**: WHILE the client's own writers produce a key the client does not declare, THE client
   SHALL record each such key with the reason it is not declared. *Sunset: the declared set covers
   every key those writers produce.*
6. **CFG-55**: THE client SHALL name each transitional behaviour in its own documentation as
   transitional. *Sunset: none. This one outlives the others.*

### Key Entities

- **Declared key**: a dotted name the client answers for. Carries one default per node kind.
- **Declaration**: every declared key the client holds. A property of the built client.
- **Node kind**: the role a node runs as. Selects which default a declared key answers with.
- **Configuration file**: the operator's file. Holds the two describing keys and the settings they
  decided.
- **Schema counter**: the migration count the file has reached.
- **Section**: a group of declared keys with one reader and one delivery. The unit a refusal covers.
- **Report**: what the client tells an operator about a resolution. The operator's only signal.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator reads the configuration file and the client's declared defaults, and can
  state what the node runs for every declared key, with no other file consulted.
- **SC-002**: No configuration mistake stops a node starting. Measured by driving each failure class
  in User Story 2 through a real start.
- **SC-003**: The check command's exit status matches whether a start would be stopped, for every
  failure class, measured by status and not by text.
- **SC-004**: A node started from a file the writing command derived from its legacy files answers
  every declared key as it did before.
- **SC-005**: No log line the client writes carries a configuration value.
- **SC-006**: Two clients that resolve one file, one environment and one command line answer the
  same value for every key both declare.
- **SC-007**: Every transitional requirement names the condition that removes it, and no
  transitional requirement remains after its condition holds.
- **SC-008**: Every criterion above is named by a test. A criterion no test names is not done.

## Assumptions

The file format is TOML today. Multi-format support is a long-term goal, so the requirements here
describe the model rather than the syntax, and only Requirement 2 depends on the format.

An operator hand-edits the configuration file, and the comments they write in it are how they
explain a choice to whoever reads it next. Requirement 2 rests on this.

A node kind comes from a fixed set the client declares. A client that adds a kind declares a default
for it.

The command namespace a client uses for these commands can already hold a command that configures
the client's own tooling rather than the node. CFG-49 has to live beside it without changing it.

An experimental namespace exists for settings outside this contract. `specs/experimental-config`
specifies it, and CFG-9 is the only statement about it here.

## Out of scope

What any individual setting means, and which value is correct for it. This document says how a value
arrives, never which value to choose.

The schema migration chain. The counter and its refusals are specified here. What acts on the
counter is specified with the migrations.

The experimental namespace's own contract.

The client's own tooling configuration, which is a different file and a different audience.

Removing the legacy files. Requirement 7 states what a client does while they exist, and the
condition that ends each obligation.
