# IBC retirement

IBC core and ICS-20 execution are removed from v6.7 nodes. Pre-v6.7 IBC
history, transaction decoding, queries, and precompile tracing are served by
v6.6 freeze nodes.

CosmWasm contracts retain their recorded IBC port metadata. During contract
execution, the `PortID` query remains available. Channel and channel-list
queries are unsupported, and every IBC send, transfer, or close message returns
a deterministic unsupported-operation error. ABCI smart queries do not expose
IBC data.

The `ibc`, `transfer`, and `capability` stores remain mounted for state
compatibility. Preserve the state  database when those retired stores are
required.
