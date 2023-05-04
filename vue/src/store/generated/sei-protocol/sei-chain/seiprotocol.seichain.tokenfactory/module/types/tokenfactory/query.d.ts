import { Reader, Writer } from "protobufjs/minimal";
import { Params } from "../tokenfactory/params";
import { DenomAuthorityMetadata } from "../tokenfactory/authorityMetadata";
export declare const protobufPackage = "seiprotocol.seichain.tokenfactory";
/** QueryParamsRequest is the request type for the Query/Params RPC method. */
export interface QueryParamsRequest {
}
/** QueryParamsResponse is the response type for the Query/Params RPC method. */
export interface QueryParamsResponse {
    /** params defines the parameters of the module. */
    params: Params | undefined;
}
/**
 * QueryDenomAuthorityMetadataRequest defines the request structure for the
 * DenomAuthorityMetadata gRPC query.
 */
export interface QueryDenomAuthorityMetadataRequest {
    denom: string;
}
/**
 * QueryDenomAuthorityMetadataResponse defines the response structure for the
 * DenomAuthorityMetadata gRPC query.
 */
export interface QueryDenomAuthorityMetadataResponse {
    authorityMetadata: DenomAuthorityMetadata | undefined;
}
/**
 * QueryDenomsFromCreatorRequest defines the request structure for the
 * DenomsFromCreator gRPC query.
 */
export interface QueryDenomsFromCreatorRequest {
    creator: string;
}
/**
 * QueryDenomsFromCreatorRequest defines the response structure for the
 * DenomsFromCreator gRPC query.
 */
export interface QueryDenomsFromCreatorResponse {
    denoms: string[];
}
/**
 * QueryDenomCreationFeeWhitelistRequest defines the request structure for the
 * DenomCreationFeeWhitelist gRPC query.
 */
export interface QueryDenomCreationFeeWhitelistRequest {
}
/**
 * QueryDenomCreationFeeWhitelistResponse defines the response structure for the
 * DenomsFromCreator gRPC query.
 */
export interface QueryDenomCreationFeeWhitelistResponse {
    creators: string[];
}
/**
 * QueryCreatorInDenomFeeWhitelistRequest defines the request structure for the
 * CreatorInDenomFeeWhitelist gRPC query.
 */
export interface QueryCreatorInDenomFeeWhitelistRequest {
    creator: string;
}
/**
 * QueryCreatorInDenomFeeWhitelistResponse defines the response structure for the
 * CreatorInDenomFeeWhitelist gRPC query.
 */
export interface QueryCreatorInDenomFeeWhitelistResponse {
    whitelisted: boolean;
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
export declare const QueryDenomAuthorityMetadataRequest: {
    encode(message: QueryDenomAuthorityMetadataRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDenomAuthorityMetadataRequest;
    fromJSON(object: any): QueryDenomAuthorityMetadataRequest;
    toJSON(message: QueryDenomAuthorityMetadataRequest): unknown;
    fromPartial(object: DeepPartial<QueryDenomAuthorityMetadataRequest>): QueryDenomAuthorityMetadataRequest;
};
export declare const QueryDenomAuthorityMetadataResponse: {
    encode(message: QueryDenomAuthorityMetadataResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDenomAuthorityMetadataResponse;
    fromJSON(object: any): QueryDenomAuthorityMetadataResponse;
    toJSON(message: QueryDenomAuthorityMetadataResponse): unknown;
    fromPartial(object: DeepPartial<QueryDenomAuthorityMetadataResponse>): QueryDenomAuthorityMetadataResponse;
};
export declare const QueryDenomsFromCreatorRequest: {
    encode(message: QueryDenomsFromCreatorRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDenomsFromCreatorRequest;
    fromJSON(object: any): QueryDenomsFromCreatorRequest;
    toJSON(message: QueryDenomsFromCreatorRequest): unknown;
    fromPartial(object: DeepPartial<QueryDenomsFromCreatorRequest>): QueryDenomsFromCreatorRequest;
};
export declare const QueryDenomsFromCreatorResponse: {
    encode(message: QueryDenomsFromCreatorResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDenomsFromCreatorResponse;
    fromJSON(object: any): QueryDenomsFromCreatorResponse;
    toJSON(message: QueryDenomsFromCreatorResponse): unknown;
    fromPartial(object: DeepPartial<QueryDenomsFromCreatorResponse>): QueryDenomsFromCreatorResponse;
};
export declare const QueryDenomCreationFeeWhitelistRequest: {
    encode(_: QueryDenomCreationFeeWhitelistRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDenomCreationFeeWhitelistRequest;
    fromJSON(_: any): QueryDenomCreationFeeWhitelistRequest;
    toJSON(_: QueryDenomCreationFeeWhitelistRequest): unknown;
    fromPartial(_: DeepPartial<QueryDenomCreationFeeWhitelistRequest>): QueryDenomCreationFeeWhitelistRequest;
};
export declare const QueryDenomCreationFeeWhitelistResponse: {
    encode(message: QueryDenomCreationFeeWhitelistResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDenomCreationFeeWhitelistResponse;
    fromJSON(object: any): QueryDenomCreationFeeWhitelistResponse;
    toJSON(message: QueryDenomCreationFeeWhitelistResponse): unknown;
    fromPartial(object: DeepPartial<QueryDenomCreationFeeWhitelistResponse>): QueryDenomCreationFeeWhitelistResponse;
};
export declare const QueryCreatorInDenomFeeWhitelistRequest: {
    encode(message: QueryCreatorInDenomFeeWhitelistRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryCreatorInDenomFeeWhitelistRequest;
    fromJSON(object: any): QueryCreatorInDenomFeeWhitelistRequest;
    toJSON(message: QueryCreatorInDenomFeeWhitelistRequest): unknown;
    fromPartial(object: DeepPartial<QueryCreatorInDenomFeeWhitelistRequest>): QueryCreatorInDenomFeeWhitelistRequest;
};
export declare const QueryCreatorInDenomFeeWhitelistResponse: {
    encode(message: QueryCreatorInDenomFeeWhitelistResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryCreatorInDenomFeeWhitelistResponse;
    fromJSON(object: any): QueryCreatorInDenomFeeWhitelistResponse;
    toJSON(message: QueryCreatorInDenomFeeWhitelistResponse): unknown;
    fromPartial(object: DeepPartial<QueryCreatorInDenomFeeWhitelistResponse>): QueryCreatorInDenomFeeWhitelistResponse;
};
/** Query defines the gRPC querier service. */
export interface Query {
    /**
     * Params defines a gRPC query method that returns the tokenfactory module's
     * parameters.
     */
    Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
    /**
     * DenomAuthorityMetadata defines a gRPC query method for fetching
     * DenomAuthorityMetadata for a particular denom.
     */
    DenomAuthorityMetadata(request: QueryDenomAuthorityMetadataRequest): Promise<QueryDenomAuthorityMetadataResponse>;
    /**
     * DenomsFromCreator defines a gRPC query method for fetching all
     * denominations created by a specific admin/creator.
     */
    DenomsFromCreator(request: QueryDenomsFromCreatorRequest): Promise<QueryDenomsFromCreatorResponse>;
    /**
     * DenomCreationFeeWhitelist defines a gRPC query method for fetching all
     * creators who are whitelisted from paying the denom creation fee.
     */
    DenomCreationFeeWhitelist(request: QueryDenomCreationFeeWhitelistRequest): Promise<QueryDenomCreationFeeWhitelistResponse>;
    /**
     * CreatorInDenomFeeWhitelist defines a gRPC query method for fetching
     * whether a creator is whitelisted from denom creation fees.
     */
    CreatorInDenomFeeWhitelist(request: QueryCreatorInDenomFeeWhitelistRequest): Promise<QueryCreatorInDenomFeeWhitelistResponse>;
}
export declare class QueryClientImpl implements Query {
    private readonly rpc;
    constructor(rpc: Rpc);
    Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
    DenomAuthorityMetadata(request: QueryDenomAuthorityMetadataRequest): Promise<QueryDenomAuthorityMetadataResponse>;
    DenomsFromCreator(request: QueryDenomsFromCreatorRequest): Promise<QueryDenomsFromCreatorResponse>;
    DenomCreationFeeWhitelist(request: QueryDenomCreationFeeWhitelistRequest): Promise<QueryDenomCreationFeeWhitelistResponse>;
    CreatorInDenomFeeWhitelist(request: QueryCreatorInDenomFeeWhitelistRequest): Promise<QueryCreatorInDenomFeeWhitelistResponse>;
}
interface Rpc {
    request(service: string, method: string, data: Uint8Array): Promise<Uint8Array>;
}
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
