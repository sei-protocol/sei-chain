export interface ProtobufAny {
    "@type"?: string;
}
export interface RpcStatus {
    /** @format int32 */
    code?: number;
    message?: string;
    details?: ProtobufAny[];
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
* DenomUnit represents a struct that describes a given
denomination unit of the basic token.
*/
export interface V1Beta1DenomUnit {
    /** denom represents the string name of the given denom unit (e.g uatom). */
    denom?: string;
    /**
     * exponent represents power of 10 exponent that one must
     * raise the base_denom to in order to equal the given DenomUnit's denom
     * 1 denom = 1^exponent base_denom
     * (e.g. with a base_denom of uatom, one can create a DenomUnit of 'atom' with
     * exponent = 6, thus: 1 atom = 10^6 uatom).
     * @format int64
     */
    exponent?: number;
    aliases?: string[];
}
/**
 * Input models transaction input.
 */
export interface V1Beta1Input {
    address?: string;
    coins?: V1Beta1Coin[];
}
/**
* Metadata represents a struct that describes
a basic token.
*/
export interface V1Beta1Metadata {
    description?: string;
    denomUnits?: V1Beta1DenomUnit[];
    /** base represents the base denom (should be the DenomUnit with exponent = 0). */
    base?: string;
    /**
     * display indicates the suggested denom that should be
     * displayed in clients.
     */
    display?: string;
    /** Since: cosmos-sdk 0.43 */
    name?: string;
    /**
     * symbol is the token symbol usually shown on exchanges (eg: ATOM). This can
     * be the same as the display.
     *
     * Since: cosmos-sdk 0.43
     */
    symbol?: string;
}
/**
 * MsgMultiSendResponse defines the Msg/MultiSend response type.
 */
export declare type V1Beta1MsgMultiSendResponse = object;
/**
 * MsgSendResponse defines the Msg/Send response type.
 */
export declare type V1Beta1MsgSendResponse = object;
/**
 * Output models transaction outputs.
 */
export interface V1Beta1Output {
    address?: string;
    coins?: V1Beta1Coin[];
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
/**
 * Params defines the parameters for the bank module.
 */
export interface V1Beta1Params {
    sendEnabled?: V1Beta1SendEnabled[];
    defaultSendEnabled?: boolean;
}
/**
* QueryAllBalancesResponse is the response type for the Query/AllBalances RPC
method.
*/
export interface V1Beta1QueryAllBalancesResponse {
    /** balances is the balances of all the coins. */
    balances?: V1Beta1Coin[];
    /** pagination defines the pagination in the response. */
    pagination?: V1Beta1PageResponse;
}
/**
 * QueryBalanceResponse is the response type for the Query/Balance RPC method.
 */
export interface V1Beta1QueryBalanceResponse {
    /** balance is the balance of the coin. */
    balance?: V1Beta1Coin;
}
/**
* QueryDenomMetadataResponse is the response type for the Query/DenomMetadata RPC
method.
*/
export interface V1Beta1QueryDenomMetadataResponse {
    /** metadata describes and provides all the client information for the requested token. */
    metadata?: V1Beta1Metadata;
}
/**
* QueryDenomsMetadataResponse is the response type for the Query/DenomsMetadata RPC
method.
*/
export interface V1Beta1QueryDenomsMetadataResponse {
    /** metadata provides the client information for all the registered tokens. */
    metadatas?: V1Beta1Metadata[];
    /** pagination defines the pagination in the response. */
    pagination?: V1Beta1PageResponse;
}
/**
 * QueryParamsResponse defines the response type for querying x/bank parameters.
 */
export interface V1Beta1QueryParamsResponse {
    /** Params defines the parameters for the bank module. */
    params?: V1Beta1Params;
}
/**
* QuerySpendableBalancesResponse defines the gRPC response structure for querying
an account's spendable balances.
*/
export interface V1Beta1QuerySpendableBalancesResponse {
    /** balances is the spendable balances of all the coins. */
    balances?: V1Beta1Coin[];
    /** pagination defines the pagination in the response. */
    pagination?: V1Beta1PageResponse;
}
/**
 * QuerySupplyOfResponse is the response type for the Query/SupplyOf RPC method.
 */
export interface V1Beta1QuerySupplyOfResponse {
    /** amount is the supply of the coin. */
    amount?: V1Beta1Coin;
}
export interface V1Beta1QueryTotalSupplyResponse {
    supply?: V1Beta1Coin[];
    /**
     * pagination defines the pagination in the response.
     *
     * Since: cosmos-sdk 0.43
     */
    pagination?: V1Beta1PageResponse;
}
/**
* SendEnabled maps coin denom to a send_enabled status (whether a denom is
sendable).
*/
export interface V1Beta1SendEnabled {
    denom?: string;
    enabled?: boolean;
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
 * @title cosmos/bank/v1beta1/authz.proto
 * @version version not set
 */
export declare class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
    /**
     * No description
     *
     * @tags Query
     * @name QueryAllBalances
     * @summary AllBalances queries the balance of all coins for a single account.
     * @request GET:/cosmos/bank/v1beta1/balances/{address}
     */
    queryAllBalances: (address: string, query?: {
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryAllBalancesResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryBalance
     * @summary Balance queries the balance of a single coin for a single account.
     * @request GET:/cosmos/bank/v1beta1/balances/{address}/by_denom
     */
    queryBalance: (address: string, query?: {
        denom?: string;
    }, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryBalanceResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryDenomsMetadata
     * @summary DenomsMetadata queries the client metadata for all registered coin denominations.
     * @request GET:/cosmos/bank/v1beta1/denoms_metadata
     */
    queryDenomsMetadata: (query?: {
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryDenomsMetadataResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryDenomMetadata
     * @summary DenomsMetadata queries the client metadata of a given coin denomination.
     * @request GET:/cosmos/bank/v1beta1/denoms_metadata/{denom}
     */
    queryDenomMetadata: (denom: string, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryDenomMetadataResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryParams
     * @summary Params queries the parameters of x/bank module.
     * @request GET:/cosmos/bank/v1beta1/params
     */
    queryParams: (params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryParamsResponse, RpcStatus>>;
    /**
   * No description
   *
   * @tags Query
   * @name QuerySpendableBalances
   * @summary SpendableBalances queries the spenable balance of all coins for a single
  account.
   * @request GET:/cosmos/bank/v1beta1/spendable_balances/{address}
   */
    querySpendableBalances: (address: string, query?: {
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<V1Beta1QuerySpendableBalancesResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryTotalSupply
     * @summary TotalSupply queries the total supply of all coins.
     * @request GET:/cosmos/bank/v1beta1/supply
     */
    queryTotalSupply: (query?: {
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryTotalSupplyResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QuerySupplyOf
     * @summary SupplyOf queries the supply of a single coin.
     * @request GET:/cosmos/bank/v1beta1/supply/{denom}
     */
    querySupplyOf: (denom: string, params?: RequestParams) => Promise<HttpResponse<V1Beta1QuerySupplyOfResponse, RpcStatus>>;
}
export {};
