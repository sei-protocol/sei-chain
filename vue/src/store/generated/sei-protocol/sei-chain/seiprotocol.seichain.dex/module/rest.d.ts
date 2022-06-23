export interface DexContractInfo {
    /** @format uint64 */
    codeId?: string;
    contractAddr?: string;
}
export declare enum DexDenom {
    SEI = "SEI",
    ATOM = "ATOM",
    BTC = "BTC",
    ETH = "ETH",
    SOL = "SOL",
    AVAX = "AVAX",
    USDC = "USDC",
    NEAR = "NEAR",
    OSMO = "OSMO"
}
export interface DexLongBook {
    price?: string;
    entry?: DexOrderEntry;
}
export declare type DexMsgCancelOrdersResponse = object;
export declare type DexMsgLiquidationResponse = object;
export interface DexMsgPlaceOrdersResponse {
    orderIds?: string[];
}
export declare type DexMsgRegisterContractResponse = object;
export declare type DexMsgRegisterPairResponse = object;
export interface DexOrderCancellation {
    positionDirection?: DexPositionDirection;
    price?: string;
    quantity?: string;
    priceDenom?: DexDenom;
    assetDenom?: DexDenom;
    positionEffect?: DexPositionEffect;
    leverage?: string;
}
export interface DexOrderEntry {
    price?: string;
    quantity?: string;
    allocationCreator?: string[];
    allocation?: string[];
    priceDenom?: DexDenom;
    assetDenom?: DexDenom;
}
export interface DexOrderPlacement {
    positionDirection?: DexPositionDirection;
    price?: string;
    quantity?: string;
    priceDenom?: DexDenom;
    assetDenom?: DexDenom;
    positionEffect?: DexPositionEffect;
    orderType?: DexOrderType;
    leverage?: string;
}
export declare enum DexOrderType {
    LIMIT = "LIMIT",
    MARKET = "MARKET",
    LIQUIDATION = "LIQUIDATION"
}
export interface DexPair {
    priceDenom?: DexDenom;
    assetDenom?: DexDenom;
}
/**
 * Params defines the parameters for the module.
 */
export interface DexParams {
    /** @format uint64 */
    priceSnapshotRetention?: string;
}
export declare enum DexPositionDirection {
    LONG = "LONG",
    SHORT = "SHORT"
}
export declare enum DexPositionEffect {
    OPEN = "OPEN",
    CLOSE = "CLOSE"
}
export interface DexPrice {
    /** @format uint64 */
    snapshotTimestampInSeconds?: string;
    price?: string;
    pair?: DexPair;
}
export interface DexQueryAllLongBookResponse {
    LongBook?: DexLongBook[];
    /**
     * PageResponse is to be embedded in gRPC response messages where the
     * corresponding request message has used PageRequest.
     *
     *  message SomeResponse {
     *          repeated Bar results = 1;
     *          PageResponse page = 2;
     *  }
     */
    pagination?: V1Beta1PageResponse;
}
export interface DexQueryAllSettlementsResponse {
    Settlements?: DexSettlements[];
    /**
     * PageResponse is to be embedded in gRPC response messages where the
     * corresponding request message has used PageRequest.
     *
     *  message SomeResponse {
     *          repeated Bar results = 1;
     *          PageResponse page = 2;
     *  }
     */
    pagination?: V1Beta1PageResponse;
}
export interface DexQueryAllShortBookResponse {
    ShortBook?: DexShortBook[];
    /**
     * PageResponse is to be embedded in gRPC response messages where the
     * corresponding request message has used PageRequest.
     *
     *  message SomeResponse {
     *          repeated Bar results = 1;
     *          PageResponse page = 2;
     *  }
     */
    pagination?: V1Beta1PageResponse;
}
export interface DexQueryGetLongBookResponse {
    LongBook?: DexLongBook;
}
export interface DexQueryGetPricesResponse {
    prices?: DexPrice[];
}
export interface DexQueryGetShortBookResponse {
    ShortBook?: DexShortBook;
}
export interface DexQueryGetTwapsResponse {
    twaps?: DexTwap[];
}
/**
 * QueryParamsResponse is response type for the Query/Params RPC method.
 */
export interface DexQueryParamsResponse {
    /** params holds all the parameters of this module. */
    params?: DexParams;
}
export interface DexSettlementEntry {
    account?: string;
    priceDenom?: string;
    assetDenom?: string;
    quantity?: string;
    executionCostOrProceed?: string;
    expectedCostOrProceed?: string;
    positionDirection?: string;
    positionEffect?: string;
    leverage?: string;
    orderType?: string;
}
export interface DexSettlements {
    /** @format int64 */
    epoch?: string;
    entries?: DexSettlementEntry[];
}
export interface DexShortBook {
    price?: string;
    entry?: DexOrderEntry;
}
export interface DexTwap {
    pair?: DexPair;
    twap?: string;
    /** @format uint64 */
    lookbackSeconds?: string;
}
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
 * @title dex/contract.proto
 * @version version not set
 */
export declare class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
    /**
     * No description
     *
     * @tags Query
     * @name QueryGetPrices
     * @request GET:/sei-protocol/seichain/dex/get_prices/{contractAddr}/{priceDenom}/{assetDenom}
     */
    queryGetPrices: (contractAddr: string, priceDenom: "SEI" | "ATOM" | "BTC" | "ETH" | "SOL" | "AVAX" | "USDC" | "NEAR" | "OSMO", assetDenom: "SEI" | "ATOM" | "BTC" | "ETH" | "SOL" | "AVAX" | "USDC" | "NEAR" | "OSMO", params?: RequestParams) => Promise<HttpResponse<DexQueryGetPricesResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryGetTwaps
     * @request GET:/sei-protocol/seichain/dex/get_twaps/{contractAddr}/{lookbackSeconds}
     */
    queryGetTwaps: (contractAddr: string, lookbackSeconds: string, params?: RequestParams) => Promise<HttpResponse<DexQueryGetTwapsResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryLongBookAll
     * @summary Queries a list of LongBook items.
     * @request GET:/sei-protocol/seichain/dex/long_book/{contractAddr}/{priceDenom}/{assetDenom}
     */
    queryLongBookAll: (contractAddr: string, priceDenom: "SEI" | "ATOM" | "BTC" | "ETH" | "SOL" | "AVAX" | "USDC" | "NEAR" | "OSMO", assetDenom: "SEI" | "ATOM" | "BTC" | "ETH" | "SOL" | "AVAX" | "USDC" | "NEAR" | "OSMO", query?: {
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<DexQueryAllLongBookResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryLongBook
     * @summary Queries a LongBook by id.
     * @request GET:/sei-protocol/seichain/dex/long_book/{contractAddr}/{priceDenom}/{assetDenom}/{price}
     */
    queryLongBook: (contractAddr: string, priceDenom: "SEI" | "ATOM" | "BTC" | "ETH" | "SOL" | "AVAX" | "USDC" | "NEAR" | "OSMO", assetDenom: "SEI" | "ATOM" | "BTC" | "ETH" | "SOL" | "AVAX" | "USDC" | "NEAR" | "OSMO", price: string, params?: RequestParams) => Promise<HttpResponse<DexQueryGetLongBookResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryParams
     * @summary Parameters queries the parameters of the module.
     * @request GET:/sei-protocol/seichain/dex/params
     */
    queryParams: (params?: RequestParams) => Promise<HttpResponse<DexQueryParamsResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QuerySettlementsAll
     * @request GET:/sei-protocol/seichain/dex/settlement
     */
    querySettlementsAll: (query?: {
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<DexQueryAllSettlementsResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryShortBookAll
     * @summary Queries a list of ShortBook items.
     * @request GET:/sei-protocol/seichain/dex/short_book/{contractAddr}/{priceDenom}/{assetDenom}
     */
    queryShortBookAll: (contractAddr: string, priceDenom: "SEI" | "ATOM" | "BTC" | "ETH" | "SOL" | "AVAX" | "USDC" | "NEAR" | "OSMO", assetDenom: "SEI" | "ATOM" | "BTC" | "ETH" | "SOL" | "AVAX" | "USDC" | "NEAR" | "OSMO", query?: {
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<DexQueryAllShortBookResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryShortBook
     * @summary Queries a ShortBook by id.
     * @request GET:/sei-protocol/seichain/dex/short_book/{contractAddr}/{priceDenom}/{assetDenom}/{price}
     */
    queryShortBook: (contractAddr: string, priceDenom: "SEI" | "ATOM" | "BTC" | "ETH" | "SOL" | "AVAX" | "USDC" | "NEAR" | "OSMO", assetDenom: "SEI" | "ATOM" | "BTC" | "ETH" | "SOL" | "AVAX" | "USDC" | "NEAR" | "OSMO", price: string, params?: RequestParams) => Promise<HttpResponse<DexQueryGetShortBookResponse, RpcStatus>>;
}
export {};
