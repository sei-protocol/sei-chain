import { Reader, Writer } from "protobufjs/minimal";
import { Grant } from "../../../cosmos/feegrant/v1beta1/feegrant";
import { PageRequest, PageResponse } from "../../../cosmos/base/query/v1beta1/pagination";
export declare const protobufPackage = "cosmos.feegrant.v1beta1";
/** Since: cosmos-sdk 0.43 */
/** QueryAllowanceRequest is the request type for the Query/Allowance RPC method. */
export interface QueryAllowanceRequest {
    /** granter is the address of the user granting an allowance of their funds. */
    granter: string;
    /** grantee is the address of the user being granted an allowance of another user's funds. */
    grantee: string;
}
/** QueryAllowanceResponse is the response type for the Query/Allowance RPC method. */
export interface QueryAllowanceResponse {
    /** allowance is a allowance granted for grantee by granter. */
    allowance: Grant | undefined;
}
/** QueryAllowancesRequest is the request type for the Query/Allowances RPC method. */
export interface QueryAllowancesRequest {
    grantee: string;
    /** pagination defines an pagination for the request. */
    pagination: PageRequest | undefined;
}
/** QueryAllowancesResponse is the response type for the Query/Allowances RPC method. */
export interface QueryAllowancesResponse {
    /** allowances are allowance's granted for grantee by granter. */
    allowances: Grant[];
    /** pagination defines an pagination for the response. */
    pagination: PageResponse | undefined;
}
export declare const QueryAllowanceRequest: {
    encode(message: QueryAllowanceRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryAllowanceRequest;
    fromJSON(object: any): QueryAllowanceRequest;
    toJSON(message: QueryAllowanceRequest): unknown;
    fromPartial(object: DeepPartial<QueryAllowanceRequest>): QueryAllowanceRequest;
};
export declare const QueryAllowanceResponse: {
    encode(message: QueryAllowanceResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryAllowanceResponse;
    fromJSON(object: any): QueryAllowanceResponse;
    toJSON(message: QueryAllowanceResponse): unknown;
    fromPartial(object: DeepPartial<QueryAllowanceResponse>): QueryAllowanceResponse;
};
export declare const QueryAllowancesRequest: {
    encode(message: QueryAllowancesRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryAllowancesRequest;
    fromJSON(object: any): QueryAllowancesRequest;
    toJSON(message: QueryAllowancesRequest): unknown;
    fromPartial(object: DeepPartial<QueryAllowancesRequest>): QueryAllowancesRequest;
};
export declare const QueryAllowancesResponse: {
    encode(message: QueryAllowancesResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryAllowancesResponse;
    fromJSON(object: any): QueryAllowancesResponse;
    toJSON(message: QueryAllowancesResponse): unknown;
    fromPartial(object: DeepPartial<QueryAllowancesResponse>): QueryAllowancesResponse;
};
/** Query defines the gRPC querier service. */
export interface Query {
    /** Allowance returns fee granted to the grantee by the granter. */
    Allowance(request: QueryAllowanceRequest): Promise<QueryAllowanceResponse>;
    /** Allowances returns all the grants for address. */
    Allowances(request: QueryAllowancesRequest): Promise<QueryAllowancesResponse>;
}
export declare class QueryClientImpl implements Query {
    private readonly rpc;
    constructor(rpc: Rpc);
    Allowance(request: QueryAllowanceRequest): Promise<QueryAllowanceResponse>;
    Allowances(request: QueryAllowancesRequest): Promise<QueryAllowancesResponse>;
}
interface Rpc {
    request(service: string, method: string, data: Uint8Array): Promise<Uint8Array>;
}
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
