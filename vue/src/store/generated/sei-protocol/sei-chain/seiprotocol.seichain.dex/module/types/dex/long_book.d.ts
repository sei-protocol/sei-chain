import { OrderEntry } from "../dex/order_entry";
import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "seiprotocol.seichain.dex";
export interface LongBook {
    price: string;
    entry: OrderEntry | undefined;
}
export declare const LongBook: {
    encode(message: LongBook, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): LongBook;
    fromJSON(object: any): LongBook;
    toJSON(message: LongBook): unknown;
    fromPartial(object: DeepPartial<LongBook>): LongBook;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
