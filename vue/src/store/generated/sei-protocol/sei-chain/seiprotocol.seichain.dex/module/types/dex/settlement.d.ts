import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "seiprotocol.seichain.dex";
export interface SettlementEntry {
    account: string;
    priceDenom: string;
    assetDenom: string;
    quantity: string;
    executionCostOrProceed: string;
    expectedCostOrProceed: string;
    positionDirection: string;
    orderType: string;
    orderId: number;
    timestamp: number;
    height: number;
    settlementId: number;
}
export interface Settlements {
    epoch: number;
    entries: SettlementEntry[];
}
export declare const SettlementEntry: {
    encode(message: SettlementEntry, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): SettlementEntry;
    fromJSON(object: any): SettlementEntry;
    toJSON(message: SettlementEntry): unknown;
    fromPartial(object: DeepPartial<SettlementEntry>): SettlementEntry;
};
export declare const Settlements: {
    encode(message: Settlements, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): Settlements;
    fromJSON(object: any): Settlements;
    toJSON(message: Settlements): unknown;
    fromPartial(object: DeepPartial<Settlements>): Settlements;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
