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
* Validator defines a validator, together with the total amount of the
Validator's bond shares and their exchange rate to coins. Slashing results in
a decrease in the exchange rate, allowing correct calculation of future
undelegations without iterating over delegators. When coins are delegated to
this validator, the validator is credited with a delegation whose number of
bond shares is based on the amount of coins delegated divided by the current
exchange rate. Voting power can be calculated as total bonded shares
multiplied by exchange rate.
*/
export interface Stakingv1Beta1Validator {
  /** operator_address defines the address of the validator's operator; bech encoded in JSON. */
  operator_address?: string;

  /** consensus_pubkey is the consensus public key of the validator, as a Protobuf Any. */
  consensus_pubkey?: ProtobufAny;

  /** jailed defined whether the validator has been jailed from bonded status or not. */
  jailed?: boolean;

  /** status is the validator status (bonded/unbonding/unbonded). */
  status?: V1Beta1BondStatus;

  /** tokens define the delegated tokens (incl. self-delegation). */
  tokens?: string;

  /** delegator_shares defines total shares issued to a validator's delegators. */
  delegator_shares?: string;

  /** description defines the description terms for the validator. */
  description?: V1Beta1Description;

  /**
   * unbonding_height defines, if unbonding, the height at which this validator has begun unbonding.
   * @format int64
   */
  unbonding_height?: string;

  /**
   * unbonding_time defines, if unbonding, the min time for the validator to complete unbonding.
   * @format date-time
   */
  unbonding_time?: string;

  /** commission defines the commission parameters. */
  commission?: V1Beta1Commission;

  /** min_self_delegation is the validator's self declared minimum self delegation. */
  min_self_delegation?: string;
}

export interface TypesBlockID {
  /** @format byte */
  hash?: string;
  part_set_header?: TypesPartSetHeader;
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

export interface TypesPartSetHeader {
  /** @format int64 */
  total?: number;

  /** @format byte */
  hash?: string;
}

/**
* BondStatus is the status of a validator.

 - BOND_STATUS_UNSPECIFIED: UNSPECIFIED defines an invalid validator status.
 - BOND_STATUS_UNBONDED: UNBONDED defines a validator that is not bonded.
 - BOND_STATUS_UNBONDING: UNBONDING defines a validator that is unbonding.
 - BOND_STATUS_BONDED: BONDED defines a validator that is bonded.
*/
export enum V1Beta1BondStatus {
  BOND_STATUS_UNSPECIFIED = "BOND_STATUS_UNSPECIFIED",
  BOND_STATUS_UNBONDED = "BOND_STATUS_UNBONDED",
  BOND_STATUS_UNBONDING = "BOND_STATUS_UNBONDING",
  BOND_STATUS_BONDED = "BOND_STATUS_BONDED",
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
 * Commission defines commission parameters for a given validator.
 */
export interface V1Beta1Commission {
  /** commission_rates defines the initial commission rates to be used for creating a validator. */
  commission_rates?: V1Beta1CommissionRates;

  /**
   * update_time is the last time the commission rate was changed.
   * @format date-time
   */
  update_time?: string;
}

/**
* CommissionRates defines the initial commission rates to be used for creating
a validator.
*/
export interface V1Beta1CommissionRates {
  /** rate is the commission rate charged to delegators, as a fraction. */
  rate?: string;

  /** max_rate defines the maximum commission rate which validator can ever charge, as a fraction. */
  max_rate?: string;

  /** max_change_rate defines the maximum daily increase of the validator commission, as a fraction. */
  max_change_rate?: string;
}

/**
* Delegation represents the bond with tokens held by an account. It is
owned by one delegator, and is associated with the voting power of one
validator.
*/
export interface V1Beta1Delegation {
  /** delegator_address is the bech32-encoded address of the delegator. */
  delegator_address?: string;

  /** validator_address is the bech32-encoded address of the validator. */
  validator_address?: string;

  /** shares define the delegation shares received. */
  shares?: string;
}

/**
* DelegationResponse is equivalent to Delegation except that it contains a
balance in addition to shares which is more suitable for client responses.
*/
export interface V1Beta1DelegationResponse {
  /**
   * Delegation represents the bond with tokens held by an account. It is
   * owned by one delegator, and is associated with the voting power of one
   * validator.
   */
  delegation?: V1Beta1Delegation;

  /**
   * Coin defines a token with a denomination and an amount.
   *
   * NOTE: The amount field is an Int which implements the custom method
   * signatures required by gogoproto.
   */
  balance?: V1Beta1Coin;
}

/**
 * Description defines a validator description.
 */
export interface V1Beta1Description {
  /** moniker defines a human-readable name for the validator. */
  moniker?: string;

  /** identity defines an optional identity signature (ex. UPort or Keybase). */
  identity?: string;

  /** website defines an optional website link. */
  website?: string;

  /** security_contact defines an optional email for security contact. */
  security_contact?: string;

  /** details define other optional details. */
  details?: string;
}

/**
* HistoricalInfo contains header and validator information for a given block.
It is stored as part of staking module's state, which persists the `n` most
recent HistoricalInfo
(`n` is set by the staking module's `historical_entries` parameter).
*/
export interface V1Beta1HistoricalInfo {
  /** Header defines the structure of a Tendermint block header. */
  header?: TypesHeader;
  valset?: Stakingv1Beta1Validator[];
}

/**
 * MsgBeginRedelegateResponse defines the Msg/BeginRedelegate response type.
 */
export interface V1Beta1MsgBeginRedelegateResponse {
  /** @format date-time */
  completion_time?: string;
}

/**
 * MsgCreateValidatorResponse defines the Msg/CreateValidator response type.
 */
export type V1Beta1MsgCreateValidatorResponse = object;

/**
 * MsgDelegateResponse defines the Msg/Delegate response type.
 */
export type V1Beta1MsgDelegateResponse = object;

/**
 * MsgEditValidatorResponse defines the Msg/EditValidator response type.
 */
export type V1Beta1MsgEditValidatorResponse = object;

/**
 * MsgUndelegateResponse defines the Msg/Undelegate response type.
 */
export interface V1Beta1MsgUndelegateResponse {
  /** @format date-time */
  completion_time?: string;
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
 * Params defines the parameters for the staking module.
 */
export interface V1Beta1Params {
  /** unbonding_time is the time duration of unbonding. */
  unbonding_time?: string;

  /**
   * max_validators is the maximum number of validators.
   * @format int64
   */
  max_validators?: number;

  /**
   * max_entries is the max entries for either unbonding delegation or redelegation (per pair/trio).
   * @format int64
   */
  max_entries?: number;

  /**
   * historical_entries is the number of historical entries to persist.
   * @format int64
   */
  historical_entries?: number;

  /** bond_denom defines the bondable coin denomination. */
  bond_denom?: string;
}

/**
* Pool is used for tracking bonded and not-bonded token supply of the bond
denomination.
*/
export interface V1Beta1Pool {
  not_bonded_tokens?: string;
  bonded_tokens?: string;
}

/**
 * QueryDelegationResponse is response type for the Query/Delegation RPC method.
 */
export interface V1Beta1QueryDelegationResponse {
  /** delegation_responses defines the delegation info of a delegation. */
  delegation_response?: V1Beta1DelegationResponse;
}

/**
* QueryDelegatorDelegationsResponse is response type for the
Query/DelegatorDelegations RPC method.
*/
export interface V1Beta1QueryDelegatorDelegationsResponse {
  /** delegation_responses defines all the delegations' info of a delegator. */
  delegation_responses?: V1Beta1DelegationResponse[];

  /** pagination defines the pagination in the response. */
  pagination?: V1Beta1PageResponse;
}

/**
* QueryUnbondingDelegatorDelegationsResponse is response type for the
Query/UnbondingDelegatorDelegations RPC method.
*/
export interface V1Beta1QueryDelegatorUnbondingDelegationsResponse {
  unbonding_responses?: V1Beta1UnbondingDelegation[];

  /** pagination defines the pagination in the response. */
  pagination?: V1Beta1PageResponse;
}

/**
* QueryDelegatorValidatorResponse response type for the
Query/DelegatorValidator RPC method.
*/
export interface V1Beta1QueryDelegatorValidatorResponse {
  /** validator defines the the validator info. */
  validator?: Stakingv1Beta1Validator;
}

/**
* QueryDelegatorValidatorsResponse is response type for the
Query/DelegatorValidators RPC method.
*/
export interface V1Beta1QueryDelegatorValidatorsResponse {
  /** validators defines the the validators' info of a delegator. */
  validators?: Stakingv1Beta1Validator[];

  /** pagination defines the pagination in the response. */
  pagination?: V1Beta1PageResponse;
}

/**
* QueryHistoricalInfoResponse is response type for the Query/HistoricalInfo RPC
method.
*/
export interface V1Beta1QueryHistoricalInfoResponse {
  /** hist defines the historical info at the given height. */
  hist?: V1Beta1HistoricalInfo;
}

/**
 * QueryParamsResponse is response type for the Query/Params RPC method.
 */
export interface V1Beta1QueryParamsResponse {
  /** params holds all the parameters of this module. */
  params?: V1Beta1Params;
}

/**
 * QueryPoolResponse is response type for the Query/Pool RPC method.
 */
export interface V1Beta1QueryPoolResponse {
  /** pool defines the pool info. */
  pool?: V1Beta1Pool;
}

/**
* QueryRedelegationsResponse is response type for the Query/Redelegations RPC
method.
*/
export interface V1Beta1QueryRedelegationsResponse {
  redelegation_responses?: V1Beta1RedelegationResponse[];

  /** pagination defines the pagination in the response. */
  pagination?: V1Beta1PageResponse;
}

/**
* QueryDelegationResponse is response type for the Query/UnbondingDelegation
RPC method.
*/
export interface V1Beta1QueryUnbondingDelegationResponse {
  /** unbond defines the unbonding information of a delegation. */
  unbond?: V1Beta1UnbondingDelegation;
}

export interface V1Beta1QueryValidatorDelegationsResponse {
  delegation_responses?: V1Beta1DelegationResponse[];

  /** pagination defines the pagination in the response. */
  pagination?: V1Beta1PageResponse;
}

export interface V1Beta1QueryValidatorResponse {
  /** validator defines the the validator info. */
  validator?: Stakingv1Beta1Validator;
}

/**
* QueryValidatorUnbondingDelegationsResponse is response type for the
Query/ValidatorUnbondingDelegations RPC method.
*/
export interface V1Beta1QueryValidatorUnbondingDelegationsResponse {
  unbonding_responses?: V1Beta1UnbondingDelegation[];

  /** pagination defines the pagination in the response. */
  pagination?: V1Beta1PageResponse;
}

export interface V1Beta1QueryValidatorsResponse {
  /** validators contains all the queried validators. */
  validators?: Stakingv1Beta1Validator[];

  /** pagination defines the pagination in the response. */
  pagination?: V1Beta1PageResponse;
}

/**
* Redelegation contains the list of a particular delegator's redelegating bonds
from a particular source validator to a particular destination validator.
*/
export interface V1Beta1Redelegation {
  /** delegator_address is the bech32-encoded address of the delegator. */
  delegator_address?: string;

  /** validator_src_address is the validator redelegation source operator address. */
  validator_src_address?: string;

  /** validator_dst_address is the validator redelegation destination operator address. */
  validator_dst_address?: string;

  /** entries are the redelegation entries. */
  entries?: V1Beta1RedelegationEntry[];
}

/**
 * RedelegationEntry defines a redelegation object with relevant metadata.
 */
export interface V1Beta1RedelegationEntry {
  /**
   * creation_height  defines the height which the redelegation took place.
   * @format int64
   */
  creation_height?: string;

  /**
   * completion_time defines the unix time for redelegation completion.
   * @format date-time
   */
  completion_time?: string;

  /** initial_balance defines the initial balance when redelegation started. */
  initial_balance?: string;

  /** shares_dst is the amount of destination-validator shares created by redelegation. */
  shares_dst?: string;
}

/**
* RedelegationEntryResponse is equivalent to a RedelegationEntry except that it
contains a balance in addition to shares which is more suitable for client
responses.
*/
export interface V1Beta1RedelegationEntryResponse {
  /** RedelegationEntry defines a redelegation object with relevant metadata. */
  redelegation_entry?: V1Beta1RedelegationEntry;
  balance?: string;
}

/**
* RedelegationResponse is equivalent to a Redelegation except that its entries
contain a balance in addition to shares which is more suitable for client
responses.
*/
export interface V1Beta1RedelegationResponse {
  /**
   * Redelegation contains the list of a particular delegator's redelegating bonds
   * from a particular source validator to a particular destination validator.
   */
  redelegation?: V1Beta1Redelegation;
  entries?: V1Beta1RedelegationEntryResponse[];
}

/**
* UnbondingDelegation stores all of a single delegator's unbonding bonds
for a single validator in an time-ordered list.
*/
export interface V1Beta1UnbondingDelegation {
  /** delegator_address is the bech32-encoded address of the delegator. */
  delegator_address?: string;

  /** validator_address is the bech32-encoded address of the validator. */
  validator_address?: string;

  /** entries are the unbonding delegation entries. */
  entries?: V1Beta1UnbondingDelegationEntry[];
}

/**
 * UnbondingDelegationEntry defines an unbonding object with relevant metadata.
 */
export interface V1Beta1UnbondingDelegationEntry {
  /**
   * creation_height is the height which the unbonding took place.
   * @format int64
   */
  creation_height?: string;

  /**
   * completion_time is the unix time for unbonding completion.
   * @format date-time
   */
  completion_time?: string;

  /** initial_balance defines the tokens initially scheduled to receive at completion. */
  initial_balance?: string;

  /** balance defines the tokens to receive at completion. */
  balance?: string;
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
 * @title cosmos/staking/v1beta1/authz.proto
 * @version version not set
 */
export class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
  /**
   * No description
   *
   * @tags Query
   * @name QueryDelegatorDelegations
   * @summary DelegatorDelegations queries all delegations of a given delegator address.
   * @request GET:/cosmos/staking/v1beta1/delegations/{delegator_addr}
   */
  queryDelegatorDelegations = (
    delegator_addr: string,
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V1Beta1QueryDelegatorDelegationsResponse, RpcStatus>({
      path: `/cosmos/staking/v1beta1/delegations/${delegator_addr}`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryRedelegations
   * @summary Redelegations queries redelegations of given address.
   * @request GET:/cosmos/staking/v1beta1/delegators/{delegator_addr}/redelegations
   */
  queryRedelegations = (
    delegator_addr: string,
    query?: {
      src_validator_addr?: string;
      dst_validator_addr?: string;
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V1Beta1QueryRedelegationsResponse, RpcStatus>({
      path: `/cosmos/staking/v1beta1/delegators/${delegator_addr}/redelegations`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
 * No description
 * 
 * @tags Query
 * @name QueryDelegatorUnbondingDelegations
 * @summary DelegatorUnbondingDelegations queries all unbonding delegations of a given
delegator address.
 * @request GET:/cosmos/staking/v1beta1/delegators/{delegator_addr}/unbonding_delegations
 */
  queryDelegatorUnbondingDelegations = (
    delegator_addr: string,
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V1Beta1QueryDelegatorUnbondingDelegationsResponse, RpcStatus>({
      path: `/cosmos/staking/v1beta1/delegators/${delegator_addr}/unbonding_delegations`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
 * No description
 * 
 * @tags Query
 * @name QueryDelegatorValidators
 * @summary DelegatorValidators queries all validators info for given delegator
address.
 * @request GET:/cosmos/staking/v1beta1/delegators/{delegator_addr}/validators
 */
  queryDelegatorValidators = (
    delegator_addr: string,
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V1Beta1QueryDelegatorValidatorsResponse, RpcStatus>({
      path: `/cosmos/staking/v1beta1/delegators/${delegator_addr}/validators`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
 * No description
 * 
 * @tags Query
 * @name QueryDelegatorValidator
 * @summary DelegatorValidator queries validator info for given delegator validator
pair.
 * @request GET:/cosmos/staking/v1beta1/delegators/{delegator_addr}/validators/{validator_addr}
 */
  queryDelegatorValidator = (delegator_addr: string, validator_addr: string, params: RequestParams = {}) =>
    this.request<V1Beta1QueryDelegatorValidatorResponse, RpcStatus>({
      path: `/cosmos/staking/v1beta1/delegators/${delegator_addr}/validators/${validator_addr}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryHistoricalInfo
   * @summary HistoricalInfo queries the historical info for given height.
   * @request GET:/cosmos/staking/v1beta1/historical_info/{height}
   */
  queryHistoricalInfo = (height: string, params: RequestParams = {}) =>
    this.request<V1Beta1QueryHistoricalInfoResponse, RpcStatus>({
      path: `/cosmos/staking/v1beta1/historical_info/${height}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryParams
   * @summary Parameters queries the staking parameters.
   * @request GET:/cosmos/staking/v1beta1/params
   */
  queryParams = (params: RequestParams = {}) =>
    this.request<V1Beta1QueryParamsResponse, RpcStatus>({
      path: `/cosmos/staking/v1beta1/params`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryPool
   * @summary Pool queries the pool info.
   * @request GET:/cosmos/staking/v1beta1/pool
   */
  queryPool = (params: RequestParams = {}) =>
    this.request<V1Beta1QueryPoolResponse, RpcStatus>({
      path: `/cosmos/staking/v1beta1/pool`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryValidators
   * @summary Validators queries all validators that match the given status.
   * @request GET:/cosmos/staking/v1beta1/validators
   */
  queryValidators = (
    query?: {
      status?: string;
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V1Beta1QueryValidatorsResponse, RpcStatus>({
      path: `/cosmos/staking/v1beta1/validators`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryValidator
   * @summary Validator queries validator info for given validator address.
   * @request GET:/cosmos/staking/v1beta1/validators/{validator_addr}
   */
  queryValidator = (validator_addr: string, params: RequestParams = {}) =>
    this.request<V1Beta1QueryValidatorResponse, RpcStatus>({
      path: `/cosmos/staking/v1beta1/validators/${validator_addr}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryValidatorDelegations
   * @summary ValidatorDelegations queries delegate info for given validator.
   * @request GET:/cosmos/staking/v1beta1/validators/{validator_addr}/delegations
   */
  queryValidatorDelegations = (
    validator_addr: string,
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V1Beta1QueryValidatorDelegationsResponse, RpcStatus>({
      path: `/cosmos/staking/v1beta1/validators/${validator_addr}/delegations`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryDelegation
   * @summary Delegation queries delegate info for given validator delegator pair.
   * @request GET:/cosmos/staking/v1beta1/validators/{validator_addr}/delegations/{delegator_addr}
   */
  queryDelegation = (validator_addr: string, delegator_addr: string, params: RequestParams = {}) =>
    this.request<V1Beta1QueryDelegationResponse, RpcStatus>({
      path: `/cosmos/staking/v1beta1/validators/${validator_addr}/delegations/${delegator_addr}`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
 * No description
 * 
 * @tags Query
 * @name QueryUnbondingDelegation
 * @summary UnbondingDelegation queries unbonding info for given validator delegator
pair.
 * @request GET:/cosmos/staking/v1beta1/validators/{validator_addr}/delegations/{delegator_addr}/unbonding_delegation
 */
  queryUnbondingDelegation = (validator_addr: string, delegator_addr: string, params: RequestParams = {}) =>
    this.request<V1Beta1QueryUnbondingDelegationResponse, RpcStatus>({
      path: `/cosmos/staking/v1beta1/validators/${validator_addr}/delegations/${delegator_addr}/unbonding_delegation`,
      method: "GET",
      format: "json",
      ...params,
    });

  /**
   * No description
   *
   * @tags Query
   * @name QueryValidatorUnbondingDelegations
   * @summary ValidatorUnbondingDelegations queries unbonding delegations of a validator.
   * @request GET:/cosmos/staking/v1beta1/validators/{validator_addr}/unbonding_delegations
   */
  queryValidatorUnbondingDelegations = (
    validator_addr: string,
    query?: {
      "pagination.key"?: string;
      "pagination.offset"?: string;
      "pagination.limit"?: string;
      "pagination.count_total"?: boolean;
      "pagination.reverse"?: boolean;
    },
    params: RequestParams = {},
  ) =>
    this.request<V1Beta1QueryValidatorUnbondingDelegationsResponse, RpcStatus>({
      path: `/cosmos/staking/v1beta1/validators/${validator_addr}/unbonding_delegations`,
      method: "GET",
      query: query,
      format: "json",
      ...params,
    });
}
