import { Metadata } from "../cosmos/bank/v1beta1/bank";
import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "seiprotocol.seichain.dex";
export interface AssetIBCInfo {
    sourceChannel: string;
    dstChannel: string;
    sourceDenom: string;
    sourceChainID: string;
}
export interface AssetMetadata {
    ibcInfo: AssetIBCInfo | undefined;
    /** Ex: cw20, ics20, erc20 */
    typeAsset: string;
    metadata: Metadata | undefined;
}
export declare const AssetIBCInfo: {
    encode(message: AssetIBCInfo, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): AssetIBCInfo;
    fromJSON(object: any): AssetIBCInfo;
    toJSON(message: AssetIBCInfo): unknown;
    fromPartial(object: DeepPartial<AssetIBCInfo>): AssetIBCInfo;
};
export declare const AssetMetadata: {
    encode(message: AssetMetadata, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): AssetMetadata;
    fromJSON(object: any): AssetMetadata;
    toJSON(message: AssetMetadata): unknown;
    fromPartial(object: DeepPartial<AssetMetadata>): AssetMetadata;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
