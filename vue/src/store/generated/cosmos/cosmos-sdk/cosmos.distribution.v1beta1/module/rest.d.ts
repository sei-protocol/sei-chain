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
* Coin defines a token with a denomination and an amount.

NOTE: The amount field is an Int which implements the custom method
signatures required by gogoproto.
*/
export interface V1Beta1Coin {
    denom?: string;
    amount?: string;
}
/**
* DecCoin defines a token with a denomination and a decimal amount.

NOTE: The amount field is an Dec which implements the custom method
signatures required by gogoproto.
*/
export interface V1Beta1DecCoin {
    denom?: string;
    amount?: string;
}
/**
* DelegationDelegatorReward represents the properties
of a delegator's delegation reward.
*/
export interface V1Beta1DelegationDelegatorReward {
    validatorAddress?: string;
    reward?: V1Beta1DecCoin[];
}
/**
 * MsgFundCommunityPoolResponse defines the Msg/FundCommunityPool response type.
 */
export declare type V1Beta1MsgFundCommunityPoolResponse = object;
/**
 * MsgSetWithdrawAddressResponse defines the Msg/SetWithdrawAddress response type.
 */
export declare type V1Beta1MsgSetWithdrawAddressResponse = object;
/**
 * MsgWithdrawDelegatorRewardResponse defines the Msg/WithdrawDelegatorReward response type.
 */
export declare type V1Beta1MsgWithdrawDelegatorRewardResponse = object;
/**
 * MsgWithdrawValidatorCommissionResponse defines the Msg/WithdrawValidatorCommission response type.
 */
export declare type V1Beta1MsgWithdrawValidatorCommissionResponse = object;
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
    countTotal?: boolean;
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
    nextKey?: string;
    /** @format uint64 */
    total?: string;
}
/**
 * Params defines the set of params for the distribution module.
 */
export interface V1Beta1Params {
    communityTax?: string;
    baseProposerReward?: string;
    bonusProposerReward?: string;
    withdrawAddrEnabled?: boolean;
}
/**
* QueryCommunityPoolResponse is the response type for the Query/CommunityPool
RPC method.
*/
export interface V1Beta1QueryCommunityPoolResponse {
    /** pool defines community pool's coins. */
    pool?: V1Beta1DecCoin[];
}
/**
* QueryDelegationRewardsResponse is the response type for the
Query/DelegationRewards RPC method.
*/
export interface V1Beta1QueryDelegationRewardsResponse {
    /** rewards defines the rewards accrued by a delegation. */
    rewards?: V1Beta1DecCoin[];
}
/**
* QueryDelegationTotalRewardsResponse is the response type for the
Query/DelegationTotalRewards RPC method.
*/
export interface V1Beta1QueryDelegationTotalRewardsResponse {
    /** rewards defines all the rewards accrued by a delegator. */
    rewards?: V1Beta1DelegationDelegatorReward[];
    /** total defines the sum of all the rewards. */
    total?: V1Beta1DecCoin[];
}
/**
* QueryDelegatorValidatorsResponse is the response type for the
Query/DelegatorValidators RPC method.
*/
export interface V1Beta1QueryDelegatorValidatorsResponse {
    /** validators defines the validators a delegator is delegating for. */
    validators?: string[];
}
/**
* QueryDelegatorWithdrawAddressResponse is the response type for the
Query/DelegatorWithdrawAddress RPC method.
*/
export interface V1Beta1QueryDelegatorWithdrawAddressResponse {
    /** withdraw_address defines the delegator address to query for. */
    withdrawAddress?: string;
}
/**
 * QueryParamsResponse is the response type for the Query/Params RPC method.
 */
export interface V1Beta1QueryParamsResponse {
    /** params defines the parameters of the module. */
    params?: V1Beta1Params;
}
export interface V1Beta1QueryValidatorCommissionResponse {
    /** commission defines the commision the validator received. */
    commission?: V1Beta1ValidatorAccumulatedCommission;
}
/**
* QueryValidatorOutstandingRewardsResponse is the response type for the
Query/ValidatorOutstandingRewards RPC method.
*/
export interface V1Beta1QueryValidatorOutstandingRewardsResponse {
    /**
     * ValidatorOutstandingRewards represents outstanding (un-withdrawn) rewards
     * for a validator inexpensive to track, allows simple sanity checks.
     */
    rewards?: V1Beta1ValidatorOutstandingRewards;
}
/**
* QueryValidatorSlashesResponse is the response type for the
Query/ValidatorSlashes RPC method.
*/
export interface V1Beta1QueryValidatorSlashesResponse {
    /** slashes defines the slashes the validator received. */
    slashes?: V1Beta1ValidatorSlashEvent[];
    /** pagination defines the pagination in the response. */
    pagination?: V1Beta1PageResponse;
}
/**
* ValidatorAccumulatedCommission represents accumulated commission
for a validator kept as a running counter, can be withdrawn at any time.
*/
export interface V1Beta1ValidatorAccumulatedCommission {
    commission?: V1Beta1DecCoin[];
}
/**
* ValidatorOutstandingRewards represents outstanding (un-withdrawn) rewards
for a validator inexpensive to track, allows simple sanity checks.
*/
export interface V1Beta1ValidatorOutstandingRewards {
    rewards?: V1Beta1DecCoin[];
}
/**
* ValidatorSlashEvent represents a validator slash event.
Height is implicit within the store key.
This is needed to calculate appropriate amount of staking tokens
for delegations which are withdrawn after a slash has occurred.
*/
export interface V1Beta1ValidatorSlashEvent {
    /** @format uint64 */
    validatorPeriod?: string;
    fraction?: string;
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
 * @title cosmos/distribution/v1beta1/distribution.proto
 * @version version not set
 */
export declare class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
    /**
     * No description
     *
     * @tags Query
     * @name QueryCommunityPool
     * @summary CommunityPool queries the community pool coins.
     * @request GET:/cosmos/distribution/v1beta1/community_pool
     */
    queryCommunityPool: (params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryCommunityPoolResponse, RpcStatus>>;
    /**
   * No description
   *
   * @tags Query
   * @name QueryDelegationTotalRewards
   * @summary DelegationTotalRewards queries the total rewards accrued by a each
  validator.
   * @request GET:/cosmos/distribution/v1beta1/delegators/{delegatorAddress}/rewards
   */
    queryDelegationTotalRewards: (delegatorAddress: string, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryDelegationTotalRewardsResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryDelegationRewards
     * @summary DelegationRewards queries the total rewards accrued by a delegation.
     * @request GET:/cosmos/distribution/v1beta1/delegators/{delegatorAddress}/rewards/{validatorAddress}
     */
    queryDelegationRewards: (delegatorAddress: string, validatorAddress: string, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryDelegationRewardsResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryDelegatorValidators
     * @summary DelegatorValidators queries the validators of a delegator.
     * @request GET:/cosmos/distribution/v1beta1/delegators/{delegatorAddress}/validators
     */
    queryDelegatorValidators: (delegatorAddress: string, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryDelegatorValidatorsResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryDelegatorWithdrawAddress
     * @summary DelegatorWithdrawAddress queries withdraw address of a delegator.
     * @request GET:/cosmos/distribution/v1beta1/delegators/{delegatorAddress}/withdraw_address
     */
    queryDelegatorWithdrawAddress: (delegatorAddress: string, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryDelegatorWithdrawAddressResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryParams
     * @summary Params queries params of the distribution module.
     * @request GET:/cosmos/distribution/v1beta1/params
     */
    queryParams: (params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryParamsResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryValidatorCommission
     * @summary ValidatorCommission queries accumulated commission for a validator.
     * @request GET:/cosmos/distribution/v1beta1/validators/{validatorAddress}/commission
     */
    queryValidatorCommission: (validatorAddress: string, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryValidatorCommissionResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryValidatorOutstandingRewards
     * @summary ValidatorOutstandingRewards queries rewards of a validator address.
     * @request GET:/cosmos/distribution/v1beta1/validators/{validatorAddress}/outstanding_rewards
     */
    queryValidatorOutstandingRewards: (validatorAddress: string, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryValidatorOutstandingRewardsResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryValidatorSlashes
     * @summary ValidatorSlashes queries slash events of a validator.
     * @request GET:/cosmos/distribution/v1beta1/validators/{validatorAddress}/slashes
     */
    queryValidatorSlashes: (validatorAddress: string, query?: {
        startingHeight?: string;
        endingHeight?: string;
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryValidatorSlashesResponse, RpcStatus>>;
}
export {};
