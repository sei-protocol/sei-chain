## Abstract

Sei Network has an `oracle` module to support asset exchange rate pricing for use by other modules and contracts. When validating for the network, participation as an Oracle is expected and required in order to ensure the most reliable and accurate pricing for assets.

For oracle pricing, the voting rounds have several steps to ensure integrity and consensus of pricing data prior to accepting the exchange rates as the source of truth. In each voting period, there are two aggregation steps that oracles must participate in.

The prevote step is a step where a validator provides their oracle pricing submission during voting window X for the next voting window X+1. In this prevote step, the validator hashes their proposed exchange rates to prevent other validators from simply copying that validator's votes.

In the vote step for window X, the validator provides their proposed exchange rates for the current window. These are hashed and compared with the prevotes from window X-1 to ensure that the voted values haven't changed across the voting window. At the end of the voting period, all of the exchange rate votes are accumulated and a weighted median is computed (weighted by validator voting power) to determine the true exchange rate for each asset.

There are penalties for non-participation and participation with bad data. Validators have a miss count that tracks the number of voting windows in which a validator has either not provided data or provided data that deviated too much from the weighted median. In a given number of voting periods, if a validators miss count is too high, they are slashed as a penalty for misbehaving over an extended period of time.

TODO: Populate Oracle README Contents below.

## Contents

## Concepts

### Voting Procedure

### Reward Band

### Slashing

### Abstaining from Voting

## State

## Messages

## Events

## Hooks

## Parameters

## Transactions

## Queries
