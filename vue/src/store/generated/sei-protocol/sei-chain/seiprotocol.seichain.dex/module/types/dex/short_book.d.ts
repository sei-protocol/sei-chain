import { OrderEntry } from "../dex/order_entry";
import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "seiprotocol.seichain.dex";
export interface ShortBook {
    price: string;
    entry: OrderEntry | undefined;
}
export declare const ShortBook: {
    encode(message: ShortBook, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): ShortBook;
    fromJSON(object: any): ShortBook;
    toJSON(message: ShortBook): unknown;
    fromPartial(object: DeepPartial<ShortBook>): ShortBook;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
