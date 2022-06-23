import { Reader, Writer } from "protobufjs/minimal";
import { Any } from "../../../google/protobuf/any";
import { PageRequest, PageResponse } from "../../../cosmos/base/query/v1beta1/pagination";
export declare const protobufPackage = "cosmos.evidence.v1beta1";
/** QueryEvidenceRequest is the request type for the Query/Evidence RPC method. */
export interface QueryEvidenceRequest {
    /** evidence_hash defines the hash of the requested evidence. */
    evidenceHash: Uint8Array;
}
/** QueryEvidenceResponse is the response type for the Query/Evidence RPC method. */
export interface QueryEvidenceResponse {
    /** evidence returns the requested evidence. */
    evidence: Any | undefined;
}
/**
 * QueryEvidenceRequest is the request type for the Query/AllEvidence RPC
 * method.
 */
export interface QueryAllEvidenceRequest {
    /** pagination defines an optional pagination for the request. */
    pagination: PageRequest | undefined;
}
/**
 * QueryAllEvidenceResponse is the response type for the Query/AllEvidence RPC
 * method.
 */
export interface QueryAllEvidenceResponse {
    /** evidence returns all evidences. */
    evidence: Any[];
    /** pagination defines the pagination in the response. */
    pagination: PageResponse | undefined;
}
export declare const QueryEvidenceRequest: {
    encode(message: QueryEvidenceRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryEvidenceRequest;
    fromJSON(object: any): QueryEvidenceRequest;
    toJSON(message: QueryEvidenceRequest): unknown;
    fromPartial(object: DeepPartial<QueryEvidenceRequest>): QueryEvidenceRequest;
};
export declare const QueryEvidenceResponse: {
    encode(message: QueryEvidenceResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryEvidenceResponse;
    fromJSON(object: any): QueryEvidenceResponse;
    toJSON(message: QueryEvidenceResponse): unknown;
    fromPartial(object: DeepPartial<QueryEvidenceResponse>): QueryEvidenceResponse;
};
export declare const QueryAllEvidenceRequest: {
    encode(message: QueryAllEvidenceRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryAllEvidenceRequest;
    fromJSON(object: any): QueryAllEvidenceRequest;
    toJSON(message: QueryAllEvidenceRequest): unknown;
    fromPartial(object: DeepPartial<QueryAllEvidenceRequest>): QueryAllEvidenceRequest;
};
export declare const QueryAllEvidenceResponse: {
    encode(message: QueryAllEvidenceResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryAllEvidenceResponse;
    fromJSON(object: any): QueryAllEvidenceResponse;
    toJSON(message: QueryAllEvidenceResponse): unknown;
    fromPartial(object: DeepPartial<QueryAllEvidenceResponse>): QueryAllEvidenceResponse;
};
/** Query defines the gRPC querier service. */
export interface Query {
    /** Evidence queries evidence based on evidence hash. */
    Evidence(request: QueryEvidenceRequest): Promise<QueryEvidenceResponse>;
    /** AllEvidence queries all evidence. */
    AllEvidence(request: QueryAllEvidenceRequest): Promise<QueryAllEvidenceResponse>;
}
export declare class QueryClientImpl implements Query {
    private readonly rpc;
    constructor(rpc: Rpc);
    Evidence(request: QueryEvidenceRequest): Promise<QueryEvidenceResponse>;
    AllEvidence(request: QueryAllEvidenceRequest): Promise<QueryAllEvidenceResponse>;
}
interface Rpc {
    request(service: string, method: string, data: Uint8Array): Promise<Uint8Array>;
}
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
