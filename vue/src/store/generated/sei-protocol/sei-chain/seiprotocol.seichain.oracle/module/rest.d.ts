export interface OracleAggregateExchangeRatePrevote {
    hash?: string;
    voter?: string;
    /** @format uint64 */
    submitBlock?: string;
}
export interface OracleAggregateExchangeRateVote {
    exchangeRateTuples?: OracleExchangeRateTuple[];
    voter?: string;
}
export interface OracleDenom {
    name?: string;
}
export interface OracleDenomOracleExchangeRatePair {
    denom?: string;
    oracleExchangeRate?: OracleOracleExchangeRate;
}
export interface OracleExchangeRateTuple {
    denom?: string;
    exchangeRate?: string;
}
/**
 * MsgAggregateExchangeRateVoteResponse defines the Msg/AggregateExchangeRateVote response type.
 */
export declare type OracleMsgAggregateExchangeRateCombinedVoteResponse = object;
/**
 * MsgAggregateExchangeRatePrevoteResponse defines the Msg/AggregateExchangeRatePrevote response type.
 */
export declare type OracleMsgAggregateExchangeRatePrevoteResponse = object;
/**
 * MsgAggregateExchangeRateVoteResponse defines the Msg/AggregateExchangeRateVote response type.
 */
export declare type OracleMsgAggregateExchangeRateVoteResponse = object;
/**
 * MsgDelegateFeedConsentResponse defines the Msg/DelegateFeedConsent response type.
 */
export declare type OracleMsgDelegateFeedConsentResponse = object;
export interface OracleOracleExchangeRate {
    exchangeRate?: string;
    lastUpdate?: string;
}
export interface OracleOracleTwap {
    denom?: string;
    twap?: string;
    /** @format int64 */
    lookbackSeconds?: string;
}
export interface OracleParams {
    /** @format uint64 */
    votePeriod?: string;
    voteThreshold?: string;
    rewardBand?: string;
    whitelist?: OracleDenom[];
    slashFraction?: string;
    /** @format uint64 */
    slashWindow?: string;
    minValidPerWindow?: string;
    /** @format int64 */
    lookbackDuration?: string;
}
export interface OraclePriceSnapshot {
    /** @format int64 */
    snapshotTimestamp?: string;
    priceSnapshotItems?: OraclePriceSnapshotItem[];
}
export interface OraclePriceSnapshotItem {
    denom?: string;
    oracleExchangeRate?: OracleOracleExchangeRate;
}
/**
* QueryActivesResponse is response type for the
Query/Actives RPC method.
*/
export interface OracleQueryActivesResponse {
    /** actives defines a list of the denomination which oracle prices aggreed upon. */
    actives?: string[];
}
/**
* QueryAggregatePrevoteResponse is response type for the
Query/AggregatePrevote RPC method.
*/
export interface OracleQueryAggregatePrevoteResponse {
    aggregatePrevote?: OracleAggregateExchangeRatePrevote;
}
/**
* QueryAggregatePrevotesResponse is response type for the
Query/AggregatePrevotes RPC method.
*/
export interface OracleQueryAggregatePrevotesResponse {
    aggregatePrevotes?: OracleAggregateExchangeRatePrevote[];
}
/**
* QueryAggregateVoteResponse is response type for the
Query/AggregateVote RPC method.
*/
export interface OracleQueryAggregateVoteResponse {
    aggregateVote?: OracleAggregateExchangeRateVote;
}
/**
* QueryAggregateVotesResponse is response type for the
Query/AggregateVotes RPC method.
*/
export interface OracleQueryAggregateVotesResponse {
    aggregateVotes?: OracleAggregateExchangeRateVote[];
}
/**
* QueryExchangeRateResponse is response type for the
Query/ExchangeRate RPC method.
*/
export interface OracleQueryExchangeRateResponse {
    oracleExchangeRate?: OracleOracleExchangeRate;
}
/**
* QueryExchangeRatesResponse is response type for the
Query/ExchangeRates RPC method.
*/
export interface OracleQueryExchangeRatesResponse {
    /** exchange_rates defines a list of the exchange rate for all whitelisted denoms. */
    denomOracleExchangeRatePairs?: OracleDenomOracleExchangeRatePair[];
}
/**
* QueryFeederDelegationResponse is response type for the
Query/FeederDelegation RPC method.
*/
export interface OracleQueryFeederDelegationResponse {
    feederAddr?: string;
}
/**
 * QueryParamsResponse is the response type for the Query/Params RPC method.
 */
export interface OracleQueryParamsResponse {
    /** params defines the parameters of the module. */
    params?: OracleParams;
}
export interface OracleQueryPriceSnapshotHistoryResponse {
    priceSnapshots?: OraclePriceSnapshot[];
}
export interface OracleQueryTwapsResponse {
    oracleTwaps?: OracleOracleTwap[];
}
/**
* QueryVotePenaltyCounterResponse is response type for the
Query/VotePenaltyCounter RPC method.
*/
export interface OracleQueryVotePenaltyCounterResponse {
    votePenaltyCounter?: OracleVotePenaltyCounter;
}
/**
* QueryVoteTargetsResponse is response type for the
Query/VoteTargets RPC method.
*/
export interface OracleQueryVoteTargetsResponse {
    /**
     * vote_targets defines a list of the denomination in which everyone
     * should vote in the current vote period.
     */
    voteTargets?: string[];
}
export interface OracleVotePenaltyCounter {
    /** @format uint64 */
    missCount?: string;
    /** @format uint64 */
    abstainCount?: string;
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
 * @title oracle/genesis.proto
 * @version version not set
 */
export declare class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
    /**
     * No description
     *
     * @tags Query
     * @name QueryActives
     * @summary Actives returns all active denoms
     * @request GET:/sei-protocol/sei-chain/oracle/denoms/actives
     */
    queryActives: (params?: RequestParams) => Promise<HttpResponse<OracleQueryActivesResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryExchangeRates
     * @summary ExchangeRates returns exchange rates of all denoms
     * @request GET:/sei-protocol/sei-chain/oracle/denoms/exchange_rates
     */
    queryExchangeRates: (params?: RequestParams) => Promise<HttpResponse<OracleQueryExchangeRatesResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryPriceSnapshotHistory
     * @summary PriceSnapshotHistory returns the history of price snapshots for all assets
     * @request GET:/sei-protocol/sei-chain/oracle/denoms/price_snapshot_history
     */
    queryPriceSnapshotHistory: (params?: RequestParams) => Promise<HttpResponse<OracleQueryPriceSnapshotHistoryResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryTwaps
     * @request GET:/sei-protocol/sei-chain/oracle/denoms/twaps
     */
    queryTwaps: (query?: {
        lookbackSeconds?: string;
    }, params?: RequestParams) => Promise<HttpResponse<OracleQueryTwapsResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryVoteTargets
     * @summary VoteTargets returns all vote target denoms
     * @request GET:/sei-protocol/sei-chain/oracle/denoms/vote_targets
     */
    queryVoteTargets: (params?: RequestParams) => Promise<HttpResponse<OracleQueryVoteTargetsResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryExchangeRate
     * @summary ExchangeRate returns exchange rate of a denom
     * @request GET:/sei-protocol/sei-chain/oracle/denoms/{denom}/exchange_rate
     */
    queryExchangeRate: (denom: string, params?: RequestParams) => Promise<HttpResponse<OracleQueryExchangeRateResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryParams
     * @summary Params queries all parameters.
     * @request GET:/sei-protocol/sei-chain/oracle/params
     */
    queryParams: (params?: RequestParams) => Promise<HttpResponse<OracleQueryParamsResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryAggregatePrevotes
     * @summary AggregatePrevotes returns aggregate prevotes of all validators
     * @request GET:/sei-protocol/sei-chain/oracle/validators/aggregate_prevotes
     */
    queryAggregatePrevotes: (params?: RequestParams) => Promise<HttpResponse<OracleQueryAggregatePrevotesResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryAggregateVotes
     * @summary AggregateVotes returns aggregate votes of all validators
     * @request GET:/sei-protocol/sei-chain/oracle/validators/aggregate_votes
     */
    queryAggregateVotes: (params?: RequestParams) => Promise<HttpResponse<OracleQueryAggregateVotesResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryAggregatePrevote
     * @summary AggregatePrevote returns an aggregate prevote of a validator
     * @request GET:/sei-protocol/sei-chain/oracle/validators/{validatorAddr}/aggregate_prevote
     */
    queryAggregatePrevote: (validatorAddr: string, params?: RequestParams) => Promise<HttpResponse<OracleQueryAggregatePrevoteResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryAggregateVote
     * @summary AggregateVote returns an aggregate vote of a validator
     * @request GET:/sei-protocol/sei-chain/oracle/validators/{validatorAddr}/aggregate_vote
     */
    queryAggregateVote: (validatorAddr: string, params?: RequestParams) => Promise<HttpResponse<OracleQueryAggregateVoteResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryFeederDelegation
     * @summary FeederDelegation returns feeder delegation of a validator
     * @request GET:/sei-protocol/sei-chain/oracle/validators/{validatorAddr}/feeder
     */
    queryFeederDelegation: (validatorAddr: string, params?: RequestParams) => Promise<HttpResponse<OracleQueryFeederDelegationResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryVotePenaltyCounter
     * @summary MissCounter returns oracle miss counter of a validator
     * @request GET:/sei-protocol/sei-chain/oracle/validators/{validatorAddr}/vote_penalty_counter
     */
    queryVotePenaltyCounter: (validatorAddr: string, params?: RequestParams) => Promise<HttpResponse<OracleQueryVotePenaltyCounterResponse, RpcStatus>>;
}
export {};
