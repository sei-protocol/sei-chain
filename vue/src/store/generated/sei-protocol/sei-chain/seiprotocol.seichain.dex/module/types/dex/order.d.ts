import { OrderStatus, OrderType, PositionDirection, CancellationInitiator } from "../dex/enums";
import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "seiprotocol.seichain.dex";
export interface Order {
    id: number;
    status: OrderStatus;
    account: string;
    contractAddr: string;
    price: string;
    quantity: string;
    priceDenom: string;
    assetDenom: string;
    orderType: OrderType;
    positionDirection: PositionDirection;
    data: string;
    statusDescription: string;
}
export interface Cancellation {
    id: number;
    initiator: CancellationInitiator;
    creator: string;
    contractAddr: string;
    priceDenom: string;
    assetDenom: string;
    positionDirection: PositionDirection;
    price: string;
}
export interface ActiveOrders {
    ids: number[];
}
export declare const Order: {
    encode(message: Order, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): Order;
    fromJSON(object: any): Order;
    toJSON(message: Order): unknown;
    fromPartial(object: DeepPartial<Order>): Order;
};
export declare const Cancellation: {
    encode(message: Cancellation, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): Cancellation;
    fromJSON(object: any): Cancellation;
    toJSON(message: Cancellation): unknown;
    fromPartial(object: DeepPartial<Cancellation>): Cancellation;
};
export declare const ActiveOrders: {
    encode(message: ActiveOrders, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): ActiveOrders;
    fromJSON(object: any): ActiveOrders;
    toJSON(message: ActiveOrders): unknown;
    fromPartial(object: DeepPartial<ActiveOrders>): ActiveOrders;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
