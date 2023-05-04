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
export interface TokenfactoryDenomAuthorityMetadata {
    admin?: string;
}
export declare type TokenfactoryMsgBurnResponse = object;
/**
* MsgChangeAdminResponse defines the response structure for an executed
MsgChangeAdmin message.
*/
export declare type TokenfactoryMsgChangeAdminResponse = object;
export interface TokenfactoryMsgCreateDenomResponse {
    newTokenDenom?: string;
}
export declare type TokenfactoryMsgMintResponse = object;
/**
 * Params defines the parameters for the tokenfactory module.
 */
export interface TokenfactoryParams {
    denomCreationFee?: V1Beta1Coin[];
}
/**
* QueryCreatorInDenomFeeWhitelistResponse defines the response structure for the
CreatorInDenomFeeWhitelist gRPC query.
*/
export interface TokenfactoryQueryCreatorInDenomFeeWhitelistResponse {
    whitelisted?: boolean;
}
/**
* QueryDenomAuthorityMetadataResponse defines the response structure for the
DenomAuthorityMetadata gRPC query.
*/
export interface TokenfactoryQueryDenomAuthorityMetadataResponse {
    /**
     * DenomAuthorityMetadata specifies metadata for addresses that have specific
     * capabilities over a token factory denom. Right now there is only one Admin
     * permission, but is planned to be extended to the future.
     */
    authorityMetadata?: TokenfactoryDenomAuthorityMetadata;
}
/**
* QueryDenomCreationFeeWhitelistResponse defines the response structure for the
DenomsFromCreator gRPC query.
*/
export interface TokenfactoryQueryDenomCreationFeeWhitelistResponse {
    creators?: string[];
}
/**
* QueryDenomsFromCreatorRequest defines the response structure for the
DenomsFromCreator gRPC query.
*/
export interface TokenfactoryQueryDenomsFromCreatorResponse {
    denoms?: string[];
}
/**
 * QueryParamsResponse is the response type for the Query/Params RPC method.
 */
export interface TokenfactoryQueryParamsResponse {
    /** params defines the parameters of the module. */
    params?: TokenfactoryParams;
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
 * @title tokenfactory/authorityMetadata.proto
 * @version version not set
 */
export declare class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
    /**
   * No description
   *
   * @tags Query
   * @name QueryDenomCreationFeeWhitelist
   * @summary DenomCreationFeeWhitelist defines a gRPC query method for fetching all
  creators who are whitelisted from paying the denom creation fee.
   * @request GET:/sei-protocol/seichain/tokenfactory/denom_creation_fee_whitelist
   */
    queryDenomCreationFeeWhitelist: (params?: RequestParams) => Promise<HttpResponse<TokenfactoryQueryDenomCreationFeeWhitelistResponse, RpcStatus>>;
    /**
   * No description
   *
   * @tags Query
   * @name QueryCreatorInDenomFeeWhitelist
   * @summary CreatorInDenomFeeWhitelist defines a gRPC query method for fetching
  whether a creator is whitelisted from denom creation fees.
   * @request GET:/sei-protocol/seichain/tokenfactory/denom_creation_fee_whitelist/{creator}
   */
    queryCreatorInDenomFeeWhitelist: (creator: string, params?: RequestParams) => Promise<HttpResponse<TokenfactoryQueryCreatorInDenomFeeWhitelistResponse, RpcStatus>>;
    /**
   * No description
   *
   * @tags Query
   * @name QueryDenomAuthorityMetadata
   * @summary DenomAuthorityMetadata defines a gRPC query method for fetching
  DenomAuthorityMetadata for a particular denom.
   * @request GET:/sei-protocol/seichain/tokenfactory/denoms/{denom}/authority_metadata
   */
    queryDenomAuthorityMetadata: (denom: string, params?: RequestParams) => Promise<HttpResponse<TokenfactoryQueryDenomAuthorityMetadataResponse, RpcStatus>>;
    /**
   * No description
   *
   * @tags Query
   * @name QueryDenomsFromCreator
   * @summary DenomsFromCreator defines a gRPC query method for fetching all
  denominations created by a specific admin/creator.
   * @request GET:/sei-protocol/seichain/tokenfactory/denoms_from_creator/{creator}
   */
    queryDenomsFromCreator: (creator: string, params?: RequestParams) => Promise<HttpResponse<TokenfactoryQueryDenomsFromCreatorResponse, RpcStatus>>;
    /**
   * No description
   *
   * @tags Query
   * @name QueryParams
   * @summary Params defines a gRPC query method that returns the tokenfactory module's
  parameters.
   * @request GET:/sei-protocol/seichain/tokenfactory/params
   */
    queryParams: (params?: RequestParams) => Promise<HttpResponse<TokenfactoryQueryParamsResponse, RpcStatus>>;
}
export {};
