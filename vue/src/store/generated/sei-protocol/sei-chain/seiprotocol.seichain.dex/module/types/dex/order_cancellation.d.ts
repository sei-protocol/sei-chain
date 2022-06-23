import { PositionDirection, Denom, PositionEffect } from "../dex/enums";
import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "seiprotocol.seichain.dex";
export interface OrderCancellation {
    positionDirection: PositionDirection;
    price: string;
    quantity: string;
    priceDenom: Denom;
    assetDenom: Denom;
    positionEffect: PositionEffect;
    leverage: string;
}
export declare const OrderCancellation: {
    encode(message: OrderCancellation, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): OrderCancellation;
    fromJSON(object: any): OrderCancellation;
    toJSON(message: OrderCancellation): unknown;
    fromPartial(object: DeepPartial<OrderCancellation>): OrderCancellation;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
