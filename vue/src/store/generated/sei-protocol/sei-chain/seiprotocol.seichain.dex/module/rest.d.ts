export interface DexAllocation {
    /** @format uint64 */
    orderId?: string;
    quantity?: string;
    account?: string;
}
export interface DexAssetIBCInfo {
    sourceChannel?: string;
    dstChannel?: string;
    sourceDenom?: string;
    sourceChainID?: string;
}
export interface DexAssetMetadata {
    ibcInfo?: DexAssetIBCInfo;
    typeAsset?: string;
    /**
     * Metadata represents a struct that describes
     * a basic token.
     */
    metadata?: V1Beta1Metadata;
}
export interface DexCancellation {
    /** @format uint64 */
    id?: string;
    initiator?: DexCancellationInitiator;
    creator?: string;
    contractAddr?: string;
    priceDenom?: string;
    assetDenom?: string;
    positionDirection?: DexPositionDirection;
    price?: string;
}
export declare enum DexCancellationInitiator {
    USER = "USER",
    LIQUIDATED = "LIQUIDATED"
}
export interface DexContractDependencyInfo {
    dependency?: string;
    immediateElderSibling?: string;
    immediateYoungerSibling?: string;
}
export interface DexContractInfo {
    /** @format uint64 */
    codeId?: string;
    contractAddr?: string;
    needHook?: boolean;
    needOrderMatching?: boolean;
    dependencies?: DexContractDependencyInfo[];
    /** @format int64 */
    numIncomingDependencies?: string;
}
export interface DexLongBook {
    price?: string;
    entry?: DexOrderEntry;
}
export interface DexMatchResult {
    /** @format int64 */
    height?: string;
    contractAddr?: string;
    orders?: DexOrder[];
    settlements?: DexSettlementEntry[];
    cancellations?: DexCancellation[];
}
export declare type DexMsgCancelOrdersResponse = object;
export interface DexMsgPlaceOrdersResponse {
    orderIds?: string[];
}
export declare type DexMsgRegisterContractResponse = object;
export interface DexOrder {
    /** @format uint64 */
    id?: string;
    status?: DexOrderStatus;
    account?: string;
    contractAddr?: string;
    price?: string;
    quantity?: string;
    priceDenom?: string;
    assetDenom?: string;
    orderType?: DexOrderType;
    positionDirection?: DexPositionDirection;
    data?: string;
    statusDescription?: string;
}
export interface DexOrderEntry {
    price?: string;
    quantity?: string;
    allocations?: DexAllocation[];
    priceDenom?: string;
    assetDenom?: string;
}
export declare enum DexOrderStatus {
    PLACED = "PLACED",
    FAILED_TO_PLACE = "FAILED_TO_PLACE",
    CANCELLED = "CANCELLED",
    FULFILLED = "FULFILLED"
}
export declare enum DexOrderType {
    LIMIT = "LIMIT",
    MARKET = "MARKET",
    LIQUIDATION = "LIQUIDATION",
    FOKMARKET = "FOKMARKET"
}
export interface DexPair {
    priceDenom?: string;
    assetDenom?: string;
    ticksize?: string;
}
export declare enum DexPositionDirection {
    LONG = "LONG",
    SHORT = "SHORT"
}
export interface DexPrice {
    /** @format uint64 */
    snapshotTimestampInSeconds?: string;
    price?: string;
    pair?: DexPair;
}
export interface DexPriceCandlestick {
    /** @format uint64 */
    beginTimestamp?: string;
    /** @format uint64 */
    endTimestamp?: string;
    open?: string;
    high?: string;
    low?: string;
    close?: string;
    volume?: string;
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
export interface DexQueryAssetListResponse {
    assetList?: DexAssetMetadata[];
}
export interface DexQueryAssetMetadataResponse {
    metadata?: DexAssetMetadata;
}
export interface DexQueryGetHistoricalPricesResponse {
    prices?: DexPriceCandlestick[];
}
export interface DexQueryGetLongBookResponse {
    LongBook?: DexLongBook;
}
export interface DexQueryGetMarketSummaryResponse {
    totalVolume?: string;
    totalVolumeNotional?: string;
    highPrice?: string;
    lowPrice?: string;
    lastPrice?: string;
}
export interface DexQueryGetMatchResultResponse {
    result?: DexMatchResult;
}
export interface DexQueryGetOrderByIDResponse {
    order?: DexOrder;
}
export interface DexQueryGetOrdersResponse {
    orders?: DexOrder[];
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
export interface DexQueryOrderSimulationResponse {
    ExecutedQuantity?: string;
}
/**
 * QueryParamsResponse is response type for the Query/Params RPC method.
 */
export interface DexQueryParamsResponse {
    /** params holds all the parameters of this module. */
    params?: SeichaindexParams;
}
export interface DexQueryRegisteredPairsResponse {
    pairs?: DexPair[];
}
export interface DexSettlementEntry {
    account?: string;
    priceDenom?: string;
    assetDenom?: string;
    quantity?: string;
    executionCostOrProceed?: string;
    expectedCostOrProceed?: string;
    positionDirection?: string;
    orderType?: string;
    /** @format uint64 */
    orderId?: string;
    /** @format uint64 */
    timestamp?: string;
    /** @format uint64 */
    height?: string;
    /** @format uint64 */
    settlementId?: string;
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
 * Params defines the parameters for the module.
 */
export interface SeichaindexParams {
    /** @format uint64 */
    priceSnapshotRetention?: string;
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
 * @title dex/asset_list.proto
 * @version version not set
 */
export declare class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
    /**
     * No description
     *
     * @tags Query
     * @name QueryAssetList
     * @summary Returns metadata for all the assets
     * @request GET:/sei-protocol/seichain/dex/asset_list
     */
    queryAssetList: (params?: RequestParams) => Promise<HttpResponse<DexQueryAssetListResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryAssetMetadata
     * @summary Returns the metadata for a specified denom / display type
     * @request GET:/sei-protocol/seichain/dex/asset_list/{denom}
     */
    queryAssetMetadata: (denom: string, params?: RequestParams) => Promise<HttpResponse<DexQueryAssetMetadataResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryGetHistoricalPrices
     * @request GET:/sei-protocol/seichain/dex/get_historical_prices/{contractAddr}/{priceDenom}/{assetDenom}/{periodLengthInSeconds}/{numOfPeriods}
     */
    queryGetHistoricalPrices: (contractAddr: string, priceDenom: string, assetDenom: string, periodLengthInSeconds: string, numOfPeriods: string, params?: RequestParams) => Promise<HttpResponse<DexQueryGetHistoricalPricesResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryGetMarketSummary
     * @request GET:/sei-protocol/seichain/dex/get_market_summary/{contractAddr}/{priceDenom}/{assetDenom}/{lookbackInSeconds}
     */
    queryGetMarketSummary: (contractAddr: string, priceDenom: string, assetDenom: string, lookbackInSeconds: string, params?: RequestParams) => Promise<HttpResponse<DexQueryGetMarketSummaryResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryGetOrder
     * @request GET:/sei-protocol/seichain/dex/get_order_by_id/{contractAddr}/{priceDenom}/{assetDenom}/{id}
     */
    queryGetOrder: (contractAddr: string, priceDenom: string, assetDenom: string, id: string, params?: RequestParams) => Promise<HttpResponse<DexQueryGetOrderByIDResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryGetOrders
     * @request GET:/sei-protocol/seichain/dex/get_orders/{contractAddr}/{account}
     */
    queryGetOrders: (contractAddr: string, account: string, params?: RequestParams) => Promise<HttpResponse<DexQueryGetOrdersResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryGetPrices
     * @request GET:/sei-protocol/seichain/dex/get_prices/{contractAddr}/{priceDenom}/{assetDenom}
     */
    queryGetPrices: (contractAddr: string, priceDenom: string, assetDenom: string, params?: RequestParams) => Promise<HttpResponse<DexQueryGetPricesResponse, RpcStatus>>;
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
    queryLongBookAll: (contractAddr: string, priceDenom: string, assetDenom: string, query?: {
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
    queryLongBook: (contractAddr: string, priceDenom: string, assetDenom: string, price: string, params?: RequestParams) => Promise<HttpResponse<DexQueryGetLongBookResponse, RpcStatus>>;
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
     * @name QueryGetRegisteredPairs
     * @summary Returns all registered pairs for specified contract address
     * @request GET:/sei-protocol/seichain/dex/registered_pairs
     */
    queryGetRegisteredPairs: (query?: {
        contractAddr?: string;
    }, params?: RequestParams) => Promise<HttpResponse<DexQueryRegisteredPairsResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryShortBookAll
     * @summary Queries a list of ShortBook items.
     * @request GET:/sei-protocol/seichain/dex/short_book/{contractAddr}/{priceDenom}/{assetDenom}
     */
    queryShortBookAll: (contractAddr: string, priceDenom: string, assetDenom: string, query?: {
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
    queryShortBook: (contractAddr: string, priceDenom: string, assetDenom: string, price: string, params?: RequestParams) => Promise<HttpResponse<DexQueryGetShortBookResponse, RpcStatus>>;
}
export {};
