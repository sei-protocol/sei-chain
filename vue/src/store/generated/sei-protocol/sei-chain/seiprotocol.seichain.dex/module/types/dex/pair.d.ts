import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "seiprotocol.seichain.dex";
export interface Pair {
    priceDenom: string;
    assetDenom: string;
    ticksize: string;
}
export interface BatchContractPair {
    contractAddr: string;
    pairs: Pair[];
}
export declare const Pair: {
    encode(message: Pair, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): Pair;
    fromJSON(object: any): Pair;
    toJSON(message: Pair): unknown;
    fromPartial(object: DeepPartial<Pair>): Pair;
};
export declare const BatchContractPair: {
    encode(message: BatchContractPair, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): BatchContractPair;
    fromJSON(object: any): BatchContractPair;
    toJSON(message: BatchContractPair): unknown;
    fromPartial(object: DeepPartial<BatchContractPair>): BatchContractPair;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
