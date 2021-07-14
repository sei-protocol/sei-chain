<!--
order: 3
-->

# Genesis Restart Upgrades

Learn how to upgrade your chain and counterparty clients using genesis restarts. {synopsis}

**NOTE**: Regular genesis restarts are currently unsupported by relayers!

### IBC Client Breaking Upgrades

IBC client breaking upgrades are possible using genesis restarts. 
It is highly recommended to use the in-place migrations instead of a genesis restart.
Genesis restarts should be used sparingly and as backup plans. 

Genesis restarts still require the usage of an IBC upgrade proposal in order to correctly upgrade counterparty clients.

#### Step-by-Step Upgrade Process for SDK Chains

If the IBC-connected chain is conducting an upgrade that will break counterparty clients, it must ensure that the upgrade is first supported by IBC using the [IBC Client Breaking Upgrade List](https://github.com/cosmos/ibc-go/blob/main/docs/ibc/upgrades/quick-guide.md#ibc-client-breaking-upgrades) and then execute the upgrade process described below in order to prevent counterparty clients from breaking.

1. Create a 02-client [`UpgradeProposal`](https://github.com/cosmos/ibc-go/blob/main/docs/ibc/proto-docs.md#upgradeproposal) with an `UpgradePlan` and a new IBC ClientState in the `UpgradedClientState` field. Note that the `UpgradePlan` must specify an upgrade height **only** (no upgrade time), and the `ClientState` should only include the fields common to all valid clients and zero out any client-customizable fields (such as TrustingPeriod).
2. Vote on and pass the `UpgradeProposal`
3. Halt the node after successful upgrade. 
4. Export the genesis file.
5. Swap to the new binary.
6. Run migrations on the genesis file.
7. Remove the `UpgradeProposal` plan from the genesis file. This may be done by migrations.
8. Change desired chain-specific fields (chain id, unbonding period, etc). This may be done by migrations.
8. Reset the node's data.
9. Start the chain.

Upon the `UpgradeProposal` passing, the upgrade module will commit the UpgradedClient under the key: `upgrade/UpgradedIBCState/{upgradeHeight}/upgradedClient`. On the block right before the upgrade height, the upgrade module will also commit an initial consensus state for the next chain under the key: `upgrade/UpgradedIBCState/{upgradeHeight}/upgradedConsState`.

Once the chain reaches the upgrade height and halts, a relayer can upgrade the counterparty clients to the last block of the old chain. They can then submit the proofs of the `UpgradedClient` and `UpgradedConsensusState` against this last block and upgrade the counterparty client.

#### Step-by-Step Upgrade Process for Relayers Upgrading Counterparty Clients

These steps are identical to the regular [IBC client breaking upgrade process](https://github.com/cosmos/ibc-go/blob/main/docs/ibc/upgrades/quick-guide.md#step-by-step-upgrade-process-for-relayers-upgrading-counterparty-clients).

### Non-IBC Client Breaking Upgrades

While ibc-go supports genesis restarts which do not break IBC clients, relayers do not support this upgrade path. 
Here is a tracking issue on [Hermes](https://github.com/informalsystems/ibc-rs/issues/1152).
Please do not attempt a regular genesis restarts unless you have a tool to update counterparty clients correctly.



