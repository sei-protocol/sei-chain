import { Reader, Writer } from "protobufjs/minimal";
import { OrderPlacement } from "../dex/order_placement";
import { Coin } from "../cosmos/base/v1beta1/coin";
import { OrderCancellation } from "../dex/order_cancellation";
import { Pair } from "../dex/pair";
import { ContractInfo } from "../dex/contract";
export declare const protobufPackage = "seiprotocol.seichain.dex";
export interface MsgPlaceOrders {
    creator: string;
    orders: OrderPlacement[];
    contractAddr: string;
    funds: Coin[];
}
export interface MsgPlaceOrdersResponse {
    orderIds: number[];
}
export interface MsgCancelOrders {
    creator: string;
    orderCancellations: OrderCancellation[];
    contractAddr: string;
}
export interface MsgCancelOrdersResponse {
}
export interface MsgLiquidation {
    creator: string;
    accountToLiquidate: string;
    contractAddr: string;
}
export interface MsgLiquidationResponse {
}
export interface MsgRegisterPair {
    creator: string;
    contractAddr: string;
    pair: Pair | undefined;
}
export interface MsgRegisterPairResponse {
}
export interface MsgRegisterContract {
    creator: string;
    contract: ContractInfo | undefined;
}
export interface MsgRegisterContractResponse {
}
export declare const MsgPlaceOrders: {
    encode(message: MsgPlaceOrders, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgPlaceOrders;
    fromJSON(object: any): MsgPlaceOrders;
    toJSON(message: MsgPlaceOrders): unknown;
    fromPartial(object: DeepPartial<MsgPlaceOrders>): MsgPlaceOrders;
};
export declare const MsgPlaceOrdersResponse: {
    encode(message: MsgPlaceOrdersResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgPlaceOrdersResponse;
    fromJSON(object: any): MsgPlaceOrdersResponse;
    toJSON(message: MsgPlaceOrdersResponse): unknown;
    fromPartial(object: DeepPartial<MsgPlaceOrdersResponse>): MsgPlaceOrdersResponse;
};
export declare const MsgCancelOrders: {
    encode(message: MsgCancelOrders, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgCancelOrders;
    fromJSON(object: any): MsgCancelOrders;
    toJSON(message: MsgCancelOrders): unknown;
    fromPartial(object: DeepPartial<MsgCancelOrders>): MsgCancelOrders;
};
export declare const MsgCancelOrdersResponse: {
    encode(_: MsgCancelOrdersResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgCancelOrdersResponse;
    fromJSON(_: any): MsgCancelOrdersResponse;
    toJSON(_: MsgCancelOrdersResponse): unknown;
    fromPartial(_: DeepPartial<MsgCancelOrdersResponse>): MsgCancelOrdersResponse;
};
export declare const MsgLiquidation: {
    encode(message: MsgLiquidation, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgLiquidation;
    fromJSON(object: any): MsgLiquidation;
    toJSON(message: MsgLiquidation): unknown;
    fromPartial(object: DeepPartial<MsgLiquidation>): MsgLiquidation;
};
export declare const MsgLiquidationResponse: {
    encode(_: MsgLiquidationResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgLiquidationResponse;
    fromJSON(_: any): MsgLiquidationResponse;
    toJSON(_: MsgLiquidationResponse): unknown;
    fromPartial(_: DeepPartial<MsgLiquidationResponse>): MsgLiquidationResponse;
};
export declare const MsgRegisterPair: {
    encode(message: MsgRegisterPair, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgRegisterPair;
    fromJSON(object: any): MsgRegisterPair;
    toJSON(message: MsgRegisterPair): unknown;
    fromPartial(object: DeepPartial<MsgRegisterPair>): MsgRegisterPair;
};
export declare const MsgRegisterPairResponse: {
    encode(_: MsgRegisterPairResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgRegisterPairResponse;
    fromJSON(_: any): MsgRegisterPairResponse;
    toJSON(_: MsgRegisterPairResponse): unknown;
    fromPartial(_: DeepPartial<MsgRegisterPairResponse>): MsgRegisterPairResponse;
};
export declare const MsgRegisterContract: {
    encode(message: MsgRegisterContract, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgRegisterContract;
    fromJSON(object: any): MsgRegisterContract;
    toJSON(message: MsgRegisterContract): unknown;
    fromPartial(object: DeepPartial<MsgRegisterContract>): MsgRegisterContract;
};
export declare const MsgRegisterContractResponse: {
    encode(_: MsgRegisterContractResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgRegisterContractResponse;
    fromJSON(_: any): MsgRegisterContractResponse;
    toJSON(_: MsgRegisterContractResponse): unknown;
    fromPartial(_: DeepPartial<MsgRegisterContractResponse>): MsgRegisterContractResponse;
};
/** Msg defines the Msg service. */
export interface Msg {
    PlaceOrders(request: MsgPlaceOrders): Promise<MsgPlaceOrdersResponse>;
    CancelOrders(request: MsgCancelOrders): Promise<MsgCancelOrdersResponse>;
    Liquidate(request: MsgLiquidation): Promise<MsgLiquidationResponse>;
    RegisterPair(request: MsgRegisterPair): Promise<MsgRegisterPairResponse>;
    /** privileged endpoints below */
    RegisterContract(request: MsgRegisterContract): Promise<MsgRegisterContractResponse>;
}
export declare class MsgClientImpl implements Msg {
    private readonly rpc;
    constructor(rpc: Rpc);
    PlaceOrders(request: MsgPlaceOrders): Promise<MsgPlaceOrdersResponse>;
    CancelOrders(request: MsgCancelOrders): Promise<MsgCancelOrdersResponse>;
    Liquidate(request: MsgLiquidation): Promise<MsgLiquidationResponse>;
    RegisterPair(request: MsgRegisterPair): Promise<MsgRegisterPairResponse>;
    RegisterContract(request: MsgRegisterContract): Promise<MsgRegisterContractResponse>;
}
interface Rpc {
    request(service: string, method: string, data: Uint8Array): Promise<Uint8Array>;
}
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
