import { Reader, Writer } from "protobufjs/minimal";
import { DenomTrace, Params } from "../../../../ibc/applications/transfer/v1/transfer";
import { PageRequest, PageResponse } from "../../../../cosmos/base/query/v1beta1/pagination";
export declare const protobufPackage = "ibc.applications.transfer.v1";
/**
 * QueryDenomTraceRequest is the request type for the Query/DenomTrace RPC
 * method
 */
export interface QueryDenomTraceRequest {
    /** hash (in hex format) of the denomination trace information. */
    hash: string;
}
/**
 * QueryDenomTraceResponse is the response type for the Query/DenomTrace RPC
 * method.
 */
export interface QueryDenomTraceResponse {
    /** denom_trace returns the requested denomination trace information. */
    denomTrace: DenomTrace | undefined;
}
/**
 * QueryConnectionsRequest is the request type for the Query/DenomTraces RPC
 * method
 */
export interface QueryDenomTracesRequest {
    /** pagination defines an optional pagination for the request. */
    pagination: PageRequest | undefined;
}
/**
 * QueryConnectionsResponse is the response type for the Query/DenomTraces RPC
 * method.
 */
export interface QueryDenomTracesResponse {
    /** denom_traces returns all denominations trace information. */
    denomTraces: DenomTrace[];
    /** pagination defines the pagination in the response. */
    pagination: PageResponse | undefined;
}
/** QueryParamsRequest is the request type for the Query/Params RPC method. */
export interface QueryParamsRequest {
}
/** QueryParamsResponse is the response type for the Query/Params RPC method. */
export interface QueryParamsResponse {
    /** params defines the parameters of the module. */
    params: Params | undefined;
}
/**
 * QueryDenomHashRequest is the request type for the Query/DenomHash RPC
 * method
 */
export interface QueryDenomHashRequest {
    /** The denomination trace ([port_id]/[channel_id])+/[denom] */
    trace: string;
}
/**
 * QueryDenomHashResponse is the response type for the Query/DenomHash RPC
 * method.
 */
export interface QueryDenomHashResponse {
    /** hash (in hex format) of the denomination trace information. */
    hash: string;
}
export declare const QueryDenomTraceRequest: {
    encode(message: QueryDenomTraceRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDenomTraceRequest;
    fromJSON(object: any): QueryDenomTraceRequest;
    toJSON(message: QueryDenomTraceRequest): unknown;
    fromPartial(object: DeepPartial<QueryDenomTraceRequest>): QueryDenomTraceRequest;
};
export declare const QueryDenomTraceResponse: {
    encode(message: QueryDenomTraceResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDenomTraceResponse;
    fromJSON(object: any): QueryDenomTraceResponse;
    toJSON(message: QueryDenomTraceResponse): unknown;
    fromPartial(object: DeepPartial<QueryDenomTraceResponse>): QueryDenomTraceResponse;
};
export declare const QueryDenomTracesRequest: {
    encode(message: QueryDenomTracesRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDenomTracesRequest;
    fromJSON(object: any): QueryDenomTracesRequest;
    toJSON(message: QueryDenomTracesRequest): unknown;
    fromPartial(object: DeepPartial<QueryDenomTracesRequest>): QueryDenomTracesRequest;
};
export declare const QueryDenomTracesResponse: {
    encode(message: QueryDenomTracesResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDenomTracesResponse;
    fromJSON(object: any): QueryDenomTracesResponse;
    toJSON(message: QueryDenomTracesResponse): unknown;
    fromPartial(object: DeepPartial<QueryDenomTracesResponse>): QueryDenomTracesResponse;
};
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
export declare const QueryDenomHashRequest: {
    encode(message: QueryDenomHashRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDenomHashRequest;
    fromJSON(object: any): QueryDenomHashRequest;
    toJSON(message: QueryDenomHashRequest): unknown;
    fromPartial(object: DeepPartial<QueryDenomHashRequest>): QueryDenomHashRequest;
};
export declare const QueryDenomHashResponse: {
    encode(message: QueryDenomHashResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDenomHashResponse;
    fromJSON(object: any): QueryDenomHashResponse;
    toJSON(message: QueryDenomHashResponse): unknown;
    fromPartial(object: DeepPartial<QueryDenomHashResponse>): QueryDenomHashResponse;
};
/** Query provides defines the gRPC querier service. */
export interface Query {
    /** DenomTrace queries a denomination trace information. */
    DenomTrace(request: QueryDenomTraceRequest): Promise<QueryDenomTraceResponse>;
    /** DenomTraces queries all denomination traces. */
    DenomTraces(request: QueryDenomTracesRequest): Promise<QueryDenomTracesResponse>;
    /** Params queries all parameters of the ibc-transfer module. */
    Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
    /** DenomHash queries a denomination hash information. */
    DenomHash(request: QueryDenomHashRequest): Promise<QueryDenomHashResponse>;
}
export declare class QueryClientImpl implements Query {
    private readonly rpc;
    constructor(rpc: Rpc);
    DenomTrace(request: QueryDenomTraceRequest): Promise<QueryDenomTraceResponse>;
    DenomTraces(request: QueryDenomTracesRequest): Promise<QueryDenomTracesResponse>;
    Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
    DenomHash(request: QueryDenomHashRequest): Promise<QueryDenomHashResponse>;
}
interface Rpc {
    request(service: string, method: string, data: Uint8Array): Promise<Uint8Array>;
}
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
