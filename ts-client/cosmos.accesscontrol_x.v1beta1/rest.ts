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

export type AccesscontrolXv1Beta1Params = object;

export interface Accesscontrolv1Beta1WasmDependencyMapping {
  base_access_ops?: V1Beta1WasmAccessOperation[];
  query_access_ops?: V1Beta1WasmAccessOperations[];
  execute_access_ops?: V1Beta1WasmAccessOperations[];
  base_contract_references?: V1Beta1WasmContractReference[];
  query_contract_references?: V1Beta1WasmContractReferences[];
  execute_contract_references?: V1Beta1WasmContractReferences[];
  reset_reason?: string;
  contract_address?: string;
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

export interface V1Beta1AccessOperation {
  access_type?: V1Beta1AccessType;
  resource_type?: V1Beta1ResourceType;
  identifier_template?: string;
}

export enum V1Beta1AccessOperationSelectorType {
  NONE = "NONE",
  JQ = "JQ",
  JQBECH32ADDRESS = "JQ_BECH32_ADDRESS",
  JQ_LENGTH_PREFIXED_ADDRESS = "JQ_LENGTH_PREFIXED_ADDRESS",
  SENDERBECH32ADDRESS = "SENDER_BECH32_ADDRESS",
  SENDER_LENGTH_PREFIXED_ADDRESS = "SENDER_LENGTH_PREFIXED_ADDRESS",
  CONTRACT_ADDRESS = "CONTRACT_ADDRESS",
  JQ_MESSAGE_CONDITIONAL = "JQ_MESSAGE_CONDITIONAL",
  CONSTANT_STRING_TO_HEX = "CONSTANT_STRING_TO_HEX",
  CONTRACT_REFERENCE = "CONTRACT_REFERENCE",
}

export enum V1Beta1AccessType {
  UNKNOWN = "UNKNOWN",
  READ = "READ",
  WRITE = "WRITE",
  COMMIT = "COMMIT",
}

export interface V1Beta1ListResourceDependencyMappingResponse {
  message_dependency_mapping_list?: V1Beta1MessageDependencyMapping[];
}

export interface V1Beta1ListWasmDependencyMappingResponse {
  wasm_dependency_mapping_list?: Accesscontrolv1Beta1WasmDependencyMapping[];
}

export interface V1Beta1MessageDependencyMapping {
  message_key?: string;
  access_ops?: V1Beta1AccessOperation[];
  dynamic_enabled?: boolean;
}

export type V1Beta1MsgRegisterWasmDependencyResponse = object;

export interface V1Beta1QueryParamsResponse {
  /** params defines the parameters of the module. */
  params?: AccesscontrolXv1Beta1Params;
}

export interface V1Beta1ResourceDependencyMappingFromMessageKeyResponse {
  message_dependency_mapping?: V1Beta1MessageDependencyMapping;
}

export enum V1Beta1ResourceType {
  ANY = "ANY",
  KV = "KV",
  Mem = "Mem",
  DexMem = "DexMem",
  KV_BANK = "KV_BANK",
  KV_STAKING = "KV_STAKING",
  KV_WASM = "KV_WASM",
  KV_ORACLE = "KV_ORACLE",
  KV_DEX = "KV_DEX",
  KV_EPOCH = "KV_EPOCH",
  KV_TOKENFACTORY = "KV_TOKENFACTORY",
  KV_ORACLE_VOTE_TARGETS = "KV_ORACLE_VOTE_TARGETS",
  KV_ORACLE_AGGREGATE_VOTES = "KV_ORACLE_AGGREGATE_VOTES",
  KV_ORACLE_FEEDERS = "KV_ORACLE_FEEDERS",
  KV_STAKING_DELEGATION = "KV_STAKING_DELEGATION",
  KV_STAKING_VALIDATOR = "KV_STAKING_VALIDATOR",
  KV_AUTH = "KV_AUTH",
  KV_AUTH_ADDRESS_STORE = "KV_AUTH_ADDRESS_STORE",
  KV_BANK_SUPPLY = "KV_BANK_SUPPLY",
  KV_BANK_DENOM = "KV_BANK_DENOM",
  KV_BANK_BALANCES = "KV_BANK_BALANCES",
  KV_TOKENFACTORY_DENOM = "KV_TOKENFACTORY_DENOM",
  KV_TOKENFACTORY_METADATA = "KV_TOKENFACTORY_METADATA",
  KV_TOKENFACTORY_ADMIN = "KV_TOKENFACTORY_ADMIN",
  KV_TOKENFACTORY_CREATOR = "KV_TOKENFACTORY_CREATOR",
  KV_ORACLE_EXCHANGE_RATE = "KV_ORACLE_EXCHANGE_RATE",
  KV_ORACLE_VOTE_PENALTY_COUNTER = "KV_ORACLE_VOTE_PENALTY_COUNTER",
  KV_ORACLE_PRICE_SNAPSHOT = "KV_ORACLE_PRICE_SNAPSHOT",
  KV_STAKING_VALIDATION_POWER = "KV_STAKING_VALIDATION_POWER",
  KV_STAKING_TOTAL_POWER = "KV_STAKING_TOTAL_POWER",
  KV_STAKING_VALIDATORS_CON_ADDR = "KV_STAKING_VALIDATORS_CON_ADDR",
  KV_STAKING_UNBONDING_DELEGATION = "KV_STAKING_UNBONDING_DELEGATION",
  KV_STAKING_UNBONDING_DELEGATION_VAL = "KV_STAKING_UNBONDING_DELEGATION_VAL",
  KV_STAKING_REDELEGATION = "KV_STAKING_REDELEGATION",
  KV_STAKING_REDELEGATION_VAL_SRC = "KV_STAKING_REDELEGATION_VAL_SRC",
  KV_STAKING_REDELEGATION_VAL_DST = "KV_STAKING_REDELEGATION_VAL_DST",
  KV_STAKING_REDELEGATION_QUEUE = "KV_STAKING_REDELEGATION_QUEUE",
  KV_STAKING_VALIDATOR_QUEUE = "KV_STAKING_VALIDATOR_QUEUE",
  KV_STAKING_HISTORICAL_INFO = "KV_STAKING_HISTORICAL_INFO",
  KV_STAKING_UNBONDING = "KV_STAKING_UNBONDING",
  KV_STAKING_VALIDATORS_BY_POWER = "KV_STAKING_VALIDATORS_BY_POWER",
  KV_DISTRIBUTION = "KV_DISTRIBUTION",
  KV_DISTRIBUTION_FEE_POOL = "KV_DISTRIBUTION_FEE_POOL",
  KV_DISTRIBUTION_PROPOSER_KEY = "KV_DISTRIBUTION_PROPOSER_KEY",
  KV_DISTRIBUTION_OUTSTANDING_REWARDS = "KV_DISTRIBUTION_OUTSTANDING_REWARDS",
  KV_DISTRIBUTION_DELEGATOR_WITHDRAW_ADDR = "KV_DISTRIBUTION_DELEGATOR_WITHDRAW_ADDR",
  KV_DISTRIBUTION_DELEGATOR_STARTING_INFO = "KV_DISTRIBUTION_DELEGATOR_STARTING_INFO",
  KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS = "KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS",
  KV_DISTRIBUTION_VAL_CURRENT_REWARDS = "KV_DISTRIBUTION_VAL_CURRENT_REWARDS",
  KV_DISTRIBUTION_VAL_ACCUM_COMMISSION = "KV_DISTRIBUTION_VAL_ACCUM_COMMISSION",
  KV_DISTRIBUTION_SLASH_EVENT = "KV_DISTRIBUTION_SLASH_EVENT",
  KV_DEX_CONTRACT_LONGBOOK = "KV_DEX_CONTRACT_LONGBOOK",
  KV_DEX_CONTRACT_SHORTBOOK = "KV_DEX_CONTRACT_SHORTBOOK",
  KV_DEX_SETTLEMENT = "KV_DEX_SETTLEMENT",
  KV_DEX_PAIR_PREFIX = "KV_DEX_PAIR_PREFIX",
  KV_DEX_TWAP = "KV_DEX_TWAP",
  KV_DEX_PRICE = "KV_DEX_PRICE",
  KV_DEX_SETTLEMENT_ENTRY = "KV_DEX_SETTLEMENT_ENTRY",
  KV_DEX_REGISTERED_PAIR = "KV_DEX_REGISTERED_PAIR",
  KV_DEX_ORDER = "KV_DEX_ORDER",
  KV_DEX_CANCEL = "KV_DEX_CANCEL",
  KV_DEX_ACCOUNT_ACTIVE_ORDERS = "KV_DEX_ACCOUNT_ACTIVE_ORDERS",
  KV_DEX_ASSET_LIST = "KV_DEX_ASSET_LIST",
  KV_DEX_NEXT_ORDER_ID = "KV_DEX_NEXT_ORDER_ID",
  KV_DEX_NEXT_SETTLEMENT_ID = "KV_DEX_NEXT_SETTLEMENT_ID",
  KV_DEX_MATCH_RESULT = "KV_DEX_MATCH_RESULT",
  KV_DEX_SETTLEMENT_ORDER_ID = "KV_DEX_SETTLEMENT_ORDER_ID",
  KV_DEX_ORDER_BOOK = "KV_DEX_ORDER_BOOK",
  KV_ACCESSCONTROL = "KV_ACCESSCONTROL",
  KV_ACCESSCONTROL_WASM_DEPENDENCY_MAPPING = "KV_ACCESSCONTROL_WASM_DEPENDENCY_MAPPING",
  KV_WASM_CODE = "KV_WASM_CODE",
  KV_WASM_CONTRACT_ADDRESS = "KV_WASM_CONTRACT_ADDRESS",
  KV_WASM_CONTRACT_STORE = "KV_WASM_CONTRACT_STORE",
  KV_WASM_SEQUENCE_KEY = "KV_WASM_SEQUENCE_KEY",
  KV_WASM_CONTRACT_CODE_HISTORY = "KV_WASM_CONTRACT_CODE_HISTORY",
  KV_WASM_CONTRACT_BY_CODE_ID = "KV_WASM_CONTRACT_BY_CODE_ID",
  KV_WASM_PINNED_CODE_INDEX = "KV_WASM_PINNED_CODE_INDEX",
  KV_AUTH_GLOBAL_ACCOUNT_NUMBER = "KV_AUTH_GLOBAL_ACCOUNT_NUMBER",
  KV_AUTHZ = "KV_AUTHZ",
  KV_FEEGRANT = "KV_FEEGRANT",
  KV_FEEGRANT_ALLOWANCE = "KV_FEEGRANT_ALLOWANCE",
  KV_SLASHING = "KV_SLASHING",
  KV_SLASHING_VAL_SIGNING_INFO = "KV_SLASHING_VAL_SIGNING_INFO",
  KV_SLASHING_ADDR_PUBKEY_RELATION_KEY = "KV_SLASHING_ADDR_PUBKEY_RELATION_KEY",
  KV_DEX_MEM_ORDER = "KV_DEX_MEM_ORDER",
  KV_DEX_MEM_CANCEL = "KV_DEX_MEM_CANCEL",
  KV_DEX_MEM_DEPOSIT = "KV_DEX_MEM_DEPOSIT",
  KV_DEX_CONTRACT = "KV_DEX_CONTRACT",
  KV_DEX_LONG_ORDER_COUNT = "KV_DEX_LONG_ORDER_COUNT",
  KV_DEX_SHORT_ORDER_COUNT = "KV_DEX_SHORT_ORDER_COUNT",
  KV_BANK_DEFERRED = "KV_BANK_DEFERRED",
  KV_BANK_DEFERRED_MODULE_TX_INDEX = "KV_BANK_DEFERRED_MODULE_TX_INDEX",
  KV_EVM = "KV_EVM",
  KV_EVM_BALANCE = "KV_EVM_BALANCE",
  KV_EVM_TRANSIENT = "KV_EVM_TRANSIENT",
  KV_EVM_ACCOUNT_TRANSIENT = "KV_EVM_ACCOUNT_TRANSIENT",
  KV_EVM_MODULE_TRANSIENT = "KV_EVM_MODULE_TRANSIENT",
  KV_EVM_NONCE = "KV_EVM_NONCE",
  KV_EVM_RECEIPT = "KV_EVM_RECEIPT",
  KVEVMS2E = "KV_EVM_S2E",
  KVEVME2S = "KV_EVM_E2S",
  KV_EVM_CODE_HASH = "KV_EVM_CODE_HASH",
  KV_EVM_CODE = "KV_EVM_CODE",
  KV_EVM_CODE_SIZE = "KV_EVM_CODE_SIZE",
  KV_BANK_WEI_BALANCE = "KV_BANK_WEI_BALANCE",
  KV_DEX_MEM_CONTRACTS_TO_PROCESS = "KV_DEX_MEM_CONTRACTS_TO_PROCESS",
  KV_DEX_MEM_DOWNSTREAM_CONTRACTS = "KV_DEX_MEM_DOWNSTREAM_CONTRACTS",
}

export interface V1Beta1WasmAccessOperation {
  operation?: V1Beta1AccessOperation;
  selector_type?: V1Beta1AccessOperationSelectorType;
  selector?: string;
}

export interface V1Beta1WasmAccessOperations {
  message_name?: string;
  wasm_operations?: V1Beta1WasmAccessOperation[];
}

export interface V1Beta1WasmContractReference {
  contract_address?: string;
  message_type?: V1Beta1WasmMessageSubtype;
  message_name?: string;
  json_translation_template?: string;
}

export interface V1Beta1WasmContractReferences {
  message_name?: string;
  contract_references?: V1Beta1WasmContractReference[];
}

export interface V1Beta1WasmDependencyMappingResponse {
  wasm_dependency_mapping?: Accesscontrolv1Beta1WasmDependencyMapping;
}

export enum V1Beta1WasmMessageSubtype {
  QUERY = "QUERY",
  EXECUTE = "EXECUTE",
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
 * @title cosmos/accesscontrol_x/genesis.proto
 * @version version not set
 */
export class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
  /**
   * No description
   *
   * @tags Query
   * @name QueryListResourceDependencyMapping
   * @request GET:/cosmos/cosmos-sdk/accesscontrol/list_resource_dependency_mapping
   */
  queryListResourceDependencyMapping = (params: RequestParams = {}) =>
    this.request<V1Beta1ListResourceDependencyMappingResponse, RpcStatus>({
      path: `/cosmos/cosmos-sdk/accesscontrol/list_resource_dependency_mapping`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryListWasmDependencyMapping
   * @request GET:/cosmos/cosmos-sdk/accesscontrol/list_wasm_dependency_mapping
   */
  queryListWasmDependencyMapping = (params: RequestParams = {}) =>
    this.request<V1Beta1ListWasmDependencyMappingResponse, RpcStatus>({
      path: `/cosmos/cosmos-sdk/accesscontrol/list_wasm_dependency_mapping`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryParams
   * @request GET:/cosmos/cosmos-sdk/accesscontrol/params
   */
  queryParams = (params: RequestParams = {}) =>
    this.request<V1Beta1QueryParamsResponse, RpcStatus>({
      path: `/cosmos/cosmos-sdk/accesscontrol/params`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryResourceDependencyMappingFromMessageKey
   * @request GET:/cosmos/cosmos-sdk/accesscontrol/resource_dependency_mapping_from_message_key/{message_key}
   */
  queryResourceDependencyMappingFromMessageKey = (message_key: string, params: RequestParams = {}) =>
    this.request<V1Beta1ResourceDependencyMappingFromMessageKeyResponse, RpcStatus>({
      path: `/cosmos/cosmos-sdk/accesscontrol/resource_dependency_mapping_from_message_key/${message_key}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryWasmDependencyMapping
   * @request GET:/cosmos/cosmos-sdk/accesscontrol/wasm_dependency_mapping/{contract_address}
   */
  queryWasmDependencyMapping = (contract_address: string, params: RequestParams = {}) =>
    this.request<V1Beta1WasmDependencyMappingResponse, RpcStatus>({
      path: `/cosmos/cosmos-sdk/accesscontrol/wasm_dependency_mapping/${contract_address}`,
      method: "GET",
      format: "json",
      ...params,
    });
}
