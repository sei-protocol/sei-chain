<!--
order: 1
-->

# Overview

Learn about what the Interchain Accounts module is, and how to build custom modules that utilize Interchain Accounts functionality {synopsis}


## What is the Interchain Accounts module?

Interchain Accounts is the Cosmos SDK implementation of the ICS-27 protocol, which enables cross-chain account management built upon IBC. Chains using the Interchain Accounts module can programmatically create accounts on other chains and control these accounts via IBC transactions. 

Interchain Accounts exposes a simple-to-use API which means IBC application developers do not require an in-depth knowledge of the underlying low-level details of IBC or the ICS-27 protocol. 

Developers looking to build upon Interchain Accounts must write custom logic in their own IBC application module, called authentication modules.

- How is an interchain account different than a regular account?

Regular accounts use a private key to sign transactions on-chain. Interchain Accounts are instead controlled programmatically by separate chains via IBC transactions. Interchain Accounts are implemented as sub-accounts of the interchain accounts module account. 

## Concepts 

`Host Chain`: The chain where the interchain account is registered. The host chain listens for IBC packets from a controller chain which should contain instructions (e.g. cosmos SDK messages) for which the interchain account will execute.

`Controller Chain`: The chain registering and controlling an account on a host chain. The controller chain sends IBC packets to the host chain to control the account. A controller chain must have at least one interchain accounts authentication module in order to act as a controller chain. 

`Authentication Module`: A custom IBC application module on the controller chain that uses the Interchain Accounts module API to build custom logic for the creation & management of interchain accounts. For a controller chain to utilize the interchain accounts module functionality, an authentication module is required.

`Interchain Account`: An account on a host chain. An interchain account has all the capabilities of a normal account. However, rather than signing transactions with a private key, a controller chain's authentication module will send IBC packets to the host chain which signals what transactions the interchain account should execute.
	
## SDK Security Model

SDK modules on a chain are assumed to be trustworthy.  For example, there are no checks to prevent an untrustworthy module from accessing the bank keeper.

The implementation of ICS27 on ibc-go uses this assumption in its security considerations. The implementation assumes the authentication module will not try to open channels on owner addresses it does not control. 

The implementation assumes other IBC application modules will not bind to ports within the ICS27 namespace. 
