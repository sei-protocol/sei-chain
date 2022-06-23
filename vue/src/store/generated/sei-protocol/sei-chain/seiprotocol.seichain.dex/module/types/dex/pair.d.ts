import { Denom } from "../dex/enums";
import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "seiprotocol.seichain.dex";
export interface Pair {
    priceDenom: Denom;
    assetDenom: Denom;
}
export declare const Pair: {
    encode(message: Pair, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): Pair;
    fromJSON(object: any): Pair;
    toJSON(message: Pair): unknown;
    fromPartial(object: DeepPartial<Pair>): Pair;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
