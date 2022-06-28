import { Writer, Reader } from "protobufjs/minimal";
import { Pair } from "../dex/pair";
export declare const protobufPackage = "seiprotocol.seichain.dex";
export interface Twap {
    pair: Pair | undefined;
    twap: string;
    lookbackSeconds: number;
}
export declare const Twap: {
    encode(message: Twap, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): Twap;
    fromJSON(object: any): Twap;
    toJSON(message: Twap): unknown;
    fromPartial(object: DeepPartial<Twap>): Twap;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
