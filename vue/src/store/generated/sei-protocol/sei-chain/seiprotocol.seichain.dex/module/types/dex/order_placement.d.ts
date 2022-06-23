import { PositionDirection, Denom, PositionEffect, OrderType } from "../dex/enums";
import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "seiprotocol.seichain.dex";
export interface OrderPlacement {
    positionDirection: PositionDirection;
    price: string;
    quantity: string;
    priceDenom: Denom;
    assetDenom: Denom;
    positionEffect: PositionEffect;
    orderType: OrderType;
    leverage: string;
}
export declare const OrderPlacement: {
    encode(message: OrderPlacement, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): OrderPlacement;
    fromJSON(object: any): OrderPlacement;
    toJSON(message: OrderPlacement): unknown;
    fromPartial(object: DeepPartial<OrderPlacement>): OrderPlacement;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
