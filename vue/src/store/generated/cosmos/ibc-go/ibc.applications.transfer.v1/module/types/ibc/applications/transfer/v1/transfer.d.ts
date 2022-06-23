import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "ibc.applications.transfer.v1";
/**
 * DenomTrace contains the base denomination for ICS20 fungible tokens and the
 * source tracing information path.
 */
export interface DenomTrace {
    /**
     * path defines the chain of port/channel identifiers used for tracing the
     * source of the fungible token.
     */
    path: string;
    /** base denomination of the relayed fungible token. */
    baseDenom: string;
}
/**
 * Params defines the set of IBC transfer parameters.
 * NOTE: To prevent a single token from being transferred, set the
 * TransfersEnabled parameter to true and then set the bank module's SendEnabled
 * parameter for the denomination to false.
 */
export interface Params {
    /**
     * send_enabled enables or disables all cross-chain token transfers from this
     * chain.
     */
    sendEnabled: boolean;
    /**
     * receive_enabled enables or disables all cross-chain token transfers to this
     * chain.
     */
    receiveEnabled: boolean;
}
export declare const DenomTrace: {
    encode(message: DenomTrace, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): DenomTrace;
    fromJSON(object: any): DenomTrace;
    toJSON(message: DenomTrace): unknown;
    fromPartial(object: DeepPartial<DenomTrace>): DenomTrace;
};
export declare const Params: {
    encode(message: Params, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): Params;
    fromJSON(object: any): Params;
    toJSON(message: Params): unknown;
    fromPartial(object: DeepPartial<Params>): Params;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
