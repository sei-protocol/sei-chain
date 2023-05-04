import { Writer, Reader } from "protobufjs/minimal";
import { Pair } from "../dex/pair";
export declare const protobufPackage = "seiprotocol.seichain.dex";
export interface Price {
    snapshotTimestampInSeconds: number;
    price: string;
    pair: Pair | undefined;
}
export interface PriceCandlestick {
    beginTimestamp: number;
    endTimestamp: number;
    open: string;
    high: string;
    low: string;
    close: string;
    volume: string;
}
export declare const Price: {
    encode(message: Price, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): Price;
    fromJSON(object: any): Price;
    toJSON(message: Price): unknown;
    fromPartial(object: DeepPartial<Price>): Price;
};
export declare const PriceCandlestick: {
    encode(message: PriceCandlestick, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): PriceCandlestick;
    fromJSON(object: any): PriceCandlestick;
    toJSON(message: PriceCandlestick): unknown;
    fromPartial(object: DeepPartial<PriceCandlestick>): PriceCandlestick;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
