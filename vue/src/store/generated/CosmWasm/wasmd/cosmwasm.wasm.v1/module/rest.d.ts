/**
* `Any` contains an arbitrary serialized protocol buffer message along with a
URL that describes the type of the serialized message.

Protobuf library provides support to pack/unpack Any values in the form
of utility functions or additional generated methods of the Any type.

Example 1: Pack and unpack a message in C++.

    Foo foo = ...;
    Any any;
    any.PackFrom(foo);
    ...
    if (any.UnpackTo(&foo)) {
      ...
    }

Example 2: Pack and unpack a message in Java.

    Foo foo = ...;
    Any any = Any.pack(foo);
    ...
    if (any.is(Foo.class)) {
      foo = any.unpack(Foo.class);
    }

 Example 3: Pack and unpack a message in Python.

    foo = Foo(...)
    any = Any()
    any.Pack(foo)
    ...
    if any.Is(Foo.DESCRIPTOR):
      any.Unpack(foo)
      ...

 Example 4: Pack and unpack a message in Go

     foo := &pb.Foo{...}
     any, err := anypb.New(foo)
     if err != nil {
       ...
     }
     ...
     foo := &pb.Foo{}
     if err := any.UnmarshalTo(foo); err != nil {
       ...
     }

The pack methods provided by protobuf library will by default use
'type.googleapis.com/full.type.name' as the type URL and the unpack
methods only use the fully qualified type name after the last '/'
in the type URL, for example "foo.bar.com/x/y.z" will yield type
name "y.z".


JSON
====
The JSON representation of an `Any` value uses the regular
representation of the deserialized, embedded message, with an
additional field `@type` which contains the type URL. Example:

    package google.profile;
    message Person {
      string first_name = 1;
      string last_name = 2;
    }

    {
      "@type": "type.googleapis.com/google.profile.Person",
      "firstName": <string>,
      "lastName": <string>
    }

If the embedded message type is well-known and has a custom JSON
representation, that representation will be embedded adding a field
`value` which holds the custom JSON in addition to the `@type`
field. Example (for message [google.protobuf.Duration][]):

    {
      "@type": "type.googleapis.com/google.protobuf.Duration",
      "value": "1.212s"
    }
*/
export interface ProtobufAny {
    /**
     * A URL/resource name that uniquely identifies the type of the serialized
     * protocol buffer message. This string must contain at least
     * one "/" character. The last segment of the URL's path must represent
     * the fully qualified name of the type (as in
     * `path/google.protobuf.Duration`). The name should be in a canonical form
     * (e.g., leading "." is not accepted).
     *
     * In practice, teams usually precompile into the binary all types that they
     * expect it to use in the context of Any. However, for URLs which use the
     * scheme `http`, `https`, or no scheme, one can optionally set up a type
     * server that maps type URLs to message definitions as follows:
     *
     * * If no scheme is provided, `https` is assumed.
     * * An HTTP GET on the URL must yield a [google.protobuf.Type][]
     *   value in binary format, or produce an error.
     * * Applications are allowed to cache lookup results based on the
     *   URL, or have them precompiled into a binary to avoid any
     *   lookup. Therefore, binary compatibility needs to be preserved
     *   on changes to types. (Use versioned type names to manage
     *   breaking changes.)
     *
     * Note: this functionality is not currently available in the official
     * protobuf release, and it is not used for type URLs beginning with
     * type.googleapis.com.
     *
     * Schemes other than `http`, `https` (or the empty scheme) might be
     * used with implementation specific semantics.
     */
    "@type"?: string;
}
export interface RpcStatus {
    /** @format int32 */
    code?: number;
    message?: string;
    details?: ProtobufAny[];
}
/**
* AbsoluteTxPosition is a unique transaction position that allows for global
ordering of transactions.
*/
export interface V1AbsoluteTxPosition {
    /** @format uint64 */
    blockHeight?: string;
    /** @format uint64 */
    txIndex?: string;
}
/**
 * AccessConfig access control type.
 */
export interface V1AccessConfig {
    /**
     * - ACCESS_TYPE_UNSPECIFIED: AccessTypeUnspecified placeholder for empty value
     *  - ACCESS_TYPE_NOBODY: AccessTypeNobody forbidden
     *  - ACCESS_TYPE_ONLY_ADDRESS: AccessTypeOnlyAddress restricted to an address
     *  - ACCESS_TYPE_EVERYBODY: AccessTypeEverybody unrestricted
     */
    permission?: V1AccessType;
    address?: string;
}
/**
* - ACCESS_TYPE_UNSPECIFIED: AccessTypeUnspecified placeholder for empty value
 - ACCESS_TYPE_NOBODY: AccessTypeNobody forbidden
 - ACCESS_TYPE_ONLY_ADDRESS: AccessTypeOnlyAddress restricted to an address
 - ACCESS_TYPE_EVERYBODY: AccessTypeEverybody unrestricted
*/
export declare enum V1AccessType {
    ACCESS_TYPE_UNSPECIFIED = "ACCESS_TYPE_UNSPECIFIED",
    ACCESS_TYPE_NOBODY = "ACCESS_TYPE_NOBODY",
    ACCESS_TYPE_ONLY_ADDRESS = "ACCESS_TYPE_ONLY_ADDRESS",
    ACCESS_TYPE_EVERYBODY = "ACCESS_TYPE_EVERYBODY"
}
export interface V1CodeInfoResponse {
    /** @format uint64 */
    codeId?: string;
    creator?: string;
    /** @format byte */
    dataHash?: string;
    /** AccessConfig access control type. */
    instantiatePermission?: V1AccessConfig;
}
/**
 * ContractCodeHistoryEntry metadata to a contract.
 */
export interface V1ContractCodeHistoryEntry {
    /**
     * - CONTRACT_CODE_HISTORY_OPERATION_TYPE_UNSPECIFIED: ContractCodeHistoryOperationTypeUnspecified placeholder for empty value
     *  - CONTRACT_CODE_HISTORY_OPERATION_TYPE_INIT: ContractCodeHistoryOperationTypeInit on chain contract instantiation
     *  - CONTRACT_CODE_HISTORY_OPERATION_TYPE_MIGRATE: ContractCodeHistoryOperationTypeMigrate code migration
     *  - CONTRACT_CODE_HISTORY_OPERATION_TYPE_GENESIS: ContractCodeHistoryOperationTypeGenesis based on genesis data
     */
    operation?: V1ContractCodeHistoryOperationType;
    /** @format uint64 */
    codeId?: string;
    /** Updated Tx position when the operation was executed. */
    updated?: V1AbsoluteTxPosition;
    /** @format byte */
    msg?: string;
}
/**
* - CONTRACT_CODE_HISTORY_OPERATION_TYPE_UNSPECIFIED: ContractCodeHistoryOperationTypeUnspecified placeholder for empty value
 - CONTRACT_CODE_HISTORY_OPERATION_TYPE_INIT: ContractCodeHistoryOperationTypeInit on chain contract instantiation
 - CONTRACT_CODE_HISTORY_OPERATION_TYPE_MIGRATE: ContractCodeHistoryOperationTypeMigrate code migration
 - CONTRACT_CODE_HISTORY_OPERATION_TYPE_GENESIS: ContractCodeHistoryOperationTypeGenesis based on genesis data
*/
export declare enum V1ContractCodeHistoryOperationType {
    CONTRACT_CODE_HISTORY_OPERATION_TYPE_UNSPECIFIED = "CONTRACT_CODE_HISTORY_OPERATION_TYPE_UNSPECIFIED",
    CONTRACT_CODE_HISTORY_OPERATION_TYPE_INIT = "CONTRACT_CODE_HISTORY_OPERATION_TYPE_INIT",
    CONTRACT_CODE_HISTORY_OPERATION_TYPE_MIGRATE = "CONTRACT_CODE_HISTORY_OPERATION_TYPE_MIGRATE",
    CONTRACT_CODE_HISTORY_OPERATION_TYPE_GENESIS = "CONTRACT_CODE_HISTORY_OPERATION_TYPE_GENESIS"
}
export interface V1ContractInfo {
    /** @format uint64 */
    codeId?: string;
    creator?: string;
    admin?: string;
    /** Label is optional metadata to be stored with a contract instance. */
    label?: string;
    /**
     * AbsoluteTxPosition is a unique transaction position that allows for global
     * ordering of transactions.
     */
    created?: V1AbsoluteTxPosition;
    ibcPortId?: string;
    /**
     * Extension is an extension point to store custom metadata within the
     * persistence model.
     */
    extension?: ProtobufAny;
}
export interface V1Model {
    /** @format byte */
    key?: string;
    /** @format byte */
    value?: string;
}
export declare type V1MsgClearAdminResponse = object;
/**
 * MsgExecuteContractResponse returns execution result data.
 */
export interface V1MsgExecuteContractResponse {
    /** @format byte */
    data?: string;
}
export interface V1MsgInstantiateContractResponse {
    /** Address is the bech32 address of the new contract instance. */
    address?: string;
    /** @format byte */
    data?: string;
}
/**
 * MsgMigrateContractResponse returns contract migration result data.
 */
export interface V1MsgMigrateContractResponse {
    /** @format byte */
    data?: string;
}
/**
 * MsgStoreCodeResponse returns store result data.
 */
export interface V1MsgStoreCodeResponse {
    /** @format uint64 */
    codeId?: string;
}
export declare type V1MsgUpdateAdminResponse = object;
export interface V1QueryAllContractStateResponse {
    models?: V1Model[];
    /** pagination defines the pagination in the response. */
    pagination?: V1Beta1PageResponse;
}
export interface V1QueryCodeResponse {
    codeInfo?: V1CodeInfoResponse;
    /** @format byte */
    data?: string;
}
export interface V1QueryCodesResponse {
    codeInfos?: V1CodeInfoResponse[];
    /** pagination defines the pagination in the response. */
    pagination?: V1Beta1PageResponse;
}
export interface V1QueryContractHistoryResponse {
    entries?: V1ContractCodeHistoryEntry[];
    /** pagination defines the pagination in the response. */
    pagination?: V1Beta1PageResponse;
}
export interface V1QueryContractInfoResponse {
    address?: string;
    contractInfo?: V1ContractInfo;
}
export interface V1QueryContractsByCodeResponse {
    contracts?: string[];
    /** pagination defines the pagination in the response. */
    pagination?: V1Beta1PageResponse;
}
export interface V1QueryPinnedCodesResponse {
    codeIds?: string[];
    /** pagination defines the pagination in the response. */
    pagination?: V1Beta1PageResponse;
}
export interface V1QueryRawContractStateResponse {
    /** @format byte */
    data?: string;
}
export interface V1QuerySmartContractStateResponse {
    /** @format byte */
    data?: string;
}
/**
* Coin defines a token with a denomination and an amount.

NOTE: The amount field is an Int which implements the custom method
signatures required by gogoproto.
*/
export interface V1Beta1Coin {
    denom?: string;
    amount?: string;
}
/**
* message SomeRequest {
         Foo some_parameter = 1;
         PageRequest pagination = 2;
 }
*/
export interface V1Beta1PageRequest {
    /**
     * key is a value returned in PageResponse.next_key to begin
     * querying the next page most efficiently. Only one of offset or key
     * should be set.
     * @format byte
     */
    key?: string;
    /**
     * offset is a numeric offset that can be used when key is unavailable.
     * It is less efficient than using key. Only one of offset or key should
     * be set.
     * @format uint64
     */
    offset?: string;
    /**
     * limit is the total number of results to be returned in the result page.
     * If left empty it will default to a value to be set by each app.
     * @format uint64
     */
    limit?: string;
    /**
     * count_total is set to true  to indicate that the result set should include
     * a count of the total number of items available for pagination in UIs.
     * count_total is only respected when offset is used. It is ignored when key
     * is set.
     */
    countTotal?: boolean;
    /**
     * reverse is set to true if results are to be returned in the descending order.
     *
     * Since: cosmos-sdk 0.43
     */
    reverse?: boolean;
}
/**
* PageResponse is to be embedded in gRPC response messages where the
corresponding request message has used PageRequest.

 message SomeResponse {
         repeated Bar results = 1;
         PageResponse page = 2;
 }
*/
export interface V1Beta1PageResponse {
    /** @format byte */
    nextKey?: string;
    /** @format uint64 */
    total?: string;
}
export declare type QueryParamsType = Record<string | number, any>;
export declare type ResponseFormat = keyof Omit<Body, "body" | "bodyUsed">;
export interface FullRequestParams extends Omit<RequestInit, "body"> {
    /** set parameter to `true` for call `securityWorker` for this request */
    secure?: boolean;
    /** request path */
    path: string;
    /** content type of request body */
    type?: ContentType;
    /** query params */
    query?: QueryParamsType;
    /** format of response (i.e. response.json() -> format: "json") */
    format?: keyof Omit<Body, "body" | "bodyUsed">;
    /** request body */
    body?: unknown;
    /** base url */
    baseUrl?: string;
    /** request cancellation token */
    cancelToken?: CancelToken;
}
export declare type RequestParams = Omit<FullRequestParams, "body" | "method" | "query" | "path">;
export interface ApiConfig<SecurityDataType = unknown> {
    baseUrl?: string;
    baseApiParams?: Omit<RequestParams, "baseUrl" | "cancelToken" | "signal">;
    securityWorker?: (securityData: SecurityDataType) => RequestParams | void;
}
export interface HttpResponse<D extends unknown, E extends unknown = unknown> extends Response {
    data: D;
    error: E;
}
declare type CancelToken = Symbol | string | number;
export declare enum ContentType {
    Json = "application/json",
    FormData = "multipart/form-data",
    UrlEncoded = "application/x-www-form-urlencoded"
}
export declare class HttpClient<SecurityDataType = unknown> {
    baseUrl: string;
    private securityData;
    private securityWorker;
    private abortControllers;
    private baseApiParams;
    constructor(apiConfig?: ApiConfig<SecurityDataType>);
    setSecurityData: (data: SecurityDataType) => void;
    private addQueryParam;
    protected toQueryString(rawQuery?: QueryParamsType): string;
    protected addQueryParams(rawQuery?: QueryParamsType): string;
    private contentFormatters;
    private mergeRequestParams;
    private createAbortSignal;
    abortRequest: (cancelToken: CancelToken) => void;
    request: <T = any, E = any>({ body, secure, path, type, query, format, baseUrl, cancelToken, ...params }: FullRequestParams) => Promise<HttpResponse<T, E>>;
}
/**
 * @title cosmwasm/wasm/v1/genesis.proto
 * @version version not set
 */
export declare class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
    /**
     * No description
     *
     * @tags Query
     * @name QueryCodes
     * @summary Codes gets the metadata for all stored wasm codes
     * @request GET:/cosmwasm/wasm/v1/code
     */
    queryCodes: (query?: {
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<V1QueryCodesResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryCode
     * @summary Code gets the binary code and metadata for a singe wasm code
     * @request GET:/cosmwasm/wasm/v1/code/{codeId}
     */
    queryCode: (codeId: string, params?: RequestParams) => Promise<HttpResponse<V1QueryCodeResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryContractsByCode
     * @summary ContractsByCode lists all smart contracts for a code id
     * @request GET:/cosmwasm/wasm/v1/code/{codeId}/contracts
     */
    queryContractsByCode: (codeId: string, query?: {
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<V1QueryContractsByCodeResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryPinnedCodes
     * @summary PinnedCodes gets the pinned code ids
     * @request GET:/cosmwasm/wasm/v1/codes/pinned
     */
    queryPinnedCodes: (query?: {
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<V1QueryPinnedCodesResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryContractInfo
     * @summary ContractInfo gets the contract meta data
     * @request GET:/cosmwasm/wasm/v1/contract/{address}
     */
    queryContractInfo: (address: string, params?: RequestParams) => Promise<HttpResponse<V1QueryContractInfoResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryContractHistory
     * @summary ContractHistory gets the contract code history
     * @request GET:/cosmwasm/wasm/v1/contract/{address}/history
     */
    queryContractHistory: (address: string, query?: {
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<V1QueryContractHistoryResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryRawContractState
     * @summary RawContractState gets single key from the raw store data of a contract
     * @request GET:/cosmwasm/wasm/v1/contract/{address}/raw/{queryData}
     */
    queryRawContractState: (address: string, queryData: string, params?: RequestParams) => Promise<HttpResponse<V1QueryRawContractStateResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QuerySmartContractState
     * @summary SmartContractState get smart query result from the contract
     * @request GET:/cosmwasm/wasm/v1/contract/{address}/smart/{queryData}
     */
    querySmartContractState: (address: string, queryData: string, params?: RequestParams) => Promise<HttpResponse<V1QuerySmartContractStateResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryAllContractState
     * @summary AllContractState gets all raw store data for a single contract
     * @request GET:/cosmwasm/wasm/v1/contract/{address}/state
     */
    queryAllContractState: (address: string, query?: {
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<V1QueryAllContractStateResponse, RpcStatus>>;
}
export {};
