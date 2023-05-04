## Abstract

Sei Network has an `oracle` module to support asset exchange rate pricing for use by other modules and contracts. When validating for the network, participation as an Oracle is expected and required in order to ensure the most reliable and accurate pricing for assets.

In the vote step for window X, the validator provides their proposed exchange rates for the current window. At the end of the voting period, all of the exchange rate votes are accumulated and a weighted median is computed (weighted by validator voting power) to determine the true exchange rate for each asset.

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
