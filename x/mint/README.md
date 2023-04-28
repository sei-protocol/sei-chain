# x/mint

## Overview

The minting mechanism was designed for creating new tokens according to a predefined schedule. It allows the creation of scheduled token release structures that define the release of tokens over a period of time. The Mint module provides a system for managing token minting, release schedule, and related parameters. It has been designed to be flexible and adaptable to a range of use-cases.

A key aim of the minting mechanism is to reach a state where there is no more inflation and the network enters a deflationary state, with no additional tokens being introduced into the network.

Minting is designed to occur over a specified period with a proportion of the total mint amount distributed daily (UTC). This approach incentivizes users to stake their tokens for longer durations.

### Minting Mechanism

The minting mechanism is built on a daily distribution model. The total_mint_amount is a predefined amount of tokens set to be minted within the duration specified by a start and end date. This total amount is evenly distributed over each day of the minting period, ensuring a consistent daily distribution of tokens.

### Daily Mint Calculation

The daily mint amount is derived by dividing the `remaining_mint_amount` by the number of days left in the minting period. This calculation is based on the assumption of a uniform distribution of tokens throughout the period, barring any instances where the chain is down for more than a day.

For example, if the `total_mint_amount` is set to 1,000,000 tokens and the minting period is 100 days, the daily mint amount would be 10,000 tokens. However, if there was a network outage and the chain was down for 1 day, the daily mint amount would be recalculated. If 500,000 tokens had already been distributed in the first 50 days, with 49 days remaining and a `remaining_mint_amount` of 500,000 tokens, the revised daily mint amount would be 10,204 tokens. This adjusted amount would be minted daily until the 100th day, when 10,208 tokens would be minted to achieve the total of 1,000,000 tokens.

### Minting Process

Every day, at a configured time (typically the start of the day), the daily mint amount is created and distributed to the fee_collector account. From here, it's distributed to stakers in the same manner as transaction fees (percentage-based).

### Updating the Minting Schedule

The minting schedule, including the start date, end date, and `total_mint_amount`, can be updated through a governance proposal. This feature allows network participants to adjust the minting parameters as necessary in response to the network's needs and conditions.

This flexibility ensures that the minting process can be adjusted and managed effectively over time, supporting the growth and sustainability of the Sei-chain network.

Note: Changes to the `total_mint_amount` or `remaining_mint_amont` after the start date will not impact tokens already minted.

## State

### Minter

The minter is a space for holding current inflation information. This can be updated through a proposal, it will be discussed more in the later sections.

```go
type Minter struct {
    // The day where the mint begins
    StartDate           string `protobuf:"bytes,1,opt,name=start_date,json=startDate,proto3" json:"start_date,omitempty"`
    // The day where the mint ends
    EndDate             string `protobuf:"bytes,2,opt,name=end_date,json=endDate,proto3" json:"end_date,omitempty"`
    // Denom for the coins minted, defaults to usei
    Denom               string `protobuf:"bytes,3,opt,name=denom,proto3" json:"denom,omitempty"`
    // Total amount to be minted
    TotalMintAmount     uint64 `protobuf:"varint,4,opt,name=total_mint_amount,json=totalMintAmount,proto3" json:"total_mint_amount,omitempty"`
    // Remaining amount to be minted
    RemainingMintAmount uint64 `protobuf:"varint,5,opt,name=remaining_mint_amount,json=remainingMintAmount,proto3" json:"remaining_mint_amount,omitempty"`
    // Last amount minted (usually from the day before)
    LastMintAmount      uint64 `protobuf:"varint,6,opt,name=last_mint_amount,json=lastMintAmount,proto3" json:"last_mint_amount,omitempty"`
    // Last day minted
    LastMintDate        string `protobuf:"bytes,7,opt,name=last_mint_date,json=lastMintDate,proto3" json:"last_mint_date,omitempty"`
    // The height of the last mint
    LastMintHeight      uint64 `protobuf:"varint,8,opt,name=last_mint_height,json=lastMintHeight,proto3" json:"last_mint_height,omitempty"`
}
```

### Params

The mint module stores it's params in state, it can be updated with governance or the address with authority.

```go
type Params struct {
    // type of coin to mint
    MintDenom string `protobuf:"bytes,1,opt,name=mint_denom,json=mintDenom,proto3" json:"mint_denom,omitempty"`
    // List of token release schedules
    TokenReleaseSchedule []ScheduledTokenRelease `protobuf:"bytes,2,rep,name=token_release_schedule,json=tokenReleaseSchedule,proto3" json:"token_release_schedule" yaml:"token_release_schedule"`
}
...
type ScheduledTokenRelease struct {
    // The day where the mint begins
    StartDate          string `protobuf:"bytes,1,opt,name=start_date,json=startDate,proto3" json:"start_date,omitempty"`
    // The day where the mint ends
    EndDate            string `protobuf:"bytes,2,opt,name=end_date,json=endDate,proto3" json:"end_date,omitempty"`
    // Total amount to be minted
    TokenReleaseAmount uint64 `protobuf:"varint,3,opt,name=token_release_amount,json=tokenReleaseAmount,proto3" json:"token_release_amount,omitempty"`
}

```

### Governance

#### Minter Governance Proposal
Here is an example of how to submit a governance proposal to update the Minter parameters:

First, prepare a proposal in JSON format, like the minter_prop.json file below:

```json
{
  "title": "Test Update Minter",
  "description": "Updating test minter",
  "minter": {
    "start_date": "2023-10-05",
    "end_date": "2023-11-22",
    "denom": "usei",
    "total_mint_amount": 100000
  }
}
```

Then, submit the proposal with the following command:

```bash
seid tx gov submit-proposal update-minter ./minter_prop.json --deposit 20sei --from admin -b block -y --gas 200000 --fees 2000usei
```

This command submits a proposal to update the minter. The --deposit flag is used to provide the initial deposit. The proposal is submitted by the address provided with the --from flag.

Before the proposal, the Minter parameters might look like this:

```**bash**
> seid q mint minter
denom: usei
end_date: "2023-04-30"
last_mint_amount: "333333333333"
last_mint_date: "2023-04-27"
last_mint_height: "0"
remaining_mint_amount: "666666666666"
start_date: "2023-04-27"
total_mint_amount: "999999999999"
```

After the proposal is passed, the Minter parameters would be updated as per the proposal:

```bash
> seid q mint minter
denom: usei
end_date: "2023-11-22"
last_mint_amount: "0"
last_mint_date: ""
last_mint_height: "0"
remaining_mint_amount: "0"
start_date: "2023-10-05"
total_mint_amount: "100000"
```

In this example, the end_date has been changed to "2023-11-22", start_date is now "2023-10-05", and total_mint_amount has been reduced to "100000".

### Params Governance Proposal

Here is an example for updating the params for the mint module

```json
{
  "title": "Param Change Proposal",
  "description": "Proposal to change some parameters",
  "changes": [
    {
      "subspace": "mint",
      "key": "MintDenom",
      "value": "usei"
    },
    {
      "subspace": "mint",
      "key": "TokenReleaseSchedule",
      "value": [
        {
          "token_release_amount": 500,
          "start_date": "2023-10-01",
          "end_date": "2023-10-30"
        },
        {
          "token_release_amount": 1000,
          "start_date": "2023-11-01",
          "end_date": "2023-11-30"
        }
      ]
    }
  ]
}
```

Submit the proposal

```bash
seid tx gov submit-proposal param-change ./param_change_prop.json --from admin -b block -y --gas 200000 --fees 200000usei
```

## Begin-Block

At the end of each epoch (defaults to 60s), the chain checks if it's the minting start date, if it is, it will mint the amount of tokens specified in the params or continue the current release period and mint a subset of the remaining amount.

### Minting events

#### Type: Mint

- mint_date: date of the mint
- mint_epoch: epoch of the mint
- amount: amount minted


### Metrics

The minting module emits a `sei_mint_coins{denom}` each time there's a successful minting event.
