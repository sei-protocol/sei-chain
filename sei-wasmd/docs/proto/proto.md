# Protocol Documentation
<a name="top"></a>

## Table of Contents

- [x/wasm/types/genesis.proto](#x/wasm/types/genesis.proto)
    - [Code](#cosmwasm.wasm.v1beta1.Code)
    - [Contract](#cosmwasm.wasm.v1beta1.Contract)
    - [GenesisState](#cosmwasm.wasm.v1beta1.GenesisState)
    - [GenesisState.GenMsgs](#cosmwasm.wasm.v1beta1.GenesisState.GenMsgs)
    - [Sequence](#cosmwasm.wasm.v1beta1.Sequence)
  
- [x/wasm/types/ibc.proto](#x/wasm/types/ibc.proto)
    - [MsgIBCCloseChannel](#cosmwasm.wasm.v1beta1.MsgIBCCloseChannel)
    - [MsgIBCSend](#cosmwasm.wasm.v1beta1.MsgIBCSend)
  
- [x/wasm/types/proposal.proto](#x/wasm/types/proposal.proto)
    - [ClearAdminProposal](#cosmwasm.wasm.v1beta1.ClearAdminProposal)
    - [InstantiateContractProposal](#cosmwasm.wasm.v1beta1.InstantiateContractProposal)
    - [MigrateContractProposal](#cosmwasm.wasm.v1beta1.MigrateContractProposal)
    - [PinCodesProposal](#cosmwasm.wasm.v1beta1.PinCodesProposal)
    - [StoreCodeProposal](#cosmwasm.wasm.v1beta1.StoreCodeProposal)
    - [UnpinCodesProposal](#cosmwasm.wasm.v1beta1.UnpinCodesProposal)
    - [UpdateAdminProposal](#cosmwasm.wasm.v1beta1.UpdateAdminProposal)
  
- [x/wasm/types/query.proto](#x/wasm/types/query.proto)
    - [CodeInfoResponse](#cosmwasm.wasm.v1beta1.CodeInfoResponse)
    - [ContractInfoWithAddress](#cosmwasm.wasm.v1beta1.ContractInfoWithAddress)
    - [QueryAllContractStateRequest](#cosmwasm.wasm.v1beta1.QueryAllContractStateRequest)
    - [QueryAllContractStateResponse](#cosmwasm.wasm.v1beta1.QueryAllContractStateResponse)
    - [QueryCodeRequest](#cosmwasm.wasm.v1beta1.QueryCodeRequest)
    - [QueryCodeResponse](#cosmwasm.wasm.v1beta1.QueryCodeResponse)
    - [QueryCodesRequest](#cosmwasm.wasm.v1beta1.QueryCodesRequest)
    - [QueryCodesResponse](#cosmwasm.wasm.v1beta1.QueryCodesResponse)
    - [QueryContractHistoryRequest](#cosmwasm.wasm.v1beta1.QueryContractHistoryRequest)
    - [QueryContractHistoryResponse](#cosmwasm.wasm.v1beta1.QueryContractHistoryResponse)
    - [QueryContractInfoRequest](#cosmwasm.wasm.v1beta1.QueryContractInfoRequest)
    - [QueryContractInfoResponse](#cosmwasm.wasm.v1beta1.QueryContractInfoResponse)
    - [QueryContractsByCodeRequest](#cosmwasm.wasm.v1beta1.QueryContractsByCodeRequest)
    - [QueryContractsByCodeResponse](#cosmwasm.wasm.v1beta1.QueryContractsByCodeResponse)
    - [QueryRawContractStateRequest](#cosmwasm.wasm.v1beta1.QueryRawContractStateRequest)
    - [QueryRawContractStateResponse](#cosmwasm.wasm.v1beta1.QueryRawContractStateResponse)
    - [QuerySmartContractStateRequest](#cosmwasm.wasm.v1beta1.QuerySmartContractStateRequest)
    - [QuerySmartContractStateResponse](#cosmwasm.wasm.v1beta1.QuerySmartContractStateResponse)
  
    - [Query](#cosmwasm.wasm.v1beta1.Query)
  
- [x/wasm/types/tx.proto](#x/wasm/types/tx.proto)
    - [MsgClearAdmin](#cosmwasm.wasm.v1beta1.MsgClearAdmin)
    - [MsgClearAdminResponse](#cosmwasm.wasm.v1beta1.MsgClearAdminResponse)
    - [MsgExecuteContract](#cosmwasm.wasm.v1beta1.MsgExecuteContract)
    - [MsgExecuteContractResponse](#cosmwasm.wasm.v1beta1.MsgExecuteContractResponse)
    - [MsgInstantiateContract](#cosmwasm.wasm.v1beta1.MsgInstantiateContract)
    - [MsgInstantiateContractResponse](#cosmwasm.wasm.v1beta1.MsgInstantiateContractResponse)
    - [MsgMigrateContract](#cosmwasm.wasm.v1beta1.MsgMigrateContract)
    - [MsgMigrateContractResponse](#cosmwasm.wasm.v1beta1.MsgMigrateContractResponse)
    - [MsgStoreCode](#cosmwasm.wasm.v1beta1.MsgStoreCode)
    - [MsgStoreCodeResponse](#cosmwasm.wasm.v1beta1.MsgStoreCodeResponse)
    - [MsgUpdateAdmin](#cosmwasm.wasm.v1beta1.MsgUpdateAdmin)
    - [MsgUpdateAdminResponse](#cosmwasm.wasm.v1beta1.MsgUpdateAdminResponse)
  
    - [Msg](#cosmwasm.wasm.v1beta1.Msg)
  
- [x/wasm/types/types.proto](#x/wasm/types/types.proto)
    - [AbsoluteTxPosition](#cosmwasm.wasm.v1beta1.AbsoluteTxPosition)
    - [AccessConfig](#cosmwasm.wasm.v1beta1.AccessConfig)
    - [AccessTypeParam](#cosmwasm.wasm.v1beta1.AccessTypeParam)
    - [CodeInfo](#cosmwasm.wasm.v1beta1.CodeInfo)
    - [ContractCodeHistoryEntry](#cosmwasm.wasm.v1beta1.ContractCodeHistoryEntry)
    - [ContractInfo](#cosmwasm.wasm.v1beta1.ContractInfo)
    - [Model](#cosmwasm.wasm.v1beta1.Model)
    - [Params](#cosmwasm.wasm.v1beta1.Params)
  
    - [AccessType](#cosmwasm.wasm.v1beta1.AccessType)
    - [ContractCodeHistoryOperationType](#cosmwasm.wasm.v1beta1.ContractCodeHistoryOperationType)
  
- [Scalar Value Types](#scalar-value-types)



<a name="x/wasm/types/genesis.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## x/wasm/types/genesis.proto



<a name="cosmwasm.wasm.v1beta1.Code"></a>

### Code
Code struct encompasses CodeInfo and CodeBytes


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code_id | [uint64](#uint64) |  |  |
| code_info | [CodeInfo](#cosmwasm.wasm.v1beta1.CodeInfo) |  |  |
| code_bytes | [bytes](#bytes) |  |  |
| pinned | [bool](#bool) |  | Pinned to wasmvm cache |






<a name="cosmwasm.wasm.v1beta1.Contract"></a>

### Contract
Contract struct encompasses ContractAddress, ContractInfo, and ContractState


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| contract_address | [string](#string) |  |  |
| contract_info | [ContractInfo](#cosmwasm.wasm.v1beta1.ContractInfo) |  |  |
| contract_state | [Model](#cosmwasm.wasm.v1beta1.Model) | repeated |  |






<a name="cosmwasm.wasm.v1beta1.GenesisState"></a>

### GenesisState
GenesisState - genesis state of x/wasm


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| params | [Params](#cosmwasm.wasm.v1beta1.Params) |  |  |
| codes | [Code](#cosmwasm.wasm.v1beta1.Code) | repeated |  |
| contracts | [Contract](#cosmwasm.wasm.v1beta1.Contract) | repeated |  |
| sequences | [Sequence](#cosmwasm.wasm.v1beta1.Sequence) | repeated |  |
| gen_msgs | [GenesisState.GenMsgs](#cosmwasm.wasm.v1beta1.GenesisState.GenMsgs) | repeated |  |






<a name="cosmwasm.wasm.v1beta1.GenesisState.GenMsgs"></a>

### GenesisState.GenMsgs
GenMsgs define the messages that can be executed during genesis phase in order.
The intention is to have more human readable data that is auditable.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| store_code | [MsgStoreCode](#cosmwasm.wasm.v1beta1.MsgStoreCode) |  |  |
| instantiate_contract | [MsgInstantiateContract](#cosmwasm.wasm.v1beta1.MsgInstantiateContract) |  |  |
| execute_contract | [MsgExecuteContract](#cosmwasm.wasm.v1beta1.MsgExecuteContract) |  |  |






<a name="cosmwasm.wasm.v1beta1.Sequence"></a>

### Sequence
Sequence key and value of an id generation counter


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id_key | [bytes](#bytes) |  |  |
| value | [uint64](#uint64) |  |  |





 

 

 

 



<a name="x/wasm/types/ibc.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## x/wasm/types/ibc.proto



<a name="cosmwasm.wasm.v1beta1.MsgIBCCloseChannel"></a>

### MsgIBCCloseChannel
MsgIBCCloseChannel port and channel need to be owned by the contract


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| channel | [string](#string) |  |  |






<a name="cosmwasm.wasm.v1beta1.MsgIBCSend"></a>

### MsgIBCSend
MsgIBCSend


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| channel | [string](#string) |  | the channel by which the packet will be sent |
| timeout_height | [uint64](#uint64) |  | Timeout height relative to the current block height. The timeout is disabled when set to 0. |
| timeout_timestamp | [uint64](#uint64) |  | Timeout timestamp (in nanoseconds) relative to the current block timestamp. The timeout is disabled when set to 0. |
| data | [bytes](#bytes) |  | data is the payload to transfer |





 

 

 

 



<a name="x/wasm/types/proposal.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## x/wasm/types/proposal.proto



<a name="cosmwasm.wasm.v1beta1.ClearAdminProposal"></a>

### ClearAdminProposal
ClearAdminProposal gov proposal content type to clear the admin of a contract.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| title | [string](#string) |  | Title is a short summary |
| description | [string](#string) |  | Description is a human readable text |
| contract | [string](#string) |  | Contract is the address of the smart contract |






<a name="cosmwasm.wasm.v1beta1.InstantiateContractProposal"></a>

### InstantiateContractProposal
InstantiateContractProposal gov proposal content type to instantiate a contract.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| title | [string](#string) |  | Title is a short summary |
| description | [string](#string) |  | Description is a human readable text |
| run_as | [string](#string) |  | RunAs is the address that is passed to the contract&#39;s environment as sender |
| admin | [string](#string) |  | Admin is an optional address that can execute migrations |
| code_id | [uint64](#uint64) |  | CodeID is the reference to the stored WASM code |
| label | [string](#string) |  | Label is optional metadata to be stored with a constract instance. |
| init_msg | [bytes](#bytes) |  | InitMsg json encoded message to be passed to the contract on instantiation |
| funds | [cosmos.base.v1beta1.Coin](#cosmos.base.v1beta1.Coin) | repeated | Funds coins that are transferred to the contract on instantiation |






<a name="cosmwasm.wasm.v1beta1.MigrateContractProposal"></a>

### MigrateContractProposal
MigrateContractProposal gov proposal content type to migrate a contract.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| title | [string](#string) |  | Title is a short summary |
| description | [string](#string) |  | Description is a human readable text |
| run_as | [string](#string) |  | RunAs is the address that is passed to the contract&#39;s environment as sender |
| contract | [string](#string) |  | Contract is the address of the smart contract |
| code_id | [uint64](#uint64) |  | CodeID references the new WASM code |
| migrate_msg | [bytes](#bytes) |  | MigrateMsg json encoded message to be passed to the contract on migration |






<a name="cosmwasm.wasm.v1beta1.PinCodesProposal"></a>

### PinCodesProposal
PinCodesProposal gov proposal content type to pin a set of code ids in the wasmvm cache.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| title | [string](#string) |  | Title is a short summary |
| description | [string](#string) |  | Description is a human readable text |
| code_ids | [uint64](#uint64) | repeated | CodeIDs references the new WASM codes |






<a name="cosmwasm.wasm.v1beta1.StoreCodeProposal"></a>

### StoreCodeProposal
StoreCodeProposal gov proposal content type to submit WASM code to the system


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| title | [string](#string) |  | Title is a short summary |
| description | [string](#string) |  | Description is a human readable text |
| run_as | [string](#string) |  | RunAs is the address that is passed to the contract&#39;s environment as sender |
| wasm_byte_code | [bytes](#bytes) |  | WASMByteCode can be raw or gzip compressed |
| source | [string](#string) |  | Source is a valid absolute HTTPS URI to the contract&#39;s source code, optional |
| builder | [string](#string) |  | Builder is a valid docker image name with tag, optional |
| instantiate_permission | [AccessConfig](#cosmwasm.wasm.v1beta1.AccessConfig) |  | InstantiatePermission to apply on contract creation, optional |






<a name="cosmwasm.wasm.v1beta1.UnpinCodesProposal"></a>

### UnpinCodesProposal
UnpinCodesProposal gov proposal content type to unpin a set of code ids in the wasmvm cache.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| title | [string](#string) |  | Title is a short summary |
| description | [string](#string) |  | Description is a human readable text |
| code_ids | [uint64](#uint64) | repeated | CodeIDs references the WASM codes |






<a name="cosmwasm.wasm.v1beta1.UpdateAdminProposal"></a>

### UpdateAdminProposal
UpdateAdminProposal gov proposal content type to set an admin for a contract.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| title | [string](#string) |  | Title is a short summary |
| description | [string](#string) |  | Description is a human readable text |
| new_admin | [string](#string) |  | NewAdmin address to be set |
| contract | [string](#string) |  | Contract is the address of the smart contract |





 

 

 

 



<a name="x/wasm/types/query.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## x/wasm/types/query.proto



<a name="cosmwasm.wasm.v1beta1.CodeInfoResponse"></a>

### CodeInfoResponse
CodeInfoResponse contains code meta data from CodeInfo


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code_id | [uint64](#uint64) |  | id for legacy support |
| creator | [string](#string) |  |  |
| data_hash | [bytes](#bytes) |  |  |
| source | [string](#string) |  |  |
| builder | [string](#string) |  |  |






<a name="cosmwasm.wasm.v1beta1.ContractInfoWithAddress"></a>

### ContractInfoWithAddress
ContractInfoWithAddress adds the address (key) to the ContractInfo representation


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| address | [string](#string) |  |  |
| contract_info | [ContractInfo](#cosmwasm.wasm.v1beta1.ContractInfo) |  |  |






<a name="cosmwasm.wasm.v1beta1.QueryAllContractStateRequest"></a>

### QueryAllContractStateRequest
QueryAllContractStateRequest is the request type for the Query/AllContractState RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| address | [string](#string) |  | address is the address of the contract |
| pagination | [cosmos.base.query.v1beta1.PageRequest](#cosmos.base.query.v1beta1.PageRequest) |  | pagination defines an optional pagination for the request. |






<a name="cosmwasm.wasm.v1beta1.QueryAllContractStateResponse"></a>

### QueryAllContractStateResponse
QueryAllContractStateResponse is the response type for the Query/AllContractState RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| models | [Model](#cosmwasm.wasm.v1beta1.Model) | repeated |  |
| pagination | [cosmos.base.query.v1beta1.PageResponse](#cosmos.base.query.v1beta1.PageResponse) |  | pagination defines the pagination in the response. |






<a name="cosmwasm.wasm.v1beta1.QueryCodeRequest"></a>

### QueryCodeRequest
QueryCodeRequest is the request type for the Query/Code RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code_id | [uint64](#uint64) |  | grpc-gateway_out does not support Go style CodID |






<a name="cosmwasm.wasm.v1beta1.QueryCodeResponse"></a>

### QueryCodeResponse
QueryCodeResponse is the response type for the Query/Code RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code_info | [CodeInfoResponse](#cosmwasm.wasm.v1beta1.CodeInfoResponse) |  |  |
| data | [bytes](#bytes) |  |  |






<a name="cosmwasm.wasm.v1beta1.QueryCodesRequest"></a>

### QueryCodesRequest
QueryCodesRequest is the request type for the Query/Codes RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| pagination | [cosmos.base.query.v1beta1.PageRequest](#cosmos.base.query.v1beta1.PageRequest) |  | pagination defines an optional pagination for the request. |






<a name="cosmwasm.wasm.v1beta1.QueryCodesResponse"></a>

### QueryCodesResponse
QueryCodesResponse is the response type for the Query/Codes RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code_infos | [CodeInfoResponse](#cosmwasm.wasm.v1beta1.CodeInfoResponse) | repeated |  |
| pagination | [cosmos.base.query.v1beta1.PageResponse](#cosmos.base.query.v1beta1.PageResponse) |  | pagination defines the pagination in the response. |






<a name="cosmwasm.wasm.v1beta1.QueryContractHistoryRequest"></a>

### QueryContractHistoryRequest
QueryContractHistoryRequest is the request type for the Query/ContractHistory RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| address | [string](#string) |  | address is the address of the contract to query |
| pagination | [cosmos.base.query.v1beta1.PageRequest](#cosmos.base.query.v1beta1.PageRequest) |  | pagination defines an optional pagination for the request. |






<a name="cosmwasm.wasm.v1beta1.QueryContractHistoryResponse"></a>

### QueryContractHistoryResponse
QueryContractHistoryResponse is the response type for the Query/ContractHistory RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| entries | [ContractCodeHistoryEntry](#cosmwasm.wasm.v1beta1.ContractCodeHistoryEntry) | repeated |  |
| pagination | [cosmos.base.query.v1beta1.PageResponse](#cosmos.base.query.v1beta1.PageResponse) |  | pagination defines the pagination in the response. |






<a name="cosmwasm.wasm.v1beta1.QueryContractInfoRequest"></a>

### QueryContractInfoRequest
QueryContractInfoRequest is the request type for the Query/ContractInfo RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| address | [string](#string) |  | address is the address of the contract to query |






<a name="cosmwasm.wasm.v1beta1.QueryContractInfoResponse"></a>

### QueryContractInfoResponse
QueryContractInfoResponse is the response type for the Query/ContractInfo RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| address | [string](#string) |  | address is the address of the contract |
| contract_info | [ContractInfo](#cosmwasm.wasm.v1beta1.ContractInfo) |  |  |






<a name="cosmwasm.wasm.v1beta1.QueryContractsByCodeRequest"></a>

### QueryContractsByCodeRequest
QueryContractsByCodeRequest is the request type for the Query/ContractsByCode RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code_id | [uint64](#uint64) |  | grpc-gateway_out does not support Go style CodID |
| pagination | [cosmos.base.query.v1beta1.PageRequest](#cosmos.base.query.v1beta1.PageRequest) |  | pagination defines an optional pagination for the request. |






<a name="cosmwasm.wasm.v1beta1.QueryContractsByCodeResponse"></a>

### QueryContractsByCodeResponse
QueryContractsByCodeResponse is the response type for the Query/ContractsByCode RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| contract_infos | [ContractInfoWithAddress](#cosmwasm.wasm.v1beta1.ContractInfoWithAddress) | repeated |  |
| pagination | [cosmos.base.query.v1beta1.PageResponse](#cosmos.base.query.v1beta1.PageResponse) |  | pagination defines the pagination in the response. |






<a name="cosmwasm.wasm.v1beta1.QueryRawContractStateRequest"></a>

### QueryRawContractStateRequest
QueryRawContractStateRequest is the request type for the Query/RawContractState RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| address | [string](#string) |  | address is the address of the contract |
| query_data | [bytes](#bytes) |  |  |






<a name="cosmwasm.wasm.v1beta1.QueryRawContractStateResponse"></a>

### QueryRawContractStateResponse
QueryRawContractStateResponse is the response type for the Query/RawContractState RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| data | [bytes](#bytes) |  | Data contains the raw store data |






<a name="cosmwasm.wasm.v1beta1.QuerySmartContractStateRequest"></a>

### QuerySmartContractStateRequest
QuerySmartContractStateRequest is the request type for the Query/SmartContractState RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| address | [string](#string) |  | address is the address of the contract |
| query_data | [bytes](#bytes) |  | QueryData contains the query data passed to the contract |






<a name="cosmwasm.wasm.v1beta1.QuerySmartContractStateResponse"></a>

### QuerySmartContractStateResponse
QuerySmartContractStateResponse is the response type for the Query/SmartContractState RPC method


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| data | [bytes](#bytes) |  | Data contains the json data returned from the smart contract |





 

 

 


<a name="cosmwasm.wasm.v1beta1.Query"></a>

### Query
Query provides defines the gRPC querier service

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ContractInfo | [QueryContractInfoRequest](#cosmwasm.wasm.v1beta1.QueryContractInfoRequest) | [QueryContractInfoResponse](#cosmwasm.wasm.v1beta1.QueryContractInfoResponse) | ContractInfo gets the contract meta data |
| ContractHistory | [QueryContractHistoryRequest](#cosmwasm.wasm.v1beta1.QueryContractHistoryRequest) | [QueryContractHistoryResponse](#cosmwasm.wasm.v1beta1.QueryContractHistoryResponse) | ContractHistory gets the contract code history |
| ContractsByCode | [QueryContractsByCodeRequest](#cosmwasm.wasm.v1beta1.QueryContractsByCodeRequest) | [QueryContractsByCodeResponse](#cosmwasm.wasm.v1beta1.QueryContractsByCodeResponse) | ContractsByCode lists all smart contracts for a code id |
| AllContractState | [QueryAllContractStateRequest](#cosmwasm.wasm.v1beta1.QueryAllContractStateRequest) | [QueryAllContractStateResponse](#cosmwasm.wasm.v1beta1.QueryAllContractStateResponse) | AllContractState gets all raw store data for a single contract |
| RawContractState | [QueryRawContractStateRequest](#cosmwasm.wasm.v1beta1.QueryRawContractStateRequest) | [QueryRawContractStateResponse](#cosmwasm.wasm.v1beta1.QueryRawContractStateResponse) | RawContractState gets single key from the raw store data of a contract |
| SmartContractState | [QuerySmartContractStateRequest](#cosmwasm.wasm.v1beta1.QuerySmartContractStateRequest) | [QuerySmartContractStateResponse](#cosmwasm.wasm.v1beta1.QuerySmartContractStateResponse) | SmartContractState get smart query result from the contract |
| Code | [QueryCodeRequest](#cosmwasm.wasm.v1beta1.QueryCodeRequest) | [QueryCodeResponse](#cosmwasm.wasm.v1beta1.QueryCodeResponse) | Code gets the binary code and metadata for a singe wasm code |
| Codes | [QueryCodesRequest](#cosmwasm.wasm.v1beta1.QueryCodesRequest) | [QueryCodesResponse](#cosmwasm.wasm.v1beta1.QueryCodesResponse) | Codes gets the metadata for all stored wasm codes |

 



<a name="x/wasm/types/tx.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## x/wasm/types/tx.proto



<a name="cosmwasm.wasm.v1beta1.MsgClearAdmin"></a>

### MsgClearAdmin
MsgClearAdmin removes any admin stored for a smart contract


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sender | [string](#string) |  | Sender is the that actor that signed the messages |
| contract | [string](#string) |  | Contract is the address of the smart contract |






<a name="cosmwasm.wasm.v1beta1.MsgClearAdminResponse"></a>

### MsgClearAdminResponse
MsgClearAdminResponse returns empty data






<a name="cosmwasm.wasm.v1beta1.MsgExecuteContract"></a>

### MsgExecuteContract
MsgExecuteContract submits the given message data to a smart contract


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sender | [string](#string) |  | Sender is the that actor that signed the messages |
| contract | [string](#string) |  | Contract is the address of the smart contract |
| msg | [bytes](#bytes) |  | Msg json encoded message to be passed to the contract |
| funds | [cosmos.base.v1beta1.Coin](#cosmos.base.v1beta1.Coin) | repeated | Funds coins that are transferred to the contract on execution |






<a name="cosmwasm.wasm.v1beta1.MsgExecuteContractResponse"></a>

### MsgExecuteContractResponse
MsgExecuteContractResponse returns execution result data.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| data | [bytes](#bytes) |  | Data contains base64-encoded bytes to returned from the contract |






<a name="cosmwasm.wasm.v1beta1.MsgInstantiateContract"></a>

### MsgInstantiateContract
MsgInstantiateContract create a new smart contract instance for the given code id.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sender | [string](#string) |  | Sender is the that actor that signed the messages |
| admin | [string](#string) |  | Admin is an optional address that can execute migrations |
| code_id | [uint64](#uint64) |  | CodeID is the reference to the stored WASM code |
| label | [string](#string) |  | Label is optional metadata to be stored with a contract instance. |
| init_msg | [bytes](#bytes) |  | InitMsg json encoded message to be passed to the contract on instantiation |
| funds | [cosmos.base.v1beta1.Coin](#cosmos.base.v1beta1.Coin) | repeated | Funds coins that are transferred to the contract on instantiation |






<a name="cosmwasm.wasm.v1beta1.MsgInstantiateContractResponse"></a>

### MsgInstantiateContractResponse
MsgInstantiateContractResponse return instantiation result data


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| address | [string](#string) |  | Address is the bech32 address of the new contract instance. |
| data | [bytes](#bytes) |  | Data contains base64-encoded bytes to returned from the contract |






<a name="cosmwasm.wasm.v1beta1.MsgMigrateContract"></a>

### MsgMigrateContract
MsgMigrateContract runs a code upgrade/ downgrade for a smart contract


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sender | [string](#string) |  | Sender is the that actor that signed the messages |
| contract | [string](#string) |  | Contract is the address of the smart contract |
| code_id | [uint64](#uint64) |  | CodeID references the new WASM code |
| migrate_msg | [bytes](#bytes) |  | MigrateMsg json encoded message to be passed to the contract on migration |






<a name="cosmwasm.wasm.v1beta1.MsgMigrateContractResponse"></a>

### MsgMigrateContractResponse
MsgMigrateContractResponse returns contract migration result data.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| data | [bytes](#bytes) |  | Data contains same raw bytes returned as data from the wasm contract. (May be empty) |






<a name="cosmwasm.wasm.v1beta1.MsgStoreCode"></a>

### MsgStoreCode
MsgStoreCode submit Wasm code to the system


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sender | [string](#string) |  | Sender is the that actor that signed the messages |
| wasm_byte_code | [bytes](#bytes) |  | WASMByteCode can be raw or gzip compressed |
| source | [string](#string) |  | Source is a valid absolute HTTPS URI to the contract&#39;s source code, optional |
| builder | [string](#string) |  | Builder is a valid docker image name with tag, optional |
| instantiate_permission | [AccessConfig](#cosmwasm.wasm.v1beta1.AccessConfig) |  | InstantiatePermission access control to apply on contract creation, optional |






<a name="cosmwasm.wasm.v1beta1.MsgStoreCodeResponse"></a>

### MsgStoreCodeResponse
MsgStoreCodeResponse returns store result data.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code_id | [uint64](#uint64) |  | CodeID is the reference to the stored WASM code |






<a name="cosmwasm.wasm.v1beta1.MsgUpdateAdmin"></a>

### MsgUpdateAdmin
MsgUpdateAdmin sets a new admin for a smart contract


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sender | [string](#string) |  | Sender is the that actor that signed the messages |
| new_admin | [string](#string) |  | NewAdmin address to be set |
| contract | [string](#string) |  | Contract is the address of the smart contract |






<a name="cosmwasm.wasm.v1beta1.MsgUpdateAdminResponse"></a>

### MsgUpdateAdminResponse
MsgUpdateAdminResponse returns empty data





 

 

 


<a name="cosmwasm.wasm.v1beta1.Msg"></a>

### Msg
Msg defines the wasm Msg service.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| StoreCode | [MsgStoreCode](#cosmwasm.wasm.v1beta1.MsgStoreCode) | [MsgStoreCodeResponse](#cosmwasm.wasm.v1beta1.MsgStoreCodeResponse) | StoreCode to submit Wasm code to the system |
| InstantiateContract | [MsgInstantiateContract](#cosmwasm.wasm.v1beta1.MsgInstantiateContract) | [MsgInstantiateContractResponse](#cosmwasm.wasm.v1beta1.MsgInstantiateContractResponse) | Instantiate creates a new smart contract instance for the given code id. |
| ExecuteContract | [MsgExecuteContract](#cosmwasm.wasm.v1beta1.MsgExecuteContract) | [MsgExecuteContractResponse](#cosmwasm.wasm.v1beta1.MsgExecuteContractResponse) | Execute submits the given message data to a smart contract |
| MigrateContract | [MsgMigrateContract](#cosmwasm.wasm.v1beta1.MsgMigrateContract) | [MsgMigrateContractResponse](#cosmwasm.wasm.v1beta1.MsgMigrateContractResponse) | Migrate runs a code upgrade/ downgrade for a smart contract |
| UpdateAdmin | [MsgUpdateAdmin](#cosmwasm.wasm.v1beta1.MsgUpdateAdmin) | [MsgUpdateAdminResponse](#cosmwasm.wasm.v1beta1.MsgUpdateAdminResponse) | UpdateAdmin sets a new admin for a smart contract |
| ClearAdmin | [MsgClearAdmin](#cosmwasm.wasm.v1beta1.MsgClearAdmin) | [MsgClearAdminResponse](#cosmwasm.wasm.v1beta1.MsgClearAdminResponse) | ClearAdmin removes any admin stored for a smart contract |

 



<a name="x/wasm/types/types.proto"></a>
<p align="right"><a href="#top">Top</a></p>

## x/wasm/types/types.proto



<a name="cosmwasm.wasm.v1beta1.AbsoluteTxPosition"></a>

### AbsoluteTxPosition
AbsoluteTxPosition is a unique transaction position that allows for global ordering of transactions.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| block_height | [uint64](#uint64) |  | BlockHeight is the block the contract was created at |
| tx_index | [uint64](#uint64) |  | TxIndex is a monotonic counter within the block (actual transaction index, or gas consumed) |






<a name="cosmwasm.wasm.v1beta1.AccessConfig"></a>

### AccessConfig
AccessConfig access control type.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| permission | [AccessType](#cosmwasm.wasm.v1beta1.AccessType) |  |  |
| address | [string](#string) |  |  |






<a name="cosmwasm.wasm.v1beta1.AccessTypeParam"></a>

### AccessTypeParam
AccessTypeParam


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| value | [AccessType](#cosmwasm.wasm.v1beta1.AccessType) |  |  |






<a name="cosmwasm.wasm.v1beta1.CodeInfo"></a>

### CodeInfo
CodeInfo is data for the uploaded contract WASM code


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code_hash | [bytes](#bytes) |  | CodeHash is the unique identifier created by wasmvm |
| creator | [string](#string) |  | Creator address who initially stored the code |
| source | [string](#string) |  | Source is a valid absolute HTTPS URI to the contract&#39;s source code, optional |
| builder | [string](#string) |  | Builder is a valid docker image name with tag, optional |
| instantiate_config | [AccessConfig](#cosmwasm.wasm.v1beta1.AccessConfig) |  | InstantiateConfig access control to apply on contract creation, optional |






<a name="cosmwasm.wasm.v1beta1.ContractCodeHistoryEntry"></a>

### ContractCodeHistoryEntry
ContractCodeHistoryEntry metadata to a contract.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| operation | [ContractCodeHistoryOperationType](#cosmwasm.wasm.v1beta1.ContractCodeHistoryOperationType) |  |  |
| code_id | [uint64](#uint64) |  | CodeID is the reference to the stored WASM code |
| updated | [AbsoluteTxPosition](#cosmwasm.wasm.v1beta1.AbsoluteTxPosition) |  | Updated Tx position when the operation was executed. |
| msg | [bytes](#bytes) |  |  |






<a name="cosmwasm.wasm.v1beta1.ContractInfo"></a>

### ContractInfo
ContractInfo stores a WASM contract instance


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code_id | [uint64](#uint64) |  | CodeID is the reference to the stored Wasm code |
| creator | [string](#string) |  | Creator address who initially instantiated the contract |
| admin | [string](#string) |  | Admin is an optional address that can execute migrations |
| label | [string](#string) |  | Label is optional metadata to be stored with a contract instance. |
| created | [AbsoluteTxPosition](#cosmwasm.wasm.v1beta1.AbsoluteTxPosition) |  | Created Tx position when the contract was instantiated. This data should kept internal and not be exposed via query results. Just use for sorting |
| ibc_port_id | [string](#string) |  |  |






<a name="cosmwasm.wasm.v1beta1.Model"></a>

### Model
Model is a struct that holds a KV pair


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [bytes](#bytes) |  | hex-encode key to read it better (this is often ascii) |
| value | [bytes](#bytes) |  | base64-encode raw value |






<a name="cosmwasm.wasm.v1beta1.Params"></a>

### Params
Params defines the set of wasm parameters.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code_upload_access | [AccessConfig](#cosmwasm.wasm.v1beta1.AccessConfig) |  |  |
| instantiate_default_permission | [AccessType](#cosmwasm.wasm.v1beta1.AccessType) |  |  |
| max_wasm_code_size | [uint64](#uint64) |  |  |





 


<a name="cosmwasm.wasm.v1beta1.AccessType"></a>

### AccessType
AccessType permission types

| Name | Number | Description |
| ---- | ------ | ----------- |
| ACCESS_TYPE_UNSPECIFIED | 0 | AccessTypeUnspecified placeholder for empty value |
| ACCESS_TYPE_NOBODY | 1 | AccessTypeNobody forbidden |
| ACCESS_TYPE_ONLY_ADDRESS | 2 | AccessTypeOnlyAddress restricted to an address |
| ACCESS_TYPE_EVERYBODY | 3 | AccessTypeEverybody unrestricted |



<a name="cosmwasm.wasm.v1beta1.ContractCodeHistoryOperationType"></a>

### ContractCodeHistoryOperationType
ContractCodeHistoryOperationType actions that caused a code change

| Name | Number | Description |
| ---- | ------ | ----------- |
| CONTRACT_CODE_HISTORY_OPERATION_TYPE_UNSPECIFIED | 0 | ContractCodeHistoryOperationTypeUnspecified placeholder for empty value |
| CONTRACT_CODE_HISTORY_OPERATION_TYPE_INIT | 1 | ContractCodeHistoryOperationTypeInit on chain contract instantiation |
| CONTRACT_CODE_HISTORY_OPERATION_TYPE_MIGRATE | 2 | ContractCodeHistoryOperationTypeMigrate code migration |
| CONTRACT_CODE_HISTORY_OPERATION_TYPE_GENESIS | 3 | ContractCodeHistoryOperationTypeGenesis based on genesis data |


 

 

 



## Scalar Value Types

| .proto Type | Notes | C++ | Java | Python | Go | C# | PHP | Ruby |
| ----------- | ----- | --- | ---- | ------ | -- | -- | --- | ---- |
| <a name="double" /> double |  | double | double | float | float64 | double | float | Float |
| <a name="float" /> float |  | float | float | float | float32 | float | float | Float |
| <a name="int32" /> int32 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint32 instead. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="int64" /> int64 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint64 instead. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="uint32" /> uint32 | Uses variable-length encoding. | uint32 | int | int/long | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="uint64" /> uint64 | Uses variable-length encoding. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum or Fixnum (as required) |
| <a name="sint32" /> sint32 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int32s. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sint64" /> sint64 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int64s. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="fixed32" /> fixed32 | Always four bytes. More efficient than uint32 if values are often greater than 2^28. | uint32 | int | int | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="fixed64" /> fixed64 | Always eight bytes. More efficient than uint64 if values are often greater than 2^56. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum |
| <a name="sfixed32" /> sfixed32 | Always four bytes. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sfixed64" /> sfixed64 | Always eight bytes. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="bool" /> bool |  | bool | boolean | boolean | bool | bool | boolean | TrueClass/FalseClass |
| <a name="string" /> string | A string must always contain UTF-8 encoded or 7-bit ASCII text. | string | String | str/unicode | string | string | string | String (UTF-8) |
| <a name="bytes" /> bytes | May contain any arbitrary sequence of bytes. | string | ByteString | str | []byte | ByteString | string | String (ASCII-8BIT) |

