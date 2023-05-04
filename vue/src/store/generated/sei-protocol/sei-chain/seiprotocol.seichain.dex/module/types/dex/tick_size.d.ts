import { Pair } from "../dex/pair";
import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "seiprotocol.seichain.dex";
export interface TickSize {
    pair: Pair | undefined;
    ticksize: string;
    contractAddr: string;
}
export declare const TickSize: {
    encode(message: TickSize, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): TickSize;
    fromJSON(object: any): TickSize;
    toJSON(message: TickSize): unknown;
    fromPartial(object: DeepPartial<TickSize>): TickSize;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
