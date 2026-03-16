# x/slashing

## Overview
To understand how the original slashing module works in general, please refer to: [slashing spec](https://github.com/sei-protocol/sei-cosmos/tree/main/x/slashing/spec).

The slashing module enables Cosmos SDK-based blockchains to disincentivize any attributable action
by a protocol-recognized actor with value at stake by penalizing them ("slashing").

Penalties may include, but are not limited to:
* Slash some amount of their stake
* Removing their ability to vote on future blocks for a period of time.

## State

### ValidatorSignInfo
Validators are penalized for failing to be included in the LastCommitInfo for some number of blocks by being automatically jailed, potentially slashed, and unbonded.

Information about validator's liveness activity is tracked through ValidatorSigningInfo.

```go
// ValidatorSigningInfo defines the signing info for a validator
type ValidatorSigningInfo struct {
    // validator consensus address
    Address             sdk.ConsAddress `json:"address" yaml:"address"`
    // height at which validator was first a candidate OR was unjailed
    StartHeight         int64           `json:"start_height" yaml:"start_height"`   
    // index offset into signed block bit array
    IndexOffset         int64           `json:"index_offset" yaml:"index_offset"`
    // timestamp validator cannot be unjailed until
    JailedUntil         time.Time       `json:"jailed_until" yaml:"jailed_until"` 
    // whether or not a validator has been tombstoned (killed out of validator set)
    Tombstoned          bool            `json:"tombstoned" yaml:"tombstoned"`
    // missed blocks counter (to avoid scanning the array every time)
    MissedBlocksCounter int64           `json:"missed_blocks_counter" yaml:"missed_blocks_counter"` 
}
```

### MissedBlocksBitArray
`MissedBlocksBitArray` acts as a bitpack-array of size SignedBlocksWindow that tells us if the validator missed the block for a given index in the bit-array. 

Different from open source, each uint64 has 64 bits, and each bit represent a bool value of either 0 or 1, where 0 indicates the validator did not miss (did sign) the corresponding block, and 1 indicates they missed the block (did not sign).

Note that the `MissedBlocksBitArray` is not explicitly initialized up-front. Keys are added as we progress through the first SignedBlocksWindow blocks for a newly bonded validator. 

The SignedBlocksWindow parameter defines the size (number of blocks) of the sliding window used to track validator liveness.

The window size determines how many bits need to be packed into `MissedBlocks`, taking the floor of `WindowSize` divided by 64 determines the size of the `MissedBlocks` array
```go
// Stores a sliding window of the last `signed_blocks_window` blocks indicating whether the validator missed the block
type ValidatorMissedBlockArray struct {
	// validator address
	Address string          `protobuf:"bytes,1,opt,name=address,proto3" json:"address,omitempty"`
	// store this in case window size changes but doesn't affect number of bit groups
	WindowSize int64        `protobuf:"varint,2,opt,name=window_size,json=windowSize,proto3" json:"window_size,omitempty"`
	// Array of contains the missed block bits packed into uint64s
	MissedBlocks []uint64   `protobuf:"varint,3,rep,packed,name=missed_blocks,json=missedBlocks,proto3" json:"missed_blocks,omitempty" yaml:"missed_blocks"`
}

Sample MissedBlocks: [1593,2453]
[0000000000000...0000011000111001,0000000000000000...0000100110010101]
|-----------64 bits--------------|---------------64 bits--------------|

```

### Params
The slashing module contains the following parameters:

| Key                     | Type           | Example                |
| ----------------------- | -------------- | ---------------------- |
| SignedBlocksWindow      | string (int64) | "100"                  |
| MinSignedPerWindow      | string (dec)   | "0.500000000000000000" |
| DowntimeJailDuration    | string (ns)    | "600000000000"         |
| SlashFractionDoubleSign | string (dec)   | "0.050000000000000000" |
| SlashFractionDowntime   | string (dec)   | "0.010000000000000000" |

Those parameters can be updated via gov proposal.

```go
// Params - used for initializing default parameter for slashing at genesis
type Params struct {
	
    SignedBlocksWindow      int64         `json:"signed_blocks_window" yaml:"signed_blocks_window"`
    MinSignedPerWindow      sdk.Dec       `json:"min_signed_per_window" yaml:"min_signed_per_window"`
    DowntimeJailDuration    time.Duration `json:"downtime_jail_duration" yaml:"downtime_jail_duration"`
    SlashFractionDoubleSign sdk.Dec       `json:"slash_fraction_double_sign" yaml:"slash_fraction_double_sign"`
    SlashFractionDowntime   sdk.Dec       `json:"slash_fraction_downtime" yaml:"slash_fraction_downtime"`
}

```

## Governance Proposal

TODO


## Begin-Block
At the beginning of each block, we update the ValidatorSigningInfo for each validator and check if they've crossed below the liveness threshold over a sliding window.

This sliding window is defined by SignedBlocksWindow and the index in this window is determined by IndexOffset found in the validator's ValidatorSigningInfo.

For each block processed, the IndexOffset is incremented regardless if the validator signed or not. Once the index is determined, the MissedBlocksBitArray and MissedBlocksCounter are updated accordingly.

Here we parallelize the logic of fetching the existing validators to gather `ValidatorSigningInfo` and `ValidatorMissedBlockArray`
```go
// Iterate over all the validators which *should* have signed this block
// store whether or not they have actually signed it and slash/unbond any
// which have missed too many blocks in a row (downtime slashing)

// this allows us to preserve the original ordering for writing purposes
var wg sync.WaitGroup

slashingWriteInfo := make([]*SlashingWriteInfo, len(req.LastCommitInfo.GetVotes()))

for i, voteInfo := range req.LastCommitInfo.GetVotes() {
    wg.Add(1)
    go func(valIndex int, vInfo abci.VoteInfo) {
        defer wg.Done()
        consAddr, missedInfo, signInfo, shouldSlash, slashInfo := k.HandleValidatorSignatureConcurrent(ctx, vInfo.Validator.Address, vInfo.Validator.Power, vInfo.SignedLastBlock)
        slashingWriteInfo[valIndex] = &SlashingWriteInfo{
            ConsAddr:    consAddr,
            MissedInfo:  missedInfo,
            SigningInfo: signInfo,
            ShouldSlash: shouldSlash,
            SlashInfo:   slashInfo,
        }
    }(i, voteInfo)
}
wg.Wait()
```

Then for each validator, we will update its corresponding sign info for the current block.
```go
for _, writeInfo := range slashingWriteInfo {
    if writeInfo == nil {
        panic("Expected slashing write info to be non-nil")
    }
    // Update the validator missed block bit array by index if different from last value at the index
    if writeInfo.ShouldSlash {
        k.ClearValidatorMissedBlockBitArray(ctx, writeInfo.ConsAddr)
    } else {
        k.SetValidatorMissedBlocks(ctx, writeInfo.ConsAddr, writeInfo.MissedInfo)
    }
    if writeInfo.ShouldSlash {
        writeInfo.SigningInfo = k.SlashJailAndUpdateSigningInfo(ctx, writeInfo.ConsAddr, writeInfo.SlashInfo, writeInfo.SigningInfo)
    }
    k.SetValidatorSigningInfo(ctx, writeInfo.ConsAddr, writeInfo.SigningInfo)
}
```

## Metrics
- `missing_signature` is a per validator counter metric, which is incremented everytime a validator is slashed or jailed due to missing too many signatures.
- `begin_blocker` is a per module histogram metric, for slashing it measures the latency for BeginBlock execution.
- `double_sign` is a per validator counter metric, which is incremented everytime a validator got slashed for double sign.
