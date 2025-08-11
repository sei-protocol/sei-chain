<!--
order: 4
-->

# Parameters

The Interchain Accounts module contains the following on-chain parameters, logically separated for each distinct submodule:

### Controller Submodule Parameters

| Key                    | Type | Default Value |
|------------------------|------|---------------|
| `ControllerEnabled`    | bool | `true`        |

#### ControllerEnabled

The `ControllerEnabled` parameter controls a chains ability to service ICS-27 controller specific logic. This includes the sending of Interchain Accounts packet data as well as the following ICS-26 callback handlers:
- `OnChanOpenInit`
- `OnChanOpenAck`
- `OnChanCloseConfirm`
- `OnAcknowledgementPacket`
- `OnTimeoutPacket`

### Host Submodule Parameters

| Key                    | Type     | Default Value |
|------------------------|----------|---------------|
| `HostEnabled`          | bool     | `true`        |
| `AllowMessages`        | []string | `[]`          |

#### HostEnabled

The `HostEnabled` parameter controls a chains ability to service ICS27 host specific logic. This includes the following ICS-26 callback handlers:
- `OnChanOpenTry`
- `OnChanOpenConfirm`
- `OnChanCloseConfirm`
- `OnRecvPacket`

#### AllowMessages

The `AllowMessages` parameter provides the ability for a chain to limit the types of messages or transactions that hosted interchain accounts are authorized to execute by defining an allowlist using the Protobuf message TypeURL format.

For example, a Cosmos SDK based chain that elects to provide hosted Interchain Accounts with the ability of governance voting and staking delegations will define its parameters as follows:

```
"params": {
    "host_enabled": true,
    "allow_messages": ["/cosmos.staking.v1beta1.MsgDelegate", "/cosmos.gov.v1beta1.MsgVote"]
}
```
There is also a special wildcard `"*"` message type which allows any type of message to be executed by the interchain account. This must be the only message in the `allow_messages` array.

```
"params": {
    "host_enabled": true,
    "allow_messages": ["*"]
}
```