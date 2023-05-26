# ADR 003: ICS27 Acknowledgement Format

## Changelog
* January 28th, 2022: Initial Draft

## Status

Accepted

## Context

Upon receiving an IBC packet, an IBC application can optionally return an acknowledgement. 
This acknowledgement will be hashed and written into state. Thus any changes to the information included in an acknowledgement are state machine breaking. 

ICS27 executes transactions on behalf of a controller chain. Information such as the message result or message error may be returned from other SDK modules outside the control of the ICS27 module. 
It might be very valuable to return message execution information inside the ICS27 acknowledgement so that controller chain interchain account auth modules can act upon this information. 
Only determinstic information returned from the message execution is allowed to be returned in the packet acknowledgement otherwise the network will halt due to a fork in the expected app hash. 

## Decision

At the time of this writing, Tendermint includes the following information in the [ABCI.ResponseDeliverTx](https://github.com/tendermint/tendermint/blob/release/v0.34.13/types/results.go#L47-#L53):
```go
// deterministicResponseDeliverTx strips non-deterministic fields from
// ResponseDeliverTx and returns another ResponseDeliverTx.
func deterministicResponseDeliverTx(response *abci.ResponseDeliverTx) *abci.ResponseDeliverTx {
	return &abci.ResponseDeliverTx{
		Code:      response.Code,
		Data:      response.Data,
		GasWanted: response.GasWanted,
		GasUsed:   response.GasUsed,
	}
}
```

### Successful acknowledgements

Successful acknowledgements should return information about the transaction execution. 
Given the determinstic fields in the `abci.ResponseDeliverTx`, the transaction `Data` can be used to indicate information about the transaction execution. 
The `abci.ResponseDeliverTx.Data` will be set in the ICS27 packet acknowledgement upon successful transaction execution.

The format for the `abci.ResponseDeliverTx.Data` is constructed by the SDK. 

At the time of this writing, the next major release of the SDK will change the format for constructing the transaction response data. 

#### v0.45 format

The current version, v0.45 constructs the transaction response as follows:
```go
    proto.Marshal(&sdk.TxMsgData{
        Data: []*sdk.MsgData{msgResponses...}, 
    }
```

Where `msgResponses` is a slice of `*sdk.MsgData`. 
The `MsgData.MsgType` contains the `sdk.MsgTypeURL` of the `sdk.Msg` being executed.
The `MsgData.Data` contains the proto marshaled `MsgResponse` for the associated message executed. 

#### Next major version format

The next major version will construct the transaction response as follows:
```go 
    proto.Marshal(&sdk.TxMsgData{
        MsgResponses: []*codectypes.Any{msgResponses...}, 
    }
```

Where `msgResponses` is a slice of the `MsgResponse`s packed into `Any`s.

#### Forwards compatible approach

A forwards compatible approach was deemed infeasible. 
The `handler` provided by the `MsgServiceRouter` will only include the `*sdk.Result` and an error (if one occurred). 
In v0.45 of the SDK, the `*sdk.Result.Data` will contain the MsgResponse marshaled data. 
However, the MsgResponse is not packed and marshaled as a `*codectypes.Any`, thus making it impossible from a generalized point of view to unmarshal the bytes. 
If the bytes could be unmarshaled, then they could be packed into an `*codectypes.Any` in antcipation of the upcoming format.  

Intercepting the MsgResponse before it becomes marshaled requires replicating this [code](https://github.com/cosmos/cosmos-sdk/blob/dfd47f5b449f558a855da284a9a7eabbfbad435d/baseapp/msg_service_router.go#L109-#L128). 
It may not even be possible to replicate the linked code. The method handler would need to be accessed somehow.

For these reasons it is deemed infeasible to attempt a fowards compatible approach. 

ICA auth developers can interpret which format was used when constructing the transaction response by checking if the `sdk.TxMsgData.Data` field is non-empty. 
If the `sdk.TxMsgData.Data` field is not empty then the format for v0.45 was used, otherwise ICA auth developers can assume the transaction response uses the newer format.


#### Decision

Replicate the transaction response format as provided by the current SDK verison. 
When the SDK version changes, adjust the transaction response format to use the updated transaction response format. 
Include the transaction response bytes in the result channel acknowledgement. 

A test has been [written](https://github.com/cosmos/ibc-go/blob/v3.0.0-beta1/modules/apps/27-interchain-accounts/host/ibc_module_test.go#L716-#L774) to fail if the `MsgResponse` is no longer included in consensus.

### Error acknowledgements

As indicated above, the `abci.ResponseDeliverTx.Code` is determinstic. 
Upon transaction execution errors, an error acknowledgement should be returned including the abci code. 

A test has been [written](https://github.com/cosmos/ibc-go/blob/v3.0.0-beta1/modules/apps/27-interchain-accounts/host/types/ack_test.go#L41-#L82) to fail if the ABCI code is no longer determinstic.

## Consequences

> This section describes the consequences, after applying the decision. All consequences should be summarized here, not just the "positive" ones.

### Positive

- interchain account auth modules can act upon transaction results without requiring a query module
- transaction results align with those returned by execution of a normal SDK message.

### Negative

- the security assumptions of this decision rest on the inclusion of the ABCI error code and the Msg response in the ResponseDeliverTx hash created by Tendermint
- events are non-determinstic and cannot be included in the packet acknowledgement

### Neutral

No neutral consequences.

