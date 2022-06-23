export interface ProtobufAny {
    "@type"?: string;
}
export interface RpcStatus {
    /** @format int32 */
    code?: number;
    message?: string;
    details?: ProtobufAny[];
}
export declare type V1Beta1MsgUnjailResponse = object;
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
 * Params represents the parameters used for by the slashing module.
 */
export interface V1Beta1Params {
    /** @format int64 */
    signedBlocksWindow?: string;
    /** @format byte */
    minSignedPerWindow?: string;
    downtimeJailDuration?: string;
    /** @format byte */
    slashFractionDoubleSign?: string;
    /** @format byte */
    slashFractionDowntime?: string;
}
export interface V1Beta1QueryParamsResponse {
    /** Params represents the parameters used for by the slashing module. */
    params?: V1Beta1Params;
}
export interface V1Beta1QuerySigningInfoResponse {
    /**
     * ValidatorSigningInfo defines a validator's signing info for monitoring their
     * liveness activity.
     */
    valSigningInfo?: V1Beta1ValidatorSigningInfo;
}
export interface V1Beta1QuerySigningInfosResponse {
    info?: V1Beta1ValidatorSigningInfo[];
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
/**
* ValidatorSigningInfo defines a validator's signing info for monitoring their
liveness activity.
*/
export interface V1Beta1ValidatorSigningInfo {
    address?: string;
    /** @format int64 */
    startHeight?: string;
    /**
     * Index which is incremented each time the validator was a bonded
     * in a block and may have signed a precommit or not. This in conjunction with the
     * `SignedBlocksWindow` param determines the index in the `MissedBlocksBitArray`.
     * @format int64
     */
    indexOffset?: string;
    /**
     * Timestamp until which the validator is jailed due to liveness downtime.
     * @format date-time
     */
    jailedUntil?: string;
    /**
     * Whether or not a validator has been tombstoned (killed out of validator set). It is set
     * once the validator commits an equivocation or for any other configured misbehiavor.
     */
    tombstoned?: boolean;
    /**
     * A counter kept to avoid unnecessary array reads.
     * Note that `Sum(MissedBlocksBitArray)` always equals `MissedBlocksCounter`.
     * @format int64
     */
    missedBlocksCounter?: string;
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
 * @title cosmos/slashing/v1beta1/genesis.proto
 * @version version not set
 */
export declare class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
    /**
     * No description
     *
     * @tags Query
     * @name QueryParams
     * @summary Params queries the parameters of slashing module
     * @request GET:/cosmos/slashing/v1beta1/params
     */
    queryParams: (params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryParamsResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QuerySigningInfos
     * @summary SigningInfos queries signing info of all validators
     * @request GET:/cosmos/slashing/v1beta1/signing_infos
     */
    querySigningInfos: (query?: {
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<V1Beta1QuerySigningInfosResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QuerySigningInfo
     * @summary SigningInfo queries the signing info of given cons address
     * @request GET:/cosmos/slashing/v1beta1/signing_infos/{consAddress}
     */
    querySigningInfo: (consAddress: string, params?: RequestParams) => Promise<HttpResponse<V1Beta1QuerySigningInfoResponse, RpcStatus>>;
}
export {};
