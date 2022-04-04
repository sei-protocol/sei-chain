# Business requirements

> **TL;DR**: Rather than creating an IBC application to expose cross-chain access to every module's features, the Interchain Accounts feature would allow to leverage the capabilities of an account to access a blockchain's application-specific features.

## Problem

Without Interchain Accounts, cross-chain access to chain-specific features (such as staking, sending, voting, etc) has to be built as separate applications on top of the IBC TAO (Transport, Authentication, Ordering) layer. Creating new IBC application standards and implementations for each application-specific feature requires considerable time and resources. Interchain Accounts will allow new chain-specific features to be immediately available over IBC.

## Objectives

Provide a way to programmatically create accounts on a destination blockchain (called the host) and control them via transactions over IBC. An IBC packet will take a message from the controller blockchain to the host blockchain where it will be executed. This will allow new features on a blockchain to be immediately supported as IBC transactions, since the (destination blockchain) native messages are encapsulated in an IBC packet in an agnostic way. This will allow all of the modules on a chain to take advantage of the network effects created by the IBC ecosystem.

## Scope

| Features  | Release |
| --------- | ------- |
| Deterministically create a new interchain account over IBC on the host chain. | v1 |
| Send over IBC a packet that contains the message to be executed by the interchain account on the host. | v1 |

# User requirements

## Use cases

### Injective <> Band Chain

Currently, Injective sends an IBC transaction to Band Chain via their custom IBC oracle module, which is a data request. When this IBC packet is executed on Band Chain, validators on Band Chain fetch prices for 10 different markets. A random selection of validators will post this selection on-chain. Once a minimum quorum has been reached, an IBC packet is sent to Injective with the prices of markets. The roundtrip latency of this flow is around 30 seconds when things go well (no packet timeouts or delays in validation).

However, Injective wants to minimise as much as possible the latency between real world price updates and price updates on Injective. They can simplify this two-transaction flow to a single transaction using Interchain Accounts: Injective opens an interchain account on Band Chain, which would be able to pay for a continuous set of update transactions and maintain a standing request for the prices of marke. This would simplify the transaction flow to a single transaction, and introduce a simple flow to update the standing request if necessary.

### Umee <> Cosmos Hub

Users on the Hub would send their ATOM to Umee. In return, the user gets equivalent amount of meTokens (this token would be a form of a liquid staking token), which could then be staked on the Hub, in some other liquidity pool, etc, in addition to other business logic which Umee could perform on behalf of the users in return for the ATOM.

Umee then stakes these ATOM tokens on the Hub on behalf of Umee (ATOMs get inflation rewards, etc). Without Interchain Accounts, Umee would have to use validator controlled multisig, because for this flow Umee needs an account on the Hub which can be controlled externally in a decentralised way. With Interchain Accounts, Umee can register an interchain account on the Hub and then receive the staking rewards for the ATOM, figure out distribution back to Umee chain, and send back to the corresponding existing account on Umee.

### Hub custodial services

The problem the Cosmos ecosystem faces is fragmentation of services. When a new chain goes live, they need to talk to custodial solutions and exchanges to integrate. Many exchanges and custodial solutions don't want to integrate tens of chains unless paid in advance.

An alternative is offering the custodial service through the Hub. When a new chain goes live, the tokens of the chain are transferred through IBC to the Hub. This means that the custodial service would just have to integrate with one chain (the Hub), rather with an arbitrary number of them.

Using Interchain Accounts, a service could be built in which a user sends tokens to an interchain account address on chain `X`, which corresponds to the registered interchain account of chain `X` on the Hub. This account would handle the token transfer to the Hub and then further on to the custodial wallet.

# Functional requirements

## Assumptions

1. Interchain account packets will rarely timeout with application-set values.
2. Cosmos-SDK modules deployed on a chain are not malicious.
3. Authentication modules may implement their own permissioning scheme.

## Features

### 1 - Configuration

| ID  | Description | Verification | Status |
| --- | ----------- | ------------ | ------ |
| 1.01 | A chain shall have the ability to enable or disable Interchain Accounts controller functionality in the genesis state. | The controller parameters have a [flag](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/host/types/host.pb.go#L30) to enable/disable the controller submodule, and this flag [is stored during genesis initialization](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/controller/keeper/params.go#L24). | `Implemented` |
| 1.02 | A chain shall have the ability to export the Interchain Accounts controller genesis state. | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/controller/keeper/genesis_test.go#L47) | `Implemented` | 
| 1.03 | A chain shall have the ability to initialize the Interchain Accounts controller genesis state. | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/controller/keeper/genesis_test.go#L10) | `Implemented` | 
| 1.04 | A chain shall have the ability to set the Interchain Accounts controller parameters when upgrading or via proposal. | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/module_test.go#L33) | `Implemented` | 
| 1.05 | A chain shall have the ability to enable or disable Interchain Accounts host functionality in the genesis state. | The host parameters have a [flag](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/host/types/host.pb.go#L30) to enable/disable the host submodule, and this flag [is stored during genesis initialization](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/host/keeper/params.go#L31) | `Implemented` |
| 1.06 | A chain shall have the ability to export the Interchain Accounts host genesis state. | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/host/keeper/genesis_test.go#L46) | `Implemented` | 
| 1.07 | A chain shall have the ability to initialize the Interchain Accounts host genesis state. | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/host/keeper/genesis_test.go#L10) | `Implemented` | 
| 1.08 | A chain shall have the ability to set the Interchain Accounts host parameters when upgrading or via proposal. | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/module_test.go#L33) | `Implemented` | 
| 1.09 | The host chain shall have the ability to whitelist what types of messages or transactions that it chooses to facilitate (e.g. it can decide that registered interchain accounts cannot execute staking messages). | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/host/keeper/params_test.go#L5) | `Implemented` | 

### 2 - Registration

| ID  | Description | Verification | Status |
| --- | ----------- | ------------ | ------ |
| 2.01 | The controller chain can programmatically create interchain accounts on the host chain that shall be controlled only by the owner account on the controller chain. | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/host/keeper/account_test.go#L10) | `Implemented` |
| 2.02 | An interchain account shall be created by any actor without the approval of a third party (e.g. chain governance). | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/host/keeper/account_test.go#L10) | `Implemented` |

### 3 - Control

| ID  | Description | Verification | Status | 
| --- | ----------- | ------------ | ------ | 
| 3.01 | The controller chain can programmatically control the interchain account by submitting transactions to be executed on the host chain on the behalf of the interchain account. | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/controller/keeper/relay_test.go#L29) | `Implemented` | 
| 3.02 | Under no circumstances shall the owner account on the controller chain irretrievably lose control over the registered interchain account on the host chain. | If the channel between controller and host closes, then [a relayer can open a new channel on the existing controller port](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/controller/keeper/account.go#L16-L17). | `Implemented` | 

### 4 - Host execution

| ID  | Description | Verification | Status | 
| --- | ----------- | ------------ | ------ | 
| 4.01 | Transactions shall be executed by an interchain account on the host chain in exactly the same order in which they are submitted by the controller chain. | IBC packets with SDK messages will be sent from the controller to the host over an [ordered channel](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/controller/keeper/account.go#L60). | `Implemented` | 
| 4.02 | The host shall execute only messages in the allow list. | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/host/keeper/relay_test.go#L340) | `Implemented` |
| 4.03 | The controller chain shall know how the host chain will handle the transaction bytes in advance. | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/controller/keeper/handshake_test.go#L109-L133) | `Implemented` |
| 4.04 | Each transaction submitted by the controller chain shall be executed only once by the interchain account on the host chain. | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/host/keeper/relay_test.go#L248) | `Implemented` |

# Non-functional requirements

## 5 - Security

| ID | Description | Verification | Status |  
| -- | ----------- | ------------ | ------ | 
| 5.01 | There shall be no means for the interchain account to execute transactions that have not been submitted first by the respective owner account on the controller chain. |[Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/host/keeper/relay_test.go#L361) |  `Implemented` | 
| 5.02 | Every interchain account on the host chain shall have one and only one respective owner account on the controller chain. | The interchain account on the host [is generated using the host connection ID and the address of the owner on the controller](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/host/keeper/handshake.go#L73-L76). | `Implemented` |
| 5.03 | The owner account on a controller chain shall not be able to control interchain accounts registered by other owner accounts on the same controller chain. | Before the host logic executes the received messages, it [retrieves the interchain account associated with the port ID](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/host/keeper/relay.go#L94) over which it received the message. For owner address B to be able to execute a message with the interchain account registered with owner address A, it would need to send the messages over a channel that binds to a port ID that contains the owner address A, and since we have assumption number 3, this should not be allowed by applications. | `Implemented` | 
| 5.04 | A controller chain shall not be able to control interchain accounts registered by owner accounts on different controller chains. | Same as 5.03. | `Implemented` |  |
| 5.05 | Each interchain account on the host chain is owned by a single owner account on the controller chain. It shall not be possible to register a second interchain account with the same owner account on the controller chain. | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/controller/keeper/account_test.go#L42) | `Implemented` |

# External interface requirements

## 6 - CLI

| ID | Description | Verification | Status | 
| -- | ----------- | ------------ | ------ | 
| 6.01 | There shall be a CLI command available to query the host parameters. | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/host/client/cli/query.go#L22) | `Implemented` |
| 6.02 | There shall be a CLI command available to query the receive packet events on the host chain to check the result of the execution of the message on the host. | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/host/client/cli/query.go#L51) | `Implemented` | 
| 6.03 | There shall be a CLI command available to query the controller parameters. | [Acceptance tests](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/controller/client/cli/query.go#L15) | `Implemented` | 


## 7 - Application developers

| ID | Description | Verification | Status | 
| -- | ----------- | ------------ | ------ |
| 7.01 | An IBC application developer shall be able to develop an Interchain Accounts authentication module that can register interchain accounts. | The [`RegisterInterchainAccount` function](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/controller/keeper/account.go#L18) is the entry point to registering an interchain account. | `Implemented` | 
| 7.02 | An IBC application developer shall be able to develop an Interchain Accounts authentication module that can send messages from the controller to the host. | The [`SendTx` function](https://github.com/cosmos/ibc-go/blob/v3.0.0/modules/apps/27-interchain-accounts/controller/keeper/relay.go#L18) takes pre-built packet data containing messages to be executed on the host chain from an authentication module and attempts to send the packet. | `Implemented` | 