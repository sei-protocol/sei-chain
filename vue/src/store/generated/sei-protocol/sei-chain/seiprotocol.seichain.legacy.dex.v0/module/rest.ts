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

export interface ProtobufAny {
  "@type"?: string;
}

export interface RpcStatus {
  /** @format int32 */
  code?: number;
  message?: string;
  details?: ProtobufAny[];
}

export interface V0ContractInfo {
  /** @format uint64 */
  codeId?: string;
  contractAddr?: string;
}

export interface V0LongBook {
  /** @format uint64 */
  id?: string;
  entry?: V0OrderEntry;
}

export type V0MsgCancelOrdersResponse = object;

export type V0MsgLiquidationResponse = object;

export interface V0MsgPlaceOrdersResponse {
  orderIds?: string[];
}

export type V0MsgRegisterContractResponse = object;

export type V0MsgRegisterPairResponse = object;

export interface V0OrderCancellation {
  long?: boolean;

  /** @format uint64 */
  price?: string;

  /** @format uint64 */
  quantity?: string;
  priceDenom?: string;
  assetDenom?: string;
  open?: boolean;
  leverage?: string;
}

export interface V0OrderEntry {
  /** @format uint64 */
  price?: string;

  /** @format uint64 */
  quantity?: string;
  allocationCreator?: string[];
  allocation?: string[];
  priceDenom?: string;
  assetDenom?: string;
}

export interface V0OrderPlacement {
  long?: boolean;

  /** @format uint64 */
  price?: string;

  /** @format uint64 */
  quantity?: string;
  priceDenom?: string;
  assetDenom?: string;
  open?: boolean;
  limit?: boolean;
  leverage?: string;
}

export interface V0Pair {
  priceDenom?: string;
  assetDenom?: string;
}

/**
 * Params defines the parameters for the module.
 */
export type V0Params = object;

export interface V0QueryAllLongBookResponse {
  LongBook?: V0LongBook[];

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

export interface V0QueryAllSettlementsResponse {
  Settlements?: V0Settlements[];

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

export interface V0QueryAllShortBookResponse {
  ShortBook?: V0ShortBook[];

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

export interface V0QueryGetLongBookResponse {
  LongBook?: V0LongBook;
}

export interface V0QueryGetShortBookResponse {
  ShortBook?: V0ShortBook;
}

export interface V0QueryGetTwapResponse {
  twaps?: V0Twap;
}

/**
 * QueryParamsResponse is response type for the Query/Params RPC method.
 */
export interface V0QueryParamsResponse {
  /** params holds all the parameters of this module. */
  params?: V0Params;
}

export interface V0SettlementEntry {
  account?: string;
  priceDenom?: string;
  assetDenom?: string;
  quantity?: string;
  executionCostOrProceed?: string;
  expectedCostOrProceed?: string;
  positionDirection?: string;
  positionEffect?: string;
  leverage?: string;
}

export interface V0Settlements {
  /** @format int64 */
  epoch?: string;
  entries?: V0SettlementEntry[];
}

export interface V0ShortBook {
  /** @format uint64 */
  id?: string;
  entry?: V0OrderEntry;
}

export interface V0Twap {
  /** @format uint64 */
  lastEpoch?: string;
  prices?: string[];

  /** @format uint64 */
  twapPrice?: string;
  priceDenom?: string;
  assetDenom?: string;
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
  count_total?: boolean;

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
  next_key?: string;

  /** @format uint64 */
  total?: string;
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
 * @title legacy/dex/v0/contract.proto
 * @version version not set
 */
export class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
  /**
   * No description
   *
   * @tags Query
   * @name QueryGetTwap
   * @summary Queries a list of GetTwap items.
   * @request GET:/sei-protocol/seichain/dex/get_twap/{priceDenom}/{assetDenom}
   */
  queryGetTwap = (
    priceDenom: string,
    assetDenom: string,
    query?: { contractAddr?: string },
    params: RequestParams = {},
  ) =>
    this.request<V0QueryGetTwapResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/get_twap/${priceDenom}/${assetDenom}`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryLongBookAll
   * @summary Queries a list of LongBook items.
   * @request GET:/sei-protocol/seichain/dex/long_book
   */
  queryLongBookAll = (
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V0QueryAllLongBookResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/long_book`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryLongBook
   * @summary Queries a LongBook by id.
   * @request GET:/sei-protocol/seichain/dex/long_book/{id}
   */
  queryLongBook = (
    id: string,
    query?: { contractAddr?: string; priceDenom?: string; assetDenom?: string },
    params: RequestParams = {},
  ) =>
    this.request<V0QueryGetLongBookResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/long_book/${id}`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryParams
   * @summary Parameters queries the parameters of the module.
   * @request GET:/sei-protocol/seichain/dex/params
   */
  queryParams = (params: RequestParams = {}) =>
    this.request<V0QueryParamsResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/params`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QuerySettlementsAll
   * @request GET:/sei-protocol/seichain/dex/settlement
   */
  querySettlementsAll = (
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V0QueryAllSettlementsResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/settlement`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryShortBookAll
   * @summary Queries a list of ShortBook items.
   * @request GET:/sei-protocol/seichain/dex/short_book
   */
  queryShortBookAll = (
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V0QueryAllShortBookResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/short_book`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryShortBook
   * @summary Queries a ShortBook by id.
   * @request GET:/sei-protocol/seichain/dex/short_book/{id}
   */
  queryShortBook = (
    id: string,
    query?: { contractAddr?: string; priceDenom?: string; assetDenom?: string },
    params: RequestParams = {},
  ) =>
    this.request<V0QueryGetShortBookResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/short_book/${id}`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });
}
