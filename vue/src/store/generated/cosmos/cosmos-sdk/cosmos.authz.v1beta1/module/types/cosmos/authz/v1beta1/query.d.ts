import { Reader, Writer } from "protobufjs/minimal";
import { PageRequest, PageResponse } from "../../../cosmos/base/query/v1beta1/pagination";
import { Grant, GrantAuthorization } from "../../../cosmos/authz/v1beta1/authz";
export declare const protobufPackage = "cosmos.authz.v1beta1";
/** Since: cosmos-sdk 0.43 */
/** QueryGrantsRequest is the request type for the Query/Grants RPC method. */
export interface QueryGrantsRequest {
    granter: string;
    grantee: string;
    /** Optional, msg_type_url, when set, will query only grants matching given msg type. */
    msgTypeUrl: string;
    /** pagination defines an pagination for the request. */
    pagination: PageRequest | undefined;
}
/** QueryGrantsResponse is the response type for the Query/Authorizations RPC method. */
export interface QueryGrantsResponse {
    /** authorizations is a list of grants granted for grantee by granter. */
    grants: Grant[];
    /** pagination defines an pagination for the response. */
    pagination: PageResponse | undefined;
}
/** QueryGranterGrantsRequest is the request type for the Query/GranterGrants RPC method. */
export interface QueryGranterGrantsRequest {
    granter: string;
    /** pagination defines an pagination for the request. */
    pagination: PageRequest | undefined;
}
/** QueryGranterGrantsResponse is the response type for the Query/GranterGrants RPC method. */
export interface QueryGranterGrantsResponse {
    /** grants is a list of grants granted by the granter. */
    grants: GrantAuthorization[];
    /** pagination defines an pagination for the response. */
    pagination: PageResponse | undefined;
}
/** QueryGranteeGrantsRequest is the request type for the Query/IssuedGrants RPC method. */
export interface QueryGranteeGrantsRequest {
    grantee: string;
    /** pagination defines an pagination for the request. */
    pagination: PageRequest | undefined;
}
/** QueryGranteeGrantsResponse is the response type for the Query/GranteeGrants RPC method. */
export interface QueryGranteeGrantsResponse {
    /** grants is a list of grants granted to the grantee. */
    grants: GrantAuthorization[];
    /** pagination defines an pagination for the response. */
    pagination: PageResponse | undefined;
}
export declare const QueryGrantsRequest: {
    encode(message: QueryGrantsRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGrantsRequest;
    fromJSON(object: any): QueryGrantsRequest;
    toJSON(message: QueryGrantsRequest): unknown;
    fromPartial(object: DeepPartial<QueryGrantsRequest>): QueryGrantsRequest;
};
export declare const QueryGrantsResponse: {
    encode(message: QueryGrantsResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGrantsResponse;
    fromJSON(object: any): QueryGrantsResponse;
    toJSON(message: QueryGrantsResponse): unknown;
    fromPartial(object: DeepPartial<QueryGrantsResponse>): QueryGrantsResponse;
};
export declare const QueryGranterGrantsRequest: {
    encode(message: QueryGranterGrantsRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGranterGrantsRequest;
    fromJSON(object: any): QueryGranterGrantsRequest;
    toJSON(message: QueryGranterGrantsRequest): unknown;
    fromPartial(object: DeepPartial<QueryGranterGrantsRequest>): QueryGranterGrantsRequest;
};
export declare const QueryGranterGrantsResponse: {
    encode(message: QueryGranterGrantsResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGranterGrantsResponse;
    fromJSON(object: any): QueryGranterGrantsResponse;
    toJSON(message: QueryGranterGrantsResponse): unknown;
    fromPartial(object: DeepPartial<QueryGranterGrantsResponse>): QueryGranterGrantsResponse;
};
export declare const QueryGranteeGrantsRequest: {
    encode(message: QueryGranteeGrantsRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGranteeGrantsRequest;
    fromJSON(object: any): QueryGranteeGrantsRequest;
    toJSON(message: QueryGranteeGrantsRequest): unknown;
    fromPartial(object: DeepPartial<QueryGranteeGrantsRequest>): QueryGranteeGrantsRequest;
};
export declare const QueryGranteeGrantsResponse: {
    encode(message: QueryGranteeGrantsResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGranteeGrantsResponse;
    fromJSON(object: any): QueryGranteeGrantsResponse;
    toJSON(message: QueryGranteeGrantsResponse): unknown;
    fromPartial(object: DeepPartial<QueryGranteeGrantsResponse>): QueryGranteeGrantsResponse;
};
/** Query defines the gRPC querier service. */
export interface Query {
    /** Returns list of `Authorization`, granted to the grantee by the granter. */
    Grants(request: QueryGrantsRequest): Promise<QueryGrantsResponse>;
    /**
     * GranterGrants returns list of `GrantAuthorization`, granted by granter.
     *
     * Since: cosmos-sdk 0.45.2
     */
    GranterGrants(request: QueryGranterGrantsRequest): Promise<QueryGranterGrantsResponse>;
    /**
     * GranteeGrants returns a list of `GrantAuthorization` by grantee.
     *
     * Since: cosmos-sdk 0.45.2
     */
    GranteeGrants(request: QueryGranteeGrantsRequest): Promise<QueryGranteeGrantsResponse>;
}
export declare class QueryClientImpl implements Query {
    private readonly rpc;
    constructor(rpc: Rpc);
    Grants(request: QueryGrantsRequest): Promise<QueryGrantsResponse>;
    GranterGrants(request: QueryGranterGrantsRequest): Promise<QueryGranterGrantsResponse>;
    GranteeGrants(request: QueryGranteeGrantsRequest): Promise<QueryGranteeGrantsResponse>;
}
interface Rpc {
    request(service: string, method: string, data: Uint8Array): Promise<Uint8Array>;
}
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
