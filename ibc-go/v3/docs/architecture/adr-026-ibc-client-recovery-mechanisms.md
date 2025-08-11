# ADR 026: IBC Client Recovery Mechanisms

## Changelog

- 2020/06/23: Initial version
- 2020/08/06: Revisions per review & to reference version
- 2021/01/15: Revision to support substitute clients for unfreezing
- 2021/05/20: Revision to simplify consensus state copying, remove initial height
- 2022/04/08: Revision to deprecate AllowUpdateAfterExpiry and AllowUpdateAfterMisbehaviour
- 2022/07/15: Revision to allow updating of TrustingPeriod

## Status

*Accepted*

## Context

### Summary

At launch, IBC will be a novel protocol, without an experienced user-base. At the protocol layer, it is not possible to distinguish between client expiry or misbehaviour due to genuine faults (Byzantine behavior) and client expiry or misbehaviour due to user mistakes (failing to update a client, or accidentally double-signing). In the base IBC protocol and ICS 20 fungible token transfer implementation, if a client can no longer be updated, funds in that channel will be permanently locked and can no longer be transferred. To the degree that it is safe to do so, it would be preferable to provide users with a recovery mechanism which can be utilised in these exceptional cases.

### Exceptional cases

The state of concern is where a client associated with connection(s) and channel(s) can no longer be updated. This can happen for several reasons:

1. The chain which the client is following has halted and is no longer producing blocks/headers, so no updates can be made to the client
1. The chain which the client is following has continued to operate, but no relayer has submitted a new header within the unbonding period, and the client has expired
    1. This could be due to real misbehaviour (intentional Byzantine behaviour) or merely a mistake by validators, but the client cannot distinguish these two cases
1. The chain which the client is following has experienced a misbehaviour event, and the client has been frozen & thus can no longer be updated

### Security model

Two-thirds of the validator set (the quorum for governance, module participation) can already sign arbitrary data, so allowing governance to manually force-update a client with a new header after a delay period does not substantially alter the security model.

## Decision

We elect not to deal with chains which have actually halted, which is necessarily Byzantine behaviour and in which case token recovery is not likely possible anyways (in-flight packets cannot be timed-out, but the relative impact of that is minor).

1. Require Tendermint light clients (ICS 07) to be created with the following additional flags
    1. `allow_update_after_expiry` (boolean, default true). Note that this flag has been deprecated, it remains to signal intent but checks against this value will not be enforced.
1. Require Tendermint light clients (ICS 07) to expose the following additional internal query functions
    1. `Expired() boolean`, which returns whether or not the client has passed the trusting period since the last update (in which case no headers can be validated)
1. Require Tendermint light clients (ICS 07) & solo machine clients (ICS 06) to be created with the following additional flags
    1. `allow_update_after_misbehaviour` (boolean, default true). Note that this flag has been deprecated, it remains to signal intent but checks against this value will not be enforced.
1. Require Tendermint light clients (ICS 07) to expose the following additional state mutation functions
    1. `Unfreeze()`, which unfreezes a light client after misbehaviour and clears any frozen height previously set
1. Add a new governance proposal type, `ClientUpdateProposal`, in the `x/ibc` module
    1. Extend the base `Proposal` with two client identifiers (`string`). 
    1. The first client identifier is the proposed client to be updated. This client must be either frozen or expired.
    1. The second client is a substitute client. It carries all the state for the client which may be updated. It must have identitical client and chain parameters to the client which may be updated (except for latest height, frozen height, and chain-id). It should be continually updated during the voting period. 
    1. If this governance proposal passes, the client on trial will be updated to the latest state of the substitute.

    Previously, `AllowUpdateAfterExpiry` and `AllowUpdateAfterMisbehaviour` were used to signal the recovery options for an expired or frozen client, and governance proposals were not allowed to overwrite the client if these parameters were set to false. However, this has now been deprecated because a code migration can overwrite the client and consensus states regardless of the value of these parameters. If governance would vote to overwrite a client or consensus state, it is likely that governance would also be willing to perform a code migration to do the same.

    In addition, `TrustingPeriod` was initally not allowed to be updated by a client upgrade proposal. However, due to the number of situations experienced in production where the `TrustingPeriod` of a client should be allowed to be updated because of ie: initial misconfiguration for a canonical channel, governance should be allowed to update this client parameter. 
    
    Note that this should NOT be lightly updated, as there may be a gap in time between when misbehaviour has occured and when the evidence of misbehaviour is submitted. For example, if the `UnbondingPeriod` is 2 weeks and the `TrustingPeriod` has also been set to two weeks, a validator could wait until right before `UnbondingPeriod` finishes, submit false information, then unbond and exit without being slashed for misbehaviour. Therefore, we recommend that the trusting period for the 07-tendermint client be set to 2/3 of the `UnbondingPeriod`.

Note that clients frozen due to misbehaviour must wait for the evidence to expire to avoid becoming refrozen. 

This ADR does not address planned upgrades, which are handled separately as per the [specification](https://github.com/cosmos/ibc/tree/master/spec/client/ics-007-tendermint-client#upgrades).

## Consequences

### Positive

- Establishes a mechanism for client recovery in the case of expiry
- Establishes a mechanism for client recovery in the case of misbehaviour
- Constructing an ClientUpdate Proposal is as difficult as creating a new client

### Negative

- Additional complexity in client creation which must be understood by the user
- Coping state of the substitute adds complexity
- Governance participants must vote on a substitute client

### Neutral

No neutral consequences.

## References

- [Prior discussion](https://github.com/cosmos/ics/issues/421)
- [Epoch number discussion](https://github.com/cosmos/ics/issues/439)
- [Upgrade plan discussion](https://github.com/cosmos/ics/issues/445)
