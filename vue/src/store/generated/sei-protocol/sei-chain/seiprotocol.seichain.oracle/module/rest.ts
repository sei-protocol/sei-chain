/* eslint-disable */
/* tslint:disable */
/*
 * ---------------------------------------------------------------
 * ## THIS FILE WAS GENERATED VIA SWAGGER-TYPESCRIPT-API        ##
 * ##                                                           ##
 * ## AUTHOR: acacode                                           ##
 * ## SOURCE: https://github.com/acacode/swagger-typescript-api ##
 * ---------------------------------------------------------------
 */

export interface OracleAggregateExchangeRatePrevote {
  hash?: string;
  voter?: string;

  /** @format uint64 */
  submit_block?: string;
}

export interface OracleAggregateExchangeRateVote {
  exchange_rate_tuples?: OracleExchangeRateTuple[];
  voter?: string;
}

export interface OracleDenom {
  name?: string;
}

export interface OracleDenomOracleExchangeRatePair {
  denom?: string;
  oracle_exchange_rate?: OracleOracleExchangeRate;
}

export interface OracleExchangeRateTuple {
  denom?: string;
  exchange_rate?: string;
}

/**
 * MsgAggregateExchangeRateVoteResponse defines the Msg/AggregateExchangeRateVote response type.
 */
export type OracleMsgAggregateExchangeRateCombinedVoteResponse = object;

/**
 * MsgAggregateExchangeRatePrevoteResponse defines the Msg/AggregateExchangeRatePrevote response type.
 */
export type OracleMsgAggregateExchangeRatePrevoteResponse = object;

/**
 * MsgAggregateExchangeRateVoteResponse defines the Msg/AggregateExchangeRateVote response type.
 */
export type OracleMsgAggregateExchangeRateVoteResponse = object;

/**
 * MsgDelegateFeedConsentResponse defines the Msg/DelegateFeedConsent response type.
 */
export type OracleMsgDelegateFeedConsentResponse = object;

export interface OracleOracleExchangeRate {
  exchange_rate?: string;
  last_update?: string;
}

export interface OracleOracleTwap {
  denom?: string;
  twap?: string;

  /** @format int64 */
  lookback_seconds?: string;
}

export interface OracleParams {
  /** @format uint64 */
  vote_period?: string;
  vote_threshold?: string;
  reward_band?: string;
  whitelist?: OracleDenom[];
  slash_fraction?: string;

  /** @format uint64 */
  slash_window?: string;
  min_valid_per_window?: string;

  /** @format int64 */
  lookback_duration?: string;
}

export interface OraclePriceSnapshot {
  /** @format int64 */
  snapshotTimestamp?: string;
  price_snapshot_items?: OraclePriceSnapshotItem[];
}

export interface OraclePriceSnapshotItem {
  denom?: string;
  oracle_exchange_rate?: OracleOracleExchangeRate;
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
  aggregate_prevote?: OracleAggregateExchangeRatePrevote;
}

/**
* QueryAggregatePrevotesResponse is response type for the
Query/AggregatePrevotes RPC method.
*/
export interface OracleQueryAggregatePrevotesResponse {
  aggregate_prevotes?: OracleAggregateExchangeRatePrevote[];
}

/**
* QueryAggregateVoteResponse is response type for the
Query/AggregateVote RPC method.
*/
export interface OracleQueryAggregateVoteResponse {
  aggregate_vote?: OracleAggregateExchangeRateVote;
}

/**
* QueryAggregateVotesResponse is response type for the
Query/AggregateVotes RPC method.
*/
export interface OracleQueryAggregateVotesResponse {
  aggregate_votes?: OracleAggregateExchangeRateVote[];
}

/**
* QueryExchangeRateResponse is response type for the
Query/ExchangeRate RPC method.
*/
export interface OracleQueryExchangeRateResponse {
  oracle_exchange_rate?: OracleOracleExchangeRate;
}

/**
* QueryExchangeRatesResponse is response type for the
Query/ExchangeRates RPC method.
*/
export interface OracleQueryExchangeRatesResponse {
  /** exchange_rates defines a list of the exchange rate for all whitelisted denoms. */
  denom_oracle_exchange_rate_pairs?: OracleDenomOracleExchangeRatePair[];
}

/**
* QueryFeederDelegationResponse is response type for the
Query/FeederDelegation RPC method.
*/
export interface OracleQueryFeederDelegationResponse {
  feeder_addr?: string;
}

/**
 * QueryParamsResponse is the response type for the Query/Params RPC method.
 */
export interface OracleQueryParamsResponse {
  /** params defines the parameters of the module. */
  params?: OracleParams;
}

export interface OracleQueryPriceSnapshotHistoryResponse {
  price_snapshots?: OraclePriceSnapshot[];
}

export interface OracleQueryTwapsResponse {
  oracle_twaps?: OracleOracleTwap[];
}

/**
* QueryVotePenaltyCounterResponse is response type for the
Query/VotePenaltyCounter RPC method.
*/
export interface OracleQueryVotePenaltyCounterResponse {
  vote_penalty_counter?: OracleVotePenaltyCounter;
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
  vote_targets?: string[];
}

export interface OracleVotePenaltyCounter {
  /** @format uint64 */
  miss_count?: string;

  /** @format uint64 */
  abstain_count?: string;
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

export type QueryParamsType = Record<string | number, any>;
export type ResponseFormat = keyof Omit<Body, "body" | "bodyUsed">;

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

export type RequestParams = Omit<FullRequestParams, "body" | "method" | "query" | "path">;

export interface ApiConfig<SecurityDataType = unknown> {
  baseUrl?: string;
  baseApiParams?: Omit<RequestParams, "baseUrl" | "cancelToken" | "signal">;
  securityWorker?: (securityData: SecurityDataType) => RequestParams | void;
}

export interface HttpResponse<D extends unknown, E extends unknown = unknown> extends Response {
  data: D;
  error: E;
}

type CancelToken = Symbol | string | number;

export enum ContentType {
  Json = "application/json",
  FormData = "multipart/form-data",
  UrlEncoded = "application/x-www-form-urlencoded",
}

export class HttpClient<SecurityDataType = unknown> {
  public baseUrl: string = "";
  private securityData: SecurityDataType = null as any;
  private securityWorker: null | ApiConfig<SecurityDataType>["securityWorker"] = null;
  private abortControllers = new Map<CancelToken, AbortController>();

  private baseApiParams: RequestParams = {
    credentials: "same-origin",
    headers: {},
    redirect: "follow",
    referrerPolicy: "no-referrer",
  };

  constructor(apiConfig: ApiConfig<SecurityDataType> = {}) {
    Object.assign(this, apiConfig);
  }

  public setSecurityData = (data: SecurityDataType) => {
    this.securityData = data;
  };

  private addQueryParam(query: QueryParamsType, key: string) {
    const value = query[key];

    return (
      encodeURIComponent(key) +
      "=" +
      encodeURIComponent(Array.isArray(value) ? value.join(",") : typeof value === "number" ? value : `${value}`)
    );
  }

  protected toQueryString(rawQuery?: QueryParamsType): string {
    const query = rawQuery || {};
    const keys = Object.keys(query).filter((key) => "undefined" !== typeof query[key]);
    return keys
      .map((key) =>
        typeof query[key] === "object" && !Array.isArray(query[key])
          ? this.toQueryString(query[key] as QueryParamsType)
          : this.addQueryParam(query, key),
      )
      .join("&");
  }

  protected addQueryParams(rawQuery?: QueryParamsType): string {
    const queryString = this.toQueryString(rawQuery);
    return queryString ? `?${queryString}` : "";
  }

  private contentFormatters: Record<ContentType, (input: any) => any> = {
    [ContentType.Json]: (input: any) =>
      input !== null && (typeof input === "object" || typeof input === "string") ? JSON.stringify(input) : input,
    [ContentType.FormData]: (input: any) =>
      Object.keys(input || {}).reduce((data, key) => {
        data.append(key, input[key]);
        return data;
      }, new FormData()),
    [ContentType.UrlEncoded]: (input: any) => this.toQueryString(input),
  };

  private mergeRequestParams(params1: RequestParams, params2?: RequestParams): RequestParams {
    return {
      ...this.baseApiParams,
      ...params1,
      ...(params2 || {}),
      headers: {
        ...(this.baseApiParams.headers || {}),
        ...(params1.headers || {}),
        ...((params2 && params2.headers) || {}),
      },
    };
  }

  private createAbortSignal = (cancelToken: CancelToken): AbortSignal | undefined => {
    if (this.abortControllers.has(cancelToken)) {
      const abortController = this.abortControllers.get(cancelToken);
      if (abortController) {
        return abortController.signal;
      }
      return void 0;
    }

    const abortController = new AbortController();
    this.abortControllers.set(cancelToken, abortController);
    return abortController.signal;
  };

  public abortRequest = (cancelToken: CancelToken) => {
    const abortController = this.abortControllers.get(cancelToken);

    if (abortController) {
      abortController.abort();
      this.abortControllers.delete(cancelToken);
    }
  };

  public request = <T = any, E = any>({
    body,
    secure,
    path,
    type,
    query,
    format = "json",
    baseUrl,
    cancelToken,
    ...params
  }: FullRequestParams): Promise<HttpResponse<T, E>> => {
    const secureParams = (secure && this.securityWorker && this.securityWorker(this.securityData)) || {};
    const requestParams = this.mergeRequestParams(params, secureParams);
    const queryString = query && this.toQueryString(query);
    const payloadFormatter = this.contentFormatters[type || ContentType.Json];

    return fetch(`${baseUrl || this.baseUrl || ""}${path}${queryString ? `?${queryString}` : ""}`, {
      ...requestParams,
      headers: {
        ...(type && type !== ContentType.FormData ? { "Content-Type": type } : {}),
        ...(requestParams.headers || {}),
      },
      signal: cancelToken ? this.createAbortSignal(cancelToken) : void 0,
      body: typeof body === "undefined" || body === null ? null : payloadFormatter(body),
    }).then(async (response) => {
      const r = response as HttpResponse<T, E>;
      r.data = (null as unknown) as T;
      r.error = (null as unknown) as E;

      const data = await response[format]()
        .then((data) => {
          if (r.ok) {
            r.data = data;
          } else {
            r.error = data;
          }
          return r;
        })
        .catch((e) => {
          r.error = e;
          return r;
        });

      if (cancelToken) {
        this.abortControllers.delete(cancelToken);
      }

      if (!response.ok) throw data;
      return data;
    });
  };
}

/**
 * @title oracle/genesis.proto
 * @version version not set
 */
export class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
  /**
   * No description
   *
   * @tags Query
   * @name QueryActives
   * @summary Actives returns all active denoms
   * @request GET:/sei-protocol/sei-chain/oracle/denoms/actives
   */
  queryActives = (params: RequestParams = {}) =>
    this.request<OracleQueryActivesResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/denoms/actives`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryExchangeRates
   * @summary ExchangeRates returns exchange rates of all denoms
   * @request GET:/sei-protocol/sei-chain/oracle/denoms/exchange_rates
   */
  queryExchangeRates = (params: RequestParams = {}) =>
    this.request<OracleQueryExchangeRatesResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/denoms/exchange_rates`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryPriceSnapshotHistory
   * @summary PriceSnapshotHistory returns the history of price snapshots for all assets
   * @request GET:/sei-protocol/sei-chain/oracle/denoms/price_snapshot_history
   */
  queryPriceSnapshotHistory = (params: RequestParams = {}) =>
    this.request<OracleQueryPriceSnapshotHistoryResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/denoms/price_snapshot_history`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryTwaps
   * @request GET:/sei-protocol/sei-chain/oracle/denoms/twaps
   */
  queryTwaps = (query?: { lookback_seconds?: string }, params: RequestParams = {}) =>
    this.request<OracleQueryTwapsResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/denoms/twaps`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryVoteTargets
   * @summary VoteTargets returns all vote target denoms
   * @request GET:/sei-protocol/sei-chain/oracle/denoms/vote_targets
   */
  queryVoteTargets = (params: RequestParams = {}) =>
    this.request<OracleQueryVoteTargetsResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/denoms/vote_targets`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryExchangeRate
   * @summary ExchangeRate returns exchange rate of a denom
   * @request GET:/sei-protocol/sei-chain/oracle/denoms/{denom}/exchange_rate
   */
  queryExchangeRate = (denom: string, params: RequestParams = {}) =>
    this.request<OracleQueryExchangeRateResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/denoms/${denom}/exchange_rate`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryParams
   * @summary Params queries all parameters.
   * @request GET:/sei-protocol/sei-chain/oracle/params
   */
  queryParams = (params: RequestParams = {}) =>
    this.request<OracleQueryParamsResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/params`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryAggregatePrevotes
   * @summary AggregatePrevotes returns aggregate prevotes of all validators
   * @request GET:/sei-protocol/sei-chain/oracle/validators/aggregate_prevotes
   */
  queryAggregatePrevotes = (params: RequestParams = {}) =>
    this.request<OracleQueryAggregatePrevotesResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/validators/aggregate_prevotes`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryAggregateVotes
   * @summary AggregateVotes returns aggregate votes of all validators
   * @request GET:/sei-protocol/sei-chain/oracle/validators/aggregate_votes
   */
  queryAggregateVotes = (params: RequestParams = {}) =>
    this.request<OracleQueryAggregateVotesResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/validators/aggregate_votes`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryAggregatePrevote
   * @summary AggregatePrevote returns an aggregate prevote of a validator
   * @request GET:/sei-protocol/sei-chain/oracle/validators/{validator_addr}/aggregate_prevote
   */
  queryAggregatePrevote = (validator_addr: string, params: RequestParams = {}) =>
    this.request<OracleQueryAggregatePrevoteResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/validators/${validator_addr}/aggregate_prevote`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryAggregateVote
   * @summary AggregateVote returns an aggregate vote of a validator
   * @request GET:/sei-protocol/sei-chain/oracle/validators/{validator_addr}/aggregate_vote
   */
  queryAggregateVote = (validator_addr: string, params: RequestParams = {}) =>
    this.request<OracleQueryAggregateVoteResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/validators/${validator_addr}/aggregate_vote`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryFeederDelegation
   * @summary FeederDelegation returns feeder delegation of a validator
   * @request GET:/sei-protocol/sei-chain/oracle/validators/{validator_addr}/feeder
   */
  queryFeederDelegation = (validator_addr: string, params: RequestParams = {}) =>
    this.request<OracleQueryFeederDelegationResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/validators/${validator_addr}/feeder`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryVotePenaltyCounter
   * @summary MissCounter returns oracle miss counter of a validator
   * @request GET:/sei-protocol/sei-chain/oracle/validators/{validator_addr}/vote_penalty_counter
   */
  queryVotePenaltyCounter = (validator_addr: string, params: RequestParams = {}) =>
    this.request<OracleQueryVotePenaltyCounterResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/validators/${validator_addr}/vote_penalty_counter`,
      method: "GET",
      format: "json",
      ...params,
    });
}
