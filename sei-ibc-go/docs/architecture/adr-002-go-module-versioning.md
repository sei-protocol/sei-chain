# ADR 002: Go module versioning

## Changelog
* 05/01/2022: initial draft

## Status

Accepted

## Context

The IBC module was originally developed in the Cosmos SDK and released during with the Stargate release series (v0.42).
It was subsequently migrated to its own repository, ibc-go.
The first official release on ibc-go was v1.0.0. 
v1.0.0 was decided to be used instead of v0.1.0 primarily for the following reasons:
- Maintaining compatibility with the IBC specification v1 requires stronger support/guarantees.
- Using the major, minor, and patch numbers allows for easier communication of what breaking changes are included in a release.
- The IBC module is being used by numerous high value projects which require stability.

### Problems

#### Go module version must be incremented

When a Go module is released under v1.0.0, all following releases must follow Go semantic versioning.
Thus when the go API is broken, the Go module major version **must** be incremented. 
For example, changing the go package version from `v2` to `v3` bumps the import from `github.com/cosmos/ibc-go/v2` to `github.com/cosmos/ibc-go/v3`.

If the Go module version is not incremented then attempting to go get a module @v3.0.0 without the suffix results in:
`invalid version: module contains a go.mod file, so major version must be compatible: should be v0 or v1, not v3`

Version validation was added in Go 1.13. This means is that in order to release a v3.0.0 git tag without a /v3 suffix on the module definition, the tag must explicitly **not** contain a go.mod file.
Not including a go.mod in our release is not a viable option.

#### Attempting to import multiple go module versions for ibc-go

Attempting to import two versions of ibc-go, such as `github.com/cosmos/ibc-go/v2` and `github.com/cosmos/ibc-go/v3`, will result in multiple issues. 

The Cosmos SDK does global registration of error and governance proposal types. 
The errors and proposals used in ibc-go would need to now register their naming based on the go module version.

The more concerning problem is that protobuf definitions will also reach a namespace collision.
ibc-go and the Cosmos SDK in general rely heavily on using extended functions for go structs generated from protobuf definitions.
This requires the go structs to be defined in the same package as the extended functions. 
Thus, bumping the import versioning causes the protobuf definitions to be generated in two places (in v2 and v3). 
When registering these types at compile time, the go compiler will panic.
The generated types need to be registered against the proto codec, but there exist two definitions for the same name.

The protobuf conflict policy can be overriden via the environment variable `GOLANG_PROTOBUF_REGISTRATION_CONFLICT`, but it is possible this could lead to various runtime errors or unexpected behaviour (see [here](https://github.com/protocolbuffers/protobuf-go/blob/master/reflect/protoregistry/registry.go#L46)).
More information [here](https://developers.google.com/protocol-buffers/docs/reference/go/faq#namespace-conflict) on namespace conflicts for protobuf versioning.

### Potential solutions

#### Changing the protobuf definition version

The protobuf definitions all have a type URL containing the protobuf version for this type. 
Changing the protobuf version would solve the namespace collision which arise from importing multiple versions of ibc-go, but it leads to new issues. 

In the Cosmos SDK, `Any`s are unpacked and decoded using the type URL.
Changing the type URL thus is creating a distinctly different type. 
The same registration on the proto codec cannot be used to unpack the new type.
For example:

All Cosmos SDK messages are packed into `Any`s. If we incremented the protobuf version for our IBC messages, clients which submitted the v1 of our Cosmos SDK messages would now be rejected since the old type is not registered on the codec.
The clients must know to submit the v2 of these messages. This pushes the burden of versioning onto relayers and wallets.

A more serious problem is that the `ClientState` and `ConsensusState` are packed as `Any`s. Changing the protobuf versioning of these types would break compatibility with IBC specification v1.

#### Moving protobuf definitions to their own go module

The protobuf definitions could be moved to their own go module which uses 0.x versioning and will never go to 1.0.
This prevents the Go module version from being incremented with breaking changes.
It also requires all extended functions to live in the same Go module, disrupting the existing code structure.

The version that implements this change will still be incompatible with previous versions, but future versions could be imported together without namespace collisions.
For example, lets say this solution is implmented in v3. Then

`github.com/cosmos/ibc-go/v2` cannot be imported with any other ibc-go version

`github.com/cosmos/ibc-go/v3` cannot be imported with any previous ibc-go versions

`github.com/cosmos/ibc-go/v4` may be imported with ibc-go versions v3+

`github.com/cosmos/ibc-go/v5` may be imported with ibc-go versions v3+

## Decision

Supporting importing multiple versions of ibc-go requires a non-trivial amount of complexity.
It is unclear when a user of the ibc-go code would need multiple versions of ibc-go. 
Until there is an overwhelming reason to support importing multiple versions of ibc-go:

**Major releases cannot be imported simultaneously**.
Releases should focus on keeping backwards compatibility for go code clients, within reason. 
Old functionality should be marked as deprecated and there should exist upgrade paths between major versions. 
Deprecated functionality may be removed when no clients rely on that functionality.
How this is determined is to be decided.

**Error and proposal type registration will not be changed between go module version increments**.
This explicitly stops external clients from trying to import two major versions (potentially risking a bug due to the instability of proto name collisions override).

## Consequences

This only affects clients relying directly on the go code. 

### Positive

### Negative

Multiple ibc-go versions cannot be imported.

### Neutral

