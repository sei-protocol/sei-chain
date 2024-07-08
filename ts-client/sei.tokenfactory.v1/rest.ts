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

/**
* DenomAuthorityMetadata specifies metadata for addresses that have specific
capabilities over a token factory denom. Right now there is only one Admin
permission, but is planned to be extended to the future.
*/
export interface Tokenfactoryv1DenomAuthorityMetadata {
  admin?: string;
}

/**
 * Params defines the parameters for the tokenfactory module.
 */
export type Tokenfactoryv1Params = object;

export type V1MsgBurnResponse = object;

/**
* MsgChangeAdminResponse defines the response structure for an executed
MsgChangeAdmin message.
*/
export type V1MsgChangeAdminResponse = object;

export interface V1MsgCreateDenomResponse {
  new_token_denom?: string;
}

export type V1MsgMintResponse = object;

/**
* MsgSetDenomMetadataResponse defines the response structure for an executed
MsgSetDenomMetadata message.
*/
export type V1MsgSetDenomMetadataResponse = object;

/**
* QueryDenomAuthorityMetadataResponse defines the response structure for the
DenomAuthorityMetadata gRPC query.
*/
export interface V1QueryDenomAuthorityMetadataResponse {
  /**
   * DenomAuthorityMetadata specifies metadata for addresses that have specific
   * capabilities over a token factory denom. Right now there is only one Admin
   * permission, but is planned to be extended to the future.
   */
  authority_metadata?: Tokenfactoryv1DenomAuthorityMetadata;
}

/**
* QueryDenomMetadataResponse is the response type for the Query/DenomMetadata gRPC
method.
*/
export interface V1QueryDenomMetadataResponse {
  /** metadata describes and provides all the client information for the requested token. */
  metadata?: V1Beta1Metadata;
}

/**
* QueryDenomsFromCreatorRequest defines the response structure for the
DenomsFromCreator gRPC query.
*/
export interface V1QueryDenomsFromCreatorResponse {
  denoms?: string[];
}

/**
 * QueryParamsResponse is the response type for the Query/Params RPC method.
 */
export interface V1QueryParamsResponse {
  /** params defines the parameters of the module. */
  params?: Tokenfactoryv1Params;
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
 * @title tokenfactory/v1/authorityMetadata.proto
 * @version version not set
 */
export class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
  /**
   * No description
   *
   * @tags Query
   * @name QueryDeprecatedDenomMetadata
   * @summary This endpoint is deprecated and will be removed in the future. Please use the `/sei/tokenfactory/v1/denoms/metadata` instead.
   * @request GET:/sei-protocol/seichain/tokenfactory/denoms/metadata
   */
  queryDeprecatedDenomMetadata = (query?: { denom?: string }, params: RequestParams = {}) =>
    this.request<V1QueryDenomMetadataResponse, RpcStatus>({
      path: `/sei-protocol/seichain/tokenfactory/denoms/metadata`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryDeprecatedDenomAuthorityMetadata
   * @summary This endpoint is deprecated and will be removed in the future. Please use the `/sei/tokenfactory/v1/denoms/{denom}/authority_metadata` instead.
   * @request GET:/sei-protocol/seichain/tokenfactory/denoms/{denom}/authority_metadata
   */
  queryDeprecatedDenomAuthorityMetadata = (denom: string, params: RequestParams = {}) =>
    this.request<V1QueryDenomAuthorityMetadataResponse, RpcStatus>({
      path: `/sei-protocol/seichain/tokenfactory/denoms/${denom}/authority_metadata`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryDeprecatedDenomsFromCreator
   * @summary This endpoint is deprecated and will be removed in the future. Please use the `/sei/tokenfactory/v1/denoms_from_creator/{creator}` instead.
   * @request GET:/sei-protocol/seichain/tokenfactory/denoms_from_creator/{creator}
   */
  queryDeprecatedDenomsFromCreator = (creator: string, params: RequestParams = {}) =>
    this.request<V1QueryDenomsFromCreatorResponse, RpcStatus>({
      path: `/sei-protocol/seichain/tokenfactory/denoms_from_creator/${creator}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
 * No description
 * 
 * @tags Query
 * @name QueryDenomMetadata
 * @summary DenomsMetadata defines a gRPC query method for fetching
 DenomMetadata for a particular denom.
 * @request GET:/sei/tokenfactory/v1/denoms/metadata
 */
  queryDenomMetadata = (query?: { denom?: string }, params: RequestParams = {}) =>
    this.request<V1QueryDenomMetadataResponse, RpcStatus>({
      path: `/sei/tokenfactory/v1/denoms/metadata`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
 * No description
 * 
 * @tags Query
 * @name QueryDenomAuthorityMetadata
 * @summary DenomAuthorityMetadata defines a gRPC query method for fetching
DenomAuthorityMetadata for a particular denom.
 * @request GET:/sei/tokenfactory/v1/denoms/{denom}/authority_metadata
 */
  queryDenomAuthorityMetadata = (denom: string, params: RequestParams = {}) =>
    this.request<V1QueryDenomAuthorityMetadataResponse, RpcStatus>({
      path: `/sei/tokenfactory/v1/denoms/${denom}/authority_metadata`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
 * No description
 * 
 * @tags Query
 * @name QueryDenomsFromCreator
 * @summary DenomsFromCreator defines a gRPC query method for fetching all
denominations created by a specific admin/creator.
 * @request GET:/sei/tokenfactory/v1/denoms_from_creator/{creator}
 */
  queryDenomsFromCreator = (creator: string, params: RequestParams = {}) =>
    this.request<V1QueryDenomsFromCreatorResponse, RpcStatus>({
      path: `/sei/tokenfactory/v1/denoms_from_creator/${creator}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
 * No description
 * 
 * @tags Query
 * @name QueryParams
 * @summary Params defines a gRPC query method that returns the tokenfactory module's
parameters.
 * @request GET:/sei/tokenfactory/v1/params
 */
  queryParams = (params: RequestParams = {}) =>
    this.request<V1QueryParamsResponse, RpcStatus>({
      path: `/sei/tokenfactory/v1/params`,
      method: "GET",
      format: "json",
      ...params,
    });
}
