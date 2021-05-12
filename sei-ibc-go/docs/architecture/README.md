# Architecture Decision Records (ADR)

This is a location to record all high-level architecture decisions in the ibc-go project.

You can read more about the ADR concept in this [blog post](https://product.reverb.com/documenting-architecture-decisions-the-reverb-way-a3563bb24bd0#.78xhdix6t).

An ADR should provide:

- Context on the relevant goals and the current state
- Proposed changes to achieve the goals
- Summary of pros and cons
- References
- Changelog

Note the distinction between an ADR and a spec. The ADR provides the context, intuition, reasoning, and
justification for a change in architecture, or for the architecture of something
new. The spec is much more compressed and streamlined summary of everything as
it is or should be.

If recorded decisions turned out to be lacking, convene a discussion, record the new decisions here, and then modify the code to match.

Note the context/background should be written in the present tense.

To suggest an ADR, please make use of the [ADR template](./adr-template.md) provided.

## Table of Contents

| ADR \# | Description | Status |
| ------ | ----------- | ------ |
| [001](./adr-001-coin-source-tracing.md) | ICS-20 coin denomination format | Accepted, Implemented |
| [015](./adr-015-ibc-packet-receiver.md) | IBC Packet Routing | Accepted |
| [025](./adr-025-ibc-passive-channels.md) | IBC passive channels | Deprecated |
| [026](./adr-026-ibc-client-recovery-mechanisms.md) | IBC client recovery mechansisms | Accepted |
| [027](./adr-027-ibc-wasm.md) | Wasm based light clients | Accepted |


