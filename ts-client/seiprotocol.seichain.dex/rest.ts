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

export interface DexBatchContractPair {
  contractAddr?: string;
  pairs?: DexPair[];
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

export enum DexCancellationInitiator {
  USER = "USER",
  LIQUIDATED = "LIQUIDATED",
}

export interface DexContractDependencyInfo {
  dependency?: string;
  immediateElderSibling?: string;
  immediateYoungerSibling?: string;
}

export interface DexContractInfoV2 {
  /** @format uint64 */
  codeId?: string;
  contractAddr?: string;
  needHook?: boolean;
  needOrderMatching?: boolean;
  dependencies?: DexContractDependencyInfo[];

  /** @format int64 */
  numIncomingDependencies?: string;
  creator?: string;

  /** @format uint64 */
  rentBalance?: string;
  suspended?: boolean;
  suspensionReason?: string;
}

export interface DexMatchResult {
  /** @format int64 */
  height?: string;
  contractAddr?: string;
  orders?: DexOrder[];
  settlements?: DexSettlementEntry[];
  cancellations?: DexCancellation[];
}

export type DexMsgCancelOrdersResponse = object;

export type DexMsgContractDepositRentResponse = object;

export interface DexMsgPlaceOrdersResponse {
  orderIds?: string[];
}

export type DexMsgRegisterContractResponse = object;

export type DexMsgRegisterPairsResponse = object;

export type DexMsgUnregisterContractResponse = object;

export type DexMsgUnsuspendContractResponse = object;

export type DexMsgUpdateTickSizeResponse = object;

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
  nominal?: string;
  triggerPrice?: string;
  triggerStatus?: boolean;
}

export interface DexOrderEntry {
  price?: string;
  quantity?: string;
  allocations?: DexAllocation[];
  priceDenom?: string;
  assetDenom?: string;
}

export enum DexOrderStatus {
  PLACED = "PLACED",
  FAILED_TO_PLACE = "FAILED_TO_PLACE",
  CANCELLED = "CANCELLED",
  FULFILLED = "FULFILLED",
}

export enum DexOrderType {
  LIMIT = "LIMIT",
  MARKET = "MARKET",
  FOKMARKET = "FOKMARKET",
  FOKMARKETBYVALUE = "FOKMARKETBYVALUE",
  STOPLOSS = "STOPLOSS",
  STOPLIMIT = "STOPLIMIT",
}

export interface DexPair {
  priceDenom?: string;
  assetDenom?: string;
  priceTicksize?: string;
  quantityTicksize?: string;
}

export enum DexPositionDirection {
  LONG = "LONG",
  SHORT = "SHORT",
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
  LongBook?: SeichaindexLongBook[];

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
  ShortBook?: SeichaindexShortBook[];

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
  assetList?: SeichaindexAssetMetadata[];
}

export interface DexQueryAssetMetadataResponse {
  metadata?: SeichaindexAssetMetadata;
}

export interface DexQueryGetHistoricalPricesResponse {
  prices?: DexPriceCandlestick[];
}

export interface DexQueryGetLatestPriceResponse {
  price?: DexPrice;
}

export interface DexQueryGetLongBookResponse {
  LongBook?: SeichaindexLongBook;
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

export interface DexQueryGetOrderCountResponse {
  /** @format uint64 */
  count?: string;
}

export interface DexQueryGetOrdersResponse {
  orders?: DexOrder[];
}

export interface DexQueryGetPriceResponse {
  price?: DexPrice;
  found?: boolean;
}

export interface DexQueryGetPricesResponse {
  prices?: DexPrice[];
}

export interface DexQueryGetShortBookResponse {
  ShortBook?: SeichaindexShortBook;
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

export interface DexQueryRegisteredContractResponse {
  contract_info?: DexContractInfoV2;
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

export interface DexTickSize {
  pair?: DexPair;
  ticksize?: string;
  contractAddr?: string;
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

export interface SeichaindexAssetMetadata {
  ibcInfo?: DexAssetIBCInfo;
  type_asset?: string;

  /**
   * Metadata represents a struct that describes
   * a basic token.
   */
  metadata?: V1Beta1Metadata;
}

export interface SeichaindexLongBook {
  price?: string;
  entry?: DexOrderEntry;
}

/**
 * Params defines the parameters for the module.
 */
export interface SeichaindexParams {
  /** @format uint64 */
  price_snapshot_retention?: string;
  sudo_call_gas_price?: string;

  /** @format uint64 */
  begin_block_gas_limit?: string;

  /** @format uint64 */
  end_block_gas_limit?: string;

  /** @format uint64 */
  default_gas_per_order?: string;

  /** @format uint64 */
  default_gas_per_cancel?: string;

  /** @format uint64 */
  min_rent_deposit?: string;

  /** @format uint64 */
  gas_allowance_per_settlement?: string;

  /** @format uint64 */
  min_processable_rent?: string;

  /** @format uint64 */
  order_book_entries_per_load?: string;

  /** @format uint64 */
  contract_unsuspend_cost?: string;

  /** @format uint64 */
  max_order_per_price?: string;

  /** @format uint64 */
  max_pairs_per_contract?: string;

  /** @format uint64 */
  default_gas_per_order_data_byte?: string;
}

export interface SeichaindexShortBook {
  price?: string;
  entry?: DexOrderEntry;
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
  denom_units?: V1Beta1DenomUnit[];

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
 * @title dex/asset_list.proto
 * @version version not set
 */
export class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
  /**
   * No description
   *
   * @tags Query
   * @name QueryAssetList
   * @summary Returns metadata for all the assets
   * @request GET:/sei-protocol/seichain/dex/asset_list
   */
  queryAssetList = (params: RequestParams = {}) =>
    this.request<DexQueryAssetListResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/asset_list`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryAssetMetadata
   * @summary Returns the metadata for a specified denom / display type
   * @request GET:/sei-protocol/seichain/dex/asset_list/{denom}
   */
  queryAssetMetadata = (denom: string, params: RequestParams = {}) =>
    this.request<DexQueryAssetMetadataResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/asset_list/${denom}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryGetHistoricalPrices
   * @request GET:/sei-protocol/seichain/dex/get_historical_prices/{contractAddr}/{priceDenom}/{assetDenom}/{periodLengthInSeconds}/{numOfPeriods}
   */
  queryGetHistoricalPrices = (
    contractAddr: string,
    priceDenom: string,
    assetDenom: string,
    periodLengthInSeconds: string,
    numOfPeriods: string,
    params: RequestParams = {},
  ) =>
    this.request<DexQueryGetHistoricalPricesResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/get_historical_prices/${contractAddr}/${priceDenom}/${assetDenom}/${periodLengthInSeconds}/${numOfPeriods}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryGetLatestPrice
   * @request GET:/sei-protocol/seichain/dex/get_latest_price/{contractAddr}/{priceDenom}/{assetDenom}
   */
  queryGetLatestPrice = (contractAddr: string, priceDenom: string, assetDenom: string, params: RequestParams = {}) =>
    this.request<DexQueryGetLatestPriceResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/get_latest_price/${contractAddr}/${priceDenom}/${assetDenom}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryGetMarketSummary
   * @request GET:/sei-protocol/seichain/dex/get_market_summary/{contractAddr}/{priceDenom}/{assetDenom}/{lookbackInSeconds}
   */
  queryGetMarketSummary = (
    contractAddr: string,
    priceDenom: string,
    assetDenom: string,
    lookbackInSeconds: string,
    params: RequestParams = {},
  ) =>
    this.request<DexQueryGetMarketSummaryResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/get_market_summary/${contractAddr}/${priceDenom}/${assetDenom}/${lookbackInSeconds}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryGetOrder
   * @request GET:/sei-protocol/seichain/dex/get_order_by_id/{contractAddr}/{priceDenom}/{assetDenom}/{id}
   */
  queryGetOrder = (
    contractAddr: string,
    priceDenom: string,
    assetDenom: string,
    id: string,
    params: RequestParams = {},
  ) =>
    this.request<DexQueryGetOrderByIDResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/get_order_by_id/${contractAddr}/${priceDenom}/${assetDenom}/${id}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryGetOrders
   * @request GET:/sei-protocol/seichain/dex/get_orders/{contractAddr}/{account}
   */
  queryGetOrders = (contractAddr: string, account: string, params: RequestParams = {}) =>
    this.request<DexQueryGetOrdersResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/get_orders/${contractAddr}/${account}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryGetPrice
   * @request GET:/sei-protocol/seichain/dex/get_price/{contractAddr}/{priceDenom}/{assetDenom}/{timestamp}
   */
  queryGetPrice = (
    contractAddr: string,
    priceDenom: string,
    assetDenom: string,
    timestamp: string,
    params: RequestParams = {},
  ) =>
    this.request<DexQueryGetPriceResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/get_price/${contractAddr}/${priceDenom}/${assetDenom}/${timestamp}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryGetPrices
   * @request GET:/sei-protocol/seichain/dex/get_prices/{contractAddr}/{priceDenom}/{assetDenom}
   */
  queryGetPrices = (contractAddr: string, priceDenom: string, assetDenom: string, params: RequestParams = {}) =>
    this.request<DexQueryGetPricesResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/get_prices/${contractAddr}/${priceDenom}/${assetDenom}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryGetTwaps
   * @request GET:/sei-protocol/seichain/dex/get_twaps/{contractAddr}/{lookbackSeconds}
   */
  queryGetTwaps = (contractAddr: string, lookbackSeconds: string, params: RequestParams = {}) =>
    this.request<DexQueryGetTwapsResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/get_twaps/${contractAddr}/${lookbackSeconds}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryLongBookAll
   * @summary Queries a list of LongBook items.
   * @request GET:/sei-protocol/seichain/dex/long_book/{contractAddr}/{priceDenom}/{assetDenom}
   */
  queryLongBookAll = (
    contractAddr: string,
    priceDenom: string,
    assetDenom: string,
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<DexQueryAllLongBookResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/long_book/${contractAddr}/${priceDenom}/${assetDenom}`,
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
   * @request GET:/sei-protocol/seichain/dex/long_book/{contractAddr}/{priceDenom}/{assetDenom}/{price}
   */
  queryLongBook = (
    contractAddr: string,
    priceDenom: string,
    assetDenom: string,
    price: string,
    params: RequestParams = {},
  ) =>
    this.request<DexQueryGetLongBookResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/long_book/${contractAddr}/${priceDenom}/${assetDenom}/${price}`,
      method: "GET",
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
    this.request<DexQueryParamsResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/params`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryGetRegisteredContract
   * @summary Returns registered contract information
   * @request GET:/sei-protocol/seichain/dex/registered_contract/{contractAddr}
   */
  queryGetRegisteredContract = (contractAddr: string, params: RequestParams = {}) =>
    this.request<DexQueryRegisteredContractResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/registered_contract/${contractAddr}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryGetRegisteredPairs
   * @summary Returns all registered pairs for specified contract address
   * @request GET:/sei-protocol/seichain/dex/registered_pairs
   */
  queryGetRegisteredPairs = (query?: { contractAddr?: string }, params: RequestParams = {}) =>
    this.request<DexQueryRegisteredPairsResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/registered_pairs`,
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
   * @request GET:/sei-protocol/seichain/dex/short_book/{contractAddr}/{priceDenom}/{assetDenom}
   */
  queryShortBookAll = (
    contractAddr: string,
    priceDenom: string,
    assetDenom: string,
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<DexQueryAllShortBookResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/short_book/${contractAddr}/${priceDenom}/${assetDenom}`,
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
   * @request GET:/sei-protocol/seichain/dex/short_book/{contractAddr}/{priceDenom}/{assetDenom}/{price}
   */
  queryShortBook = (
    contractAddr: string,
    priceDenom: string,
    assetDenom: string,
    price: string,
    params: RequestParams = {},
  ) =>
    this.request<DexQueryGetShortBookResponse, RpcStatus>({
      path: `/sei-protocol/seichain/dex/short_book/${contractAddr}/${priceDenom}/${assetDenom}/${price}`,
      method: "GET",
      format: "json",
      ...params,
    });
}
