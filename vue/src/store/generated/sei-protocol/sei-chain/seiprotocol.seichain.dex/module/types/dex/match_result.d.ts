import { Writer, Reader } from "protobufjs/minimal";
import { Order, Cancellation } from "../dex/order";
import { SettlementEntry } from "../dex/settlement";
export declare const protobufPackage = "seiprotocol.seichain.dex";
export interface MatchResult {
    height: number;
    contractAddr: string;
    orders: Order[];
    settlements: SettlementEntry[];
    cancellations: Cancellation[];
}
export declare const MatchResult: {
    encode(message: MatchResult, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MatchResult;
    fromJSON(object: any): MatchResult;
    toJSON(message: MatchResult): unknown;
    fromPartial(object: DeepPartial<MatchResult>): MatchResult;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
