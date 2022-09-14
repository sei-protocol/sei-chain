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

export interface CryptoPublicKey {
  /** @format byte */
  ed25519?: string;

  /** @format byte */
  secp256k1?: string;
}

export interface P2PDefaultNodeInfo {
  protocol_version?: P2PProtocolVersion;
  default_node_id?: string;
  listen_addr?: string;
  network?: string;
  version?: string;

  /** @format byte */
  channels?: string;
  moniker?: string;
  other?: P2PDefaultNodeInfoOther;
}

export interface P2PDefaultNodeInfoOther {
  tx_index?: string;
  rpc_address?: string;
}

export interface P2PProtocolVersion {
  /** @format uint64 */
  p2p?: string;

  /** @format uint64 */
  block?: string;

  /** @format uint64 */
  app?: string;
}

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

export interface TenderminttypesValidator {
  /** @format byte */
  address?: string;
  pub_key?: CryptoPublicKey;

  /** @format int64 */
  voting_power?: string;

  /** @format int64 */
  proposer_priority?: string;
}

/**
 * Validator is the type for the validator-set.
 */
export interface Tendermintv1Beta1Validator {
  address?: string;

  /**
   * `Any` contains an arbitrary serialized protocol buffer message along with a
   * URL that describes the type of the serialized message.
   *
   * Protobuf library provides support to pack/unpack Any values in the form
   * of utility functions or additional generated methods of the Any type.
   *
   * Example 1: Pack and unpack a message in C++.
   *
   *     Foo foo = ...;
   *     Any any;
   *     any.PackFrom(foo);
   *     ...
   *     if (any.UnpackTo(&foo)) {
   *       ...
   *     }
   *
   * Example 2: Pack and unpack a message in Java.
   *
   *     Foo foo = ...;
   *     Any any = Any.pack(foo);
   *     ...
   *     if (any.is(Foo.class)) {
   *       foo = any.unpack(Foo.class);
   *     }
   *
   *  Example 3: Pack and unpack a message in Python.
   *
   *     foo = Foo(...)
   *     any = Any()
   *     any.Pack(foo)
   *     ...
   *     if any.Is(Foo.DESCRIPTOR):
   *       any.Unpack(foo)
   *       ...
   *
   *  Example 4: Pack and unpack a message in Go
   *
   *      foo := &pb.Foo{...}
   *      any, err := anypb.New(foo)
   *      if err != nil {
   *        ...
   *      }
   *      ...
   *      foo := &pb.Foo{}
   *      if err := any.UnmarshalTo(foo); err != nil {
   *        ...
   *      }
   *
   * The pack methods provided by protobuf library will by default use
   * 'type.googleapis.com/full.type.name' as the type URL and the unpack
   * methods only use the fully qualified type name after the last '/'
   * in the type URL, for example "foo.bar.com/x/y.z" will yield type
   * name "y.z".
   *
   *
   * JSON
   * ====
   * The JSON representation of an `Any` value uses the regular
   * representation of the deserialized, embedded message, with an
   * additional field `@type` which contains the type URL. Example:
   *
   *     package google.profile;
   *     message Person {
   *       string first_name = 1;
   *       string last_name = 2;
   *     }
   *
   *     {
   *       "@type": "type.googleapis.com/google.profile.Person",
   *       "firstName": <string>,
   *       "lastName": <string>
   *     }
   *
   * If the embedded message type is well-known and has a custom JSON
   * representation, that representation will be embedded adding a field
   * `value` which holds the custom JSON in addition to the `@type`
   * field. Example (for message [google.protobuf.Duration][]):
   *
   *     {
   *       "@type": "type.googleapis.com/google.protobuf.Duration",
   *       "value": "1.212s"
   *     }
   */
  pub_key?: ProtobufAny;

  /** @format int64 */
  voting_power?: string;

  /** @format int64 */
  proposer_priority?: string;
}

export interface TypesBlock {
  /** Header defines the structure of a Tendermint block header. */
  header?: TypesHeader;
  data?: TypesData;
  evidence?: TypesEvidenceList;

  /** Commit contains the evidence that a block was committed by a set of validators. */
  last_commit?: TypesCommit;
}

export interface TypesBlockID {
  /** @format byte */
  hash?: string;
  part_set_header?: TypesPartSetHeader;
}

export enum TypesBlockIDFlag {
  BLOCK_ID_FLAG_UNKNOWN = "BLOCK_ID_FLAG_UNKNOWN",
  BLOCK_ID_FLAG_ABSENT = "BLOCK_ID_FLAG_ABSENT",
  BLOCK_ID_FLAG_COMMIT = "BLOCK_ID_FLAG_COMMIT",
  BLOCK_ID_FLAG_NIL = "BLOCK_ID_FLAG_NIL",
}

/**
 * Commit contains the evidence that a block was committed by a set of validators.
 */
export interface TypesCommit {
  /** @format int64 */
  height?: string;

  /** @format int32 */
  round?: number;
  block_id?: TypesBlockID;
  signatures?: TypesCommitSig[];
}

/**
 * CommitSig is a part of the Vote included in a Commit.
 */
export interface TypesCommitSig {
  block_id_flag?: TypesBlockIDFlag;

  /** @format byte */
  validator_address?: string;

  /** @format date-time */
  timestamp?: string;

  /** @format byte */
  signature?: string;
}

export interface TypesData {
  /**
   * Txs that will be applied by state @ block.Height+1.
   * NOTE: not all txs here are valid.  We're just agreeing on the order first.
   * This means that block.AppHash does not include these txs.
   */
  txs?: string[];
}

/**
 * DuplicateVoteEvidence contains evidence of a validator signed two conflicting votes.
 */
export interface TypesDuplicateVoteEvidence {
  /**
   * Vote represents a prevote, precommit, or commit vote from validators for
   * consensus.
   */
  vote_a?: TypesVote;

  /**
   * Vote represents a prevote, precommit, or commit vote from validators for
   * consensus.
   */
  vote_b?: TypesVote;

  /** @format int64 */
  total_voting_power?: string;

  /** @format int64 */
  validator_power?: string;

  /** @format date-time */
  timestamp?: string;
}

export interface TypesEvidence {
  /** DuplicateVoteEvidence contains evidence of a validator signed two conflicting votes. */
  duplicate_vote_evidence?: TypesDuplicateVoteEvidence;

  /** LightClientAttackEvidence contains evidence of a set of validators attempting to mislead a light client. */
  light_client_attack_evidence?: TypesLightClientAttackEvidence;
}

export interface TypesEvidenceList {
  evidence?: TypesEvidence[];
}

/**
 * Header defines the structure of a Tendermint block header.
 */
export interface TypesHeader {
  /**
   * Consensus captures the consensus rules for processing a block in the blockchain,
   * including all blockchain data structures and the rules of the application's
   * state transition machine.
   */
  version?: VersionConsensus;
  chain_id?: string;

  /** @format int64 */
  height?: string;

  /** @format date-time */
  time?: string;
  last_block_id?: TypesBlockID;

  /** @format byte */
  last_commit_hash?: string;

  /** @format byte */
  data_hash?: string;

  /** @format byte */
  validators_hash?: string;

  /** @format byte */
  next_validators_hash?: string;

  /** @format byte */
  consensus_hash?: string;

  /** @format byte */
  app_hash?: string;

  /** @format byte */
  last_results_hash?: string;

  /** @format byte */
  evidence_hash?: string;

  /** @format byte */
  proposer_address?: string;
}

export interface TypesLightBlock {
  signed_header?: TypesSignedHeader;
  validator_set?: TypesValidatorSet;
}

/**
 * LightClientAttackEvidence contains evidence of a set of validators attempting to mislead a light client.
 */
export interface TypesLightClientAttackEvidence {
  conflicting_block?: TypesLightBlock;

  /** @format int64 */
  common_height?: string;
  byzantine_validators?: TenderminttypesValidator[];

  /** @format int64 */
  total_voting_power?: string;

  /** @format date-time */
  timestamp?: string;
}

export interface TypesPartSetHeader {
  /** @format int64 */
  total?: number;

  /** @format byte */
  hash?: string;
}

export interface TypesSignedHeader {
  /** Header defines the structure of a Tendermint block header. */
  header?: TypesHeader;

  /** Commit contains the evidence that a block was committed by a set of validators. */
  commit?: TypesCommit;
}

/**
* SignedMsgType is a type of signed message in the consensus.

 - SIGNED_MSG_TYPE_PREVOTE: Votes
 - SIGNED_MSG_TYPE_PROPOSAL: Proposals
*/
export enum TypesSignedMsgType {
  SIGNED_MSG_TYPE_UNKNOWN = "SIGNED_MSG_TYPE_UNKNOWN",
  SIGNED_MSG_TYPE_PREVOTE = "SIGNED_MSG_TYPE_PREVOTE",
  SIGNED_MSG_TYPE_PRECOMMIT = "SIGNED_MSG_TYPE_PRECOMMIT",
  SIGNED_MSG_TYPE_PROPOSAL = "SIGNED_MSG_TYPE_PROPOSAL",
}

export interface TypesValidatorSet {
  validators?: TenderminttypesValidator[];
  proposer?: TenderminttypesValidator;

  /** @format int64 */
  total_voting_power?: string;
}

/**
* Vote represents a prevote, precommit, or commit vote from validators for
consensus.
*/
export interface TypesVote {
  /**
   * SignedMsgType is a type of signed message in the consensus.
   *
   *  - SIGNED_MSG_TYPE_PREVOTE: Votes
   *  - SIGNED_MSG_TYPE_PROPOSAL: Proposals
   */
  type?: TypesSignedMsgType;

  /** @format int64 */
  height?: string;

  /** @format int32 */
  round?: number;
  block_id?: TypesBlockID;

  /** @format date-time */
  timestamp?: string;

  /** @format byte */
  validator_address?: string;

  /** @format int32 */
  validator_index?: number;

  /** @format byte */
  signature?: string;
}

/**
 * GetBlockByHeightResponse is the response type for the Query/GetBlockByHeight RPC method.
 */
export interface V1Beta1GetBlockByHeightResponse {
  block_id?: TypesBlockID;
  block?: TypesBlock;
}

/**
 * GetLatestBlockResponse is the response type for the Query/GetLatestBlock RPC method.
 */
export interface V1Beta1GetLatestBlockResponse {
  block_id?: TypesBlockID;
  block?: TypesBlock;
}

/**
 * GetLatestValidatorSetResponse is the response type for the Query/GetValidatorSetByHeight RPC method.
 */
export interface V1Beta1GetLatestValidatorSetResponse {
  /** @format int64 */
  block_height?: string;
  validators?: Tendermintv1Beta1Validator[];

  /** pagination defines an pagination for the response. */
  pagination?: V1Beta1PageResponse;
}

/**
 * GetNodeInfoResponse is the request type for the Query/GetNodeInfo RPC method.
 */
export interface V1Beta1GetNodeInfoResponse {
  default_node_info?: P2PDefaultNodeInfo;

  /** VersionInfo is the type for the GetNodeInfoResponse message. */
  application_version?: V1Beta1VersionInfo;
}

/**
 * GetSyncingResponse is the response type for the Query/GetSyncing RPC method.
 */
export interface V1Beta1GetSyncingResponse {
  syncing?: boolean;
}

/**
 * GetValidatorSetByHeightResponse is the response type for the Query/GetValidatorSetByHeight RPC method.
 */
export interface V1Beta1GetValidatorSetByHeightResponse {
  /** @format int64 */
  block_height?: string;
  validators?: Tendermintv1Beta1Validator[];

  /** pagination defines an pagination for the response. */
  pagination?: V1Beta1PageResponse;
}

export interface V1Beta1Module {
  path?: string;
  version?: string;
  sum?: string;
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

/**
 * VersionInfo is the type for the GetNodeInfoResponse message.
 */
export interface V1Beta1VersionInfo {
  name?: string;
  app_name?: string;
  version?: string;
  git_commit?: string;
  build_tags?: string;
  go_version?: string;
  build_deps?: V1Beta1Module[];
  cosmos_sdk_version?: string;
}

/**
* Consensus captures the consensus rules for processing a block in the blockchain,
including all blockchain data structures and the rules of the application's
state transition machine.
*/
export interface VersionConsensus {
  /** @format uint64 */
  block?: string;

  /** @format uint64 */
  app?: string;
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
 * @title cosmos/base/tendermint/v1beta1/query.proto
 * @version version not set
 */
export class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
  /**
   * No description
   *
   * @tags Service
   * @name ServiceGetLatestBlock
   * @summary GetLatestBlock returns the latest block.
   * @request GET:/cosmos/base/tendermint/v1beta1/blocks/latest
   */
  serviceGetLatestBlock = (params: RequestParams = {}) =>
    this.request<V1Beta1GetLatestBlockResponse, RpcStatus>({
      path: `/cosmos/base/tendermint/v1beta1/blocks/latest`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Service
   * @name ServiceGetBlockByHeight
   * @summary GetBlockByHeight queries block for given height.
   * @request GET:/cosmos/base/tendermint/v1beta1/blocks/{height}
   */
  serviceGetBlockByHeight = (height: string, params: RequestParams = {}) =>
    this.request<V1Beta1GetBlockByHeightResponse, RpcStatus>({
      path: `/cosmos/base/tendermint/v1beta1/blocks/${height}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Service
   * @name ServiceGetNodeInfo
   * @summary GetNodeInfo queries the current node info.
   * @request GET:/cosmos/base/tendermint/v1beta1/node_info
   */
  serviceGetNodeInfo = (params: RequestParams = {}) =>
    this.request<V1Beta1GetNodeInfoResponse, RpcStatus>({
      path: `/cosmos/base/tendermint/v1beta1/node_info`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Service
   * @name ServiceGetSyncing
   * @summary GetSyncing queries node syncing.
   * @request GET:/cosmos/base/tendermint/v1beta1/syncing
   */
  serviceGetSyncing = (params: RequestParams = {}) =>
    this.request<V1Beta1GetSyncingResponse, RpcStatus>({
      path: `/cosmos/base/tendermint/v1beta1/syncing`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Service
   * @name ServiceGetLatestValidatorSet
   * @summary GetLatestValidatorSet queries latest validator-set.
   * @request GET:/cosmos/base/tendermint/v1beta1/validatorsets/latest
   */
  serviceGetLatestValidatorSet = (
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V1Beta1GetLatestValidatorSetResponse, RpcStatus>({
      path: `/cosmos/base/tendermint/v1beta1/validatorsets/latest`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Service
   * @name ServiceGetValidatorSetByHeight
   * @summary GetValidatorSetByHeight queries validator-set at a given height.
   * @request GET:/cosmos/base/tendermint/v1beta1/validatorsets/{height}
   */
  serviceGetValidatorSetByHeight = (
    height: string,
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V1Beta1GetValidatorSetByHeightResponse, RpcStatus>({
      path: `/cosmos/base/tendermint/v1beta1/validatorsets/${height}`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });
}
