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

export interface Oraclev1Params {
  /**
   * The number of blocks per voting window, at the end of the vote period, the oracle votes are assessed and exchange rates are calculated. If the vote period is 1 this is equivalent to having oracle votes assessed and exchange rates calculated in each block.
   * @format uint64
   */
  vote_period?: string;
  vote_threshold?: string;
  reward_band?: string;
  whitelist?: V1Denom[];
  slash_fraction?: string;

  /**
   * The interval in blocks at which the oracle module will assess validator penalty counters, and penalize validators with too poor performance.
   * @format uint64
   */
  slash_window?: string;

  /** The minimum percentage of voting windows for which a validator must have `success`es in order to not be penalized at the end of the slash window. */
  min_valid_per_window?: string;

  /** @format uint64 */
  lookback_duration?: string;
}

export interface Oraclev1VotePenaltyCounter {
  /** @format uint64 */
  miss_count?: string;

  /** @format uint64 */
  abstain_count?: string;

  /** @format uint64 */
  success_count?: string;
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

export interface V1Denom {
  name?: string;
}

export interface V1DenomOracleExchangeRatePair {
  denom?: string;
  oracle_exchange_rate?: V1OracleExchangeRate;
}

/**
 * MsgAggregateExchangeRateVoteResponse defines the Msg/AggregateExchangeRateVote response type.
 */
export type V1MsgAggregateExchangeRateVoteResponse = object;

/**
 * MsgDelegateFeedConsentResponse defines the Msg/DelegateFeedConsent response type.
 */
export type V1MsgDelegateFeedConsentResponse = object;

export interface V1OracleExchangeRate {
  exchange_rate?: string;
  last_update?: string;

  /** @format int64 */
  last_update_timestamp?: string;
}

export interface V1OracleTwap {
  denom?: string;
  twap?: string;

  /** @format int64 */
  lookback_seconds?: string;
}

export interface V1PriceSnapshot {
  /** @format int64 */
  snapshot_timestamp?: string;
  price_snapshot_items?: V1PriceSnapshotItem[];
}

export interface V1PriceSnapshotItem {
  denom?: string;
  oracle_exchange_rate?: V1OracleExchangeRate;
}

/**
* QueryActivesResponse is response type for the
Query/Actives RPC method.
*/
export interface V1QueryActivesResponse {
  /** actives defines a list of the denomination which oracle prices aggreed upon. */
  actives?: string[];
}

/**
* QueryExchangeRateResponse is response type for the
Query/ExchangeRate RPC method.
*/
export interface V1QueryExchangeRateResponse {
  oracle_exchange_rate?: V1OracleExchangeRate;
}

/**
* QueryExchangeRatesResponse is response type for the
Query/ExchangeRates RPC method.
*/
export interface V1QueryExchangeRatesResponse {
  /** exchange_rates defines a list of the exchange rate for all whitelisted denoms. */
  denom_oracle_exchange_rate_pairs?: V1DenomOracleExchangeRatePair[];
}

/**
* QueryFeederDelegationResponse is response type for the
Query/FeederDelegation RPC method.
*/
export interface V1QueryFeederDelegationResponse {
  feeder_addr?: string;
}

/**
 * QueryParamsResponse is the response type for the Query/Params RPC method.
 */
export interface V1QueryParamsResponse {
  /** params defines the parameters of the module. */
  params?: Oraclev1Params;
}

export interface V1QueryPriceSnapshotHistoryResponse {
  price_snapshots?: V1PriceSnapshot[];
}

/**
* QuerySlashWindowResponse is response type for the
Query/SlashWindow RPC method.
*/
export interface V1QuerySlashWindowResponse {
  /**
   * window_progress defines the number of voting periods
   * since the last slashing event would have taken place.
   * @format uint64
   */
  window_progress?: string;
}

export interface V1QueryTwapsResponse {
  oracle_twaps?: V1OracleTwap[];
}

/**
* QueryVotePenaltyCounterResponse is response type for the
Query/VotePenaltyCounter RPC method.
*/
export interface V1QueryVotePenaltyCounterResponse {
  vote_penalty_counter?: Oraclev1VotePenaltyCounter;
}

/**
* QueryVoteTargetsResponse is response type for the
Query/VoteTargets RPC method.
*/
export interface V1QueryVoteTargetsResponse {
  /**
   * vote_targets defines a list of the denomination in which everyone
   * should vote in the current vote period.
   */
  vote_targets?: string[];
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
 * @title oracle/v1/genesis.proto
 * @version version not set
 */
export class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
  /**
   * No description
   *
   * @tags Query
   * @name QueryParams
   * @summary Params queries all parameters.
   * @request GET:/sei-protocol/oracle/v1/params
   */
  queryParams = (params: RequestParams = {}) =>
    this.request<V1QueryParamsResponse, RpcStatus>({
      path: `/sei-protocol/oracle/v1/params`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryDeprecatedActives
   * @summary This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/denoms/actives` instead.
   * @request GET:/sei-protocol/sei-chain/oracle/denoms/actives
   */
  queryDeprecatedActives = (params: RequestParams = {}) =>
    this.request<V1QueryActivesResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/denoms/actives`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryDeprecatedExchangeRates
   * @summary This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/denoms/exchange_rates` instead.
   * @request GET:/sei-protocol/sei-chain/oracle/denoms/exchange_rates
   */
  queryDeprecatedExchangeRates = (params: RequestParams = {}) =>
    this.request<V1QueryExchangeRatesResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/denoms/exchange_rates`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryDeprecatedPriceSnapshotHistory
   * @summary This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/denoms/price_snapshot_history` instead.
   * @request GET:/sei-protocol/sei-chain/oracle/denoms/price_snapshot_history
   */
  queryDeprecatedPriceSnapshotHistory = (params: RequestParams = {}) =>
    this.request<V1QueryPriceSnapshotHistoryResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/denoms/price_snapshot_history`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryDeprecatedTwaps
   * @summary This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/denoms/twaps/{lookback_seconds}` instead.
   * @request GET:/sei-protocol/sei-chain/oracle/denoms/twaps/{lookback_seconds}
   */
  queryDeprecatedTwaps = (lookback_seconds: string, params: RequestParams = {}) =>
    this.request<V1QueryTwapsResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/denoms/twaps/${lookback_seconds}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryDeprecatedVoteTargets
   * @summary This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/denoms/vote_targets` instead.
   * @request GET:/sei-protocol/sei-chain/oracle/denoms/vote_targets
   */
  queryDeprecatedVoteTargets = (params: RequestParams = {}) =>
    this.request<V1QueryVoteTargetsResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/denoms/vote_targets`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryDeprecatedExchangeRate
   * @summary This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/denoms/{denom}/exchange_rate` instead.
   * @request GET:/sei-protocol/sei-chain/oracle/denoms/{denom}/exchange_rate
   */
  queryDeprecatedExchangeRate = (denom: string, params: RequestParams = {}) =>
    this.request<V1QueryExchangeRateResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/denoms/${denom}/exchange_rate`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryDeprecatedParams
   * @summary This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/params` instead.
   * @request GET:/sei-protocol/sei-chain/oracle/params
   */
  queryDeprecatedParams = (params: RequestParams = {}) =>
    this.request<V1QueryParamsResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/params`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryDeprecatedSlashWindow
   * @summary This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/slash_window` instead.
   * @request GET:/sei-protocol/sei-chain/oracle/slash_window
   */
  queryDeprecatedSlashWindow = (params: RequestParams = {}) =>
    this.request<V1QuerySlashWindowResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/slash_window`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryDeprecatedFeederDelegation
   * @summary This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/validators/{validator_addr}/feeder` instead.
   * @request GET:/sei-protocol/sei-chain/oracle/validators/{validator_addr}/feeder
   */
  queryDeprecatedFeederDelegation = (validator_addr: string, params: RequestParams = {}) =>
    this.request<V1QueryFeederDelegationResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/validators/${validator_addr}/feeder`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryDeprecatedVotePenaltyCounter
   * @summary This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/validators/{validator_addr}/vote_penalty_counter` instead.
   * @request GET:/sei-protocol/sei-chain/oracle/validators/{validator_addr}/vote_penalty_counter
   */
  queryDeprecatedVotePenaltyCounter = (validator_addr: string, params: RequestParams = {}) =>
    this.request<V1QueryVotePenaltyCounterResponse, RpcStatus>({
      path: `/sei-protocol/sei-chain/oracle/validators/${validator_addr}/vote_penalty_counter`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryActives
   * @summary Actives returns all active denoms
   * @request GET:/sei/oracle/v1/denoms/actives
   */
  queryActives = (params: RequestParams = {}) =>
    this.request<V1QueryActivesResponse, RpcStatus>({
      path: `/sei/oracle/v1/denoms/actives`,
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
   * @request GET:/sei/oracle/v1/denoms/exchange_rates
   */
  queryExchangeRates = (params: RequestParams = {}) =>
    this.request<V1QueryExchangeRatesResponse, RpcStatus>({
      path: `/sei/oracle/v1/denoms/exchange_rates`,
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
   * @request GET:/sei/oracle/v1/denoms/price_snapshot_history
   */
  queryPriceSnapshotHistory = (params: RequestParams = {}) =>
    this.request<V1QueryPriceSnapshotHistoryResponse, RpcStatus>({
      path: `/sei/oracle/v1/denoms/price_snapshot_history`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryTwaps
   * @request GET:/sei/oracle/v1/denoms/twaps/{lookback_seconds}
   */
  queryTwaps = (lookback_seconds: string, params: RequestParams = {}) =>
    this.request<V1QueryTwapsResponse, RpcStatus>({
      path: `/sei/oracle/v1/denoms/twaps/${lookback_seconds}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryVoteTargets
   * @summary VoteTargets returns all vote target denoms
   * @request GET:/sei/oracle/v1/denoms/vote_targets
   */
  queryVoteTargets = (params: RequestParams = {}) =>
    this.request<V1QueryVoteTargetsResponse, RpcStatus>({
      path: `/sei/oracle/v1/denoms/vote_targets`,
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
   * @request GET:/sei/oracle/v1/denoms/{denom}/exchange_rate
   */
  queryExchangeRate = (denom: string, params: RequestParams = {}) =>
    this.request<V1QueryExchangeRateResponse, RpcStatus>({
      path: `/sei/oracle/v1/denoms/${denom}/exchange_rate`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QuerySlashWindow
   * @summary SlashWindow returns slash window information
   * @request GET:/sei/oracle/v1/slash_window
   */
  querySlashWindow = (params: RequestParams = {}) =>
    this.request<V1QuerySlashWindowResponse, RpcStatus>({
      path: `/sei/oracle/v1/slash_window`,
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
   * @request GET:/sei/oracle/v1/validators/{validator_addr}/feeder
   */
  queryFeederDelegation = (validator_addr: string, params: RequestParams = {}) =>
    this.request<V1QueryFeederDelegationResponse, RpcStatus>({
      path: `/sei/oracle/v1/validators/${validator_addr}/feeder`,
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
   * @request GET:/sei/oracle/v1/validators/{validator_addr}/vote_penalty_counter
   */
  queryVotePenaltyCounter = (validator_addr: string, params: RequestParams = {}) =>
    this.request<V1QueryVotePenaltyCounterResponse, RpcStatus>({
      path: `/sei/oracle/v1/validators/${validator_addr}/vote_penalty_counter`,
      method: "GET",
      format: "json",
      ...params,
    });
}
