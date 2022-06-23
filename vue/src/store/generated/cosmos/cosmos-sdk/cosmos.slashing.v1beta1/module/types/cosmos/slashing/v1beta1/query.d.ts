import { Reader, Writer } from "protobufjs/minimal";
import { Params, ValidatorSigningInfo } from "../../../cosmos/slashing/v1beta1/slashing";
import { PageRequest, PageResponse } from "../../../cosmos/base/query/v1beta1/pagination";
export declare const protobufPackage = "cosmos.slashing.v1beta1";
/** QueryParamsRequest is the request type for the Query/Params RPC method */
export interface QueryParamsRequest {
}
/** QueryParamsResponse is the response type for the Query/Params RPC method */
export interface QueryParamsResponse {
    params: Params | undefined;
}
/**
 * QuerySigningInfoRequest is the request type for the Query/SigningInfo RPC
 * method
 */
export interface QuerySigningInfoRequest {
    /** cons_address is the address to query signing info of */
    consAddress: string;
}
/**
 * QuerySigningInfoResponse is the response type for the Query/SigningInfo RPC
 * method
 */
export interface QuerySigningInfoResponse {
    /** val_signing_info is the signing info of requested val cons address */
    valSigningInfo: ValidatorSigningInfo | undefined;
}
/**
 * QuerySigningInfosRequest is the request type for the Query/SigningInfos RPC
 * method
 */
export interface QuerySigningInfosRequest {
    pagination: PageRequest | undefined;
}
/**
 * QuerySigningInfosResponse is the response type for the Query/SigningInfos RPC
 * method
 */
export interface QuerySigningInfosResponse {
    /** info is the signing info of all validators */
    info: ValidatorSigningInfo[];
    pagination: PageResponse | undefined;
}
export declare const QueryParamsRequest: {
    encode(_: QueryParamsRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryParamsRequest;
    fromJSON(_: any): QueryParamsRequest;
    toJSON(_: QueryParamsRequest): unknown;
    fromPartial(_: DeepPartial<QueryParamsRequest>): QueryParamsRequest;
};
export declare const QueryParamsResponse: {
    encode(message: QueryParamsResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryParamsResponse;
    fromJSON(object: any): QueryParamsResponse;
    toJSON(message: QueryParamsResponse): unknown;
    fromPartial(object: DeepPartial<QueryParamsResponse>): QueryParamsResponse;
};
export declare const QuerySigningInfoRequest: {
    encode(message: QuerySigningInfoRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QuerySigningInfoRequest;
    fromJSON(object: any): QuerySigningInfoRequest;
    toJSON(message: QuerySigningInfoRequest): unknown;
    fromPartial(object: DeepPartial<QuerySigningInfoRequest>): QuerySigningInfoRequest;
};
export declare const QuerySigningInfoResponse: {
    encode(message: QuerySigningInfoResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QuerySigningInfoResponse;
    fromJSON(object: any): QuerySigningInfoResponse;
    toJSON(message: QuerySigningInfoResponse): unknown;
    fromPartial(object: DeepPartial<QuerySigningInfoResponse>): QuerySigningInfoResponse;
};
export declare const QuerySigningInfosRequest: {
    encode(message: QuerySigningInfosRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QuerySigningInfosRequest;
    fromJSON(object: any): QuerySigningInfosRequest;
    toJSON(message: QuerySigningInfosRequest): unknown;
    fromPartial(object: DeepPartial<QuerySigningInfosRequest>): QuerySigningInfosRequest;
};
export declare const QuerySigningInfosResponse: {
    encode(message: QuerySigningInfosResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QuerySigningInfosResponse;
    fromJSON(object: any): QuerySigningInfosResponse;
    toJSON(message: QuerySigningInfosResponse): unknown;
    fromPartial(object: DeepPartial<QuerySigningInfosResponse>): QuerySigningInfosResponse;
};
/** Query provides defines the gRPC querier service */
export interface Query {
    /** Params queries the parameters of slashing module */
    Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
    /** SigningInfo queries the signing info of given cons address */
    SigningInfo(request: QuerySigningInfoRequest): Promise<QuerySigningInfoResponse>;
    /** SigningInfos queries signing info of all validators */
    SigningInfos(request: QuerySigningInfosRequest): Promise<QuerySigningInfosResponse>;
}
export declare class QueryClientImpl implements Query {
    private readonly rpc;
    constructor(rpc: Rpc);
    Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
    SigningInfo(request: QuerySigningInfoRequest): Promise<QuerySigningInfoResponse>;
    SigningInfos(request: QuerySigningInfosRequest): Promise<QuerySigningInfosResponse>;
}
interface Rpc {
    request(service: string, method: string, data: Uint8Array): Promise<Uint8Array>;
}
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
