# Experimental Configuration Plan

## Minimum Viable Version

The smallest thing that delivers the point of the namespace is a package registering
a knob, a `sei.toml` writing it, a node running it, and an older binary meeting it
without complaint. That is EXP-1 through EXP-9, EXP-14 through EXP-18.

Two requirements are deliberately outside it. EXP-10 and EXP-11 are the migration
chain's half of the contract, and EXP-12 and EXP-13 are rules a graduation follows.
Neither can be exercised before a chain exists, so the minimum version states the
stability on each section and stops there. That much is what the chain will read.

## Interfaces

The SDK verb, beside the four registration verbs the registry already offers:

```go
// RegisterExperimental records a group of settings this binary offers without
// committing to them.
//
// owner is one segment and names the group, so its keys are experimental.<owner>.<tag>
// where each tag comes from the prototype. One package per owner.
func RegisterExperimental(owner string, prototype any, defaults func(Mode) any)
```

The stability a section carries, read by the migration chain rather than matched on a
name:

```go
// Stability says whether a section's keys are inside the schema contract.
type Stability int

const (
	// Committed keys are versioned by schema_version, and changing one is a migration.
	Committed Stability = iota
	// Experimental keys sit outside that contract and may change between releases.
	Experimental
)

// Stability reports whether this section's keys are inside the schema contract.
func (s Section) Stability() Stability
```

What the resolution reports about a key nothing declares, split so a caller can tell
the two apart:

```go
type Resolved struct {
	// ...
	// UnknownInFile are keys the file carried that no section declares, sorted.
	UnknownInFile []string
	// UnknownExperimental are keys the file carried under the experimental prefix that
	// no section declares, sorted.
	//
	// Separate from UnknownInFile because the two are different situations. A key
	// outside the prefix that nothing declares is a mistake. A key under it is
	// routinely a file written for a newer binary, which is the case a controller
	// rolling a change forward produces on every node it has not reached yet.
	UnknownExperimental []string
}
```

No new verb for reading. An experimental key reaches its reader through the delivery
its group declares, so a reader calls what it already calls.

## Sequence

**1. The namespace and the verb.** EXP-1 to EXP-6. The registry reserves the prefix,
refuses a section claiming it, refuses a duplicate owner, and derives keys from the
prototype. Nothing consumes it yet, so this lands with the registry's own tests.

**2. Resolution and delivery.** EXP-7 to EXP-9. A registered group resolves like a
section and reaches its reader the same way. The separateness in EXP-9 is a property
to measure rather than build: the prefix cannot collide with a committed key because
EXP-1 reserves it.

**3. The unknown key.** EXP-14 to EXP-16. `Resolved` splits the two kinds of unknown
key, the boot reports the experimental ones, and the check reports without failing.
This is the requirement that lets a controller write ahead of a rollout.

**4. Carrying it forward.** EXP-17 and EXP-18. `seid config generate` reads the
existing `sei.toml` and re-emits its experimental section.

**5. Stability on a section.** EXP-11. One accessor, so the chain has something to
read when it arrives.

Steps 1 and 2 are one slice. Step 3 stands alone. Step 4 stands alone and is the one
with a data-loss failure, so it does not wait.

## Risks

**Regeneration drops the value.** `seid config generate` derives its file from
`app.toml` and `config.toml`, and neither carries an experimental key. A controller
regenerating on every start therefore discards every experimental value unless step 4
lands with the namespace. The failure is silent, and the node reverts to the
registered default rather than refusing.

**Registration is available before a migration exists.** A package can register a
group, the key can be written into files on real nodes, and no chain exists yet to
graduate it. That is acceptable while the namespace promises nothing, and it is the
reason EXP-13 is stated now rather than when the chain is built.

**One owner per package is a convention the compiler cannot hold.** EXP-3 refuses a
duplicate owner at registration, which is a defect the registry reports rather than a
build failure, because registration happens during package initialisation.

## Verification

Each requirement is covered by a test naming its ID. Three are worth naming here
because they are the ones a passing suite could otherwise fake.

**EXP-14 and EXP-15** need a `sei.toml` carrying a key under the prefix that no
registered group declares, driven through a real boot and a real check. Measured by
the check's exit status, not by its text.

**EXP-17** needs a file with an experimental value, a regeneration over it, and the
value still there. The mutation is removing the carry-forward and confirming the value
is gone.

**EXP-9** needs a committed key and an experimental key whose tags read alike, with
the committed one measured after the experimental one is written.

## Out of Scope

The migration chain. Graduation tooling. Any change to what a committed key resolves
to.
