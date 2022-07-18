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
  block_height?: string;

  /** @format uint64 */
  tx_index?: string;
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
export enum V1AccessType {
  ACCESS_TYPE_UNSPECIFIED = "ACCESS_TYPE_UNSPECIFIED",
  ACCESS_TYPE_NOBODY = "ACCESS_TYPE_NOBODY",
  ACCESS_TYPE_ONLY_ADDRESS = "ACCESS_TYPE_ONLY_ADDRESS",
  ACCESS_TYPE_EVERYBODY = "ACCESS_TYPE_EVERYBODY",
}

export interface V1CodeInfoResponse {
  /** @format uint64 */
  code_id?: string;
  creator?: string;

  /** @format byte */
  data_hash?: string;

  /** AccessConfig access control type. */
  instantiate_permission?: V1AccessConfig;
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
  code_id?: string;

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
export enum V1ContractCodeHistoryOperationType {
  CONTRACT_CODE_HISTORY_OPERATION_TYPE_UNSPECIFIED = "CONTRACT_CODE_HISTORY_OPERATION_TYPE_UNSPECIFIED",
  CONTRACT_CODE_HISTORY_OPERATION_TYPE_INIT = "CONTRACT_CODE_HISTORY_OPERATION_TYPE_INIT",
  CONTRACT_CODE_HISTORY_OPERATION_TYPE_MIGRATE = "CONTRACT_CODE_HISTORY_OPERATION_TYPE_MIGRATE",
  CONTRACT_CODE_HISTORY_OPERATION_TYPE_GENESIS = "CONTRACT_CODE_HISTORY_OPERATION_TYPE_GENESIS",
}

export interface V1ContractInfo {
  /** @format uint64 */
  code_id?: string;
  creator?: string;
  admin?: string;

  /** Label is optional metadata to be stored with a contract instance. */
  label?: string;

  /**
   * AbsoluteTxPosition is a unique transaction position that allows for global
   * ordering of transactions.
   */
  created?: V1AbsoluteTxPosition;
  ibc_port_id?: string;

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

export type V1MsgClearAdminResponse = object;

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
  code_id?: string;
}

export type V1MsgUpdateAdminResponse = object;

export interface V1QueryAllContractStateResponse {
  models?: V1Model[];

  /** pagination defines the pagination in the response. */
  pagination?: V1Beta1PageResponse;
}

export interface V1QueryCodeResponse {
  code_info?: V1CodeInfoResponse;

  /** @format byte */
  data?: string;
}

export interface V1QueryCodesResponse {
  code_infos?: V1CodeInfoResponse[];

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
  contract_info?: V1ContractInfo;
}

export interface V1QueryContractsByCodeResponse {
  contracts?: string[];

  /** pagination defines the pagination in the response. */
  pagination?: V1Beta1PageResponse;
}

export interface V1QueryPinnedCodesResponse {
  code_ids?: string[];

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
 * @title cosmwasm/wasm/v1/genesis.proto
 * @version version not set
 */
export class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
  /**
   * No description
   *
   * @tags Query
   * @name QueryCodes
   * @summary Codes gets the metadata for all stored wasm codes
   * @request GET:/cosmwasm/wasm/v1/code
   */
  queryCodes = (
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V1QueryCodesResponse, RpcStatus>({
      path: `/cosmwasm/wasm/v1/code`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryCode
   * @summary Code gets the binary code and metadata for a singe wasm code
   * @request GET:/cosmwasm/wasm/v1/code/{code_id}
   */
  queryCode = (code_id: string, params: RequestParams = {}) =>
    this.request<V1QueryCodeResponse, RpcStatus>({
      path: `/cosmwasm/wasm/v1/code/${code_id}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryContractsByCode
   * @summary ContractsByCode lists all smart contracts for a code id
   * @request GET:/cosmwasm/wasm/v1/code/{code_id}/contracts
   */
  queryContractsByCode = (
    code_id: string,
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V1QueryContractsByCodeResponse, RpcStatus>({
      path: `/cosmwasm/wasm/v1/code/${code_id}/contracts`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryPinnedCodes
   * @summary PinnedCodes gets the pinned code ids
   * @request GET:/cosmwasm/wasm/v1/codes/pinned
   */
  queryPinnedCodes = (
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V1QueryPinnedCodesResponse, RpcStatus>({
      path: `/cosmwasm/wasm/v1/codes/pinned`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryContractInfo
   * @summary ContractInfo gets the contract meta data
   * @request GET:/cosmwasm/wasm/v1/contract/{address}
   */
  queryContractInfo = (address: string, params: RequestParams = {}) =>
    this.request<V1QueryContractInfoResponse, RpcStatus>({
      path: `/cosmwasm/wasm/v1/contract/${address}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryContractHistory
   * @summary ContractHistory gets the contract code history
   * @request GET:/cosmwasm/wasm/v1/contract/{address}/history
   */
  queryContractHistory = (
    address: string,
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V1QueryContractHistoryResponse, RpcStatus>({
      path: `/cosmwasm/wasm/v1/contract/${address}/history`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryRawContractState
   * @summary RawContractState gets single key from the raw store data of a contract
   * @request GET:/cosmwasm/wasm/v1/contract/{address}/raw/{query_data}
   */
  queryRawContractState = (address: string, query_data: string, params: RequestParams = {}) =>
    this.request<V1QueryRawContractStateResponse, RpcStatus>({
      path: `/cosmwasm/wasm/v1/contract/${address}/raw/${query_data}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QuerySmartContractState
   * @summary SmartContractState get smart query result from the contract
   * @request GET:/cosmwasm/wasm/v1/contract/{address}/smart/{query_data}
   */
  querySmartContractState = (address: string, query_data: string, params: RequestParams = {}) =>
    this.request<V1QuerySmartContractStateResponse, RpcStatus>({
      path: `/cosmwasm/wasm/v1/contract/${address}/smart/${query_data}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryAllContractState
   * @summary AllContractState gets all raw store data for a single contract
   * @request GET:/cosmwasm/wasm/v1/contract/{address}/state
   */
  queryAllContractState = (
    address: string,
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V1QueryAllContractStateResponse, RpcStatus>({
      path: `/cosmwasm/wasm/v1/contract/${address}/state`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });
}
