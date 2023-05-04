import { BatchContractPair } from "../dex/pair";
import { TickSize } from "../dex/tick_size";
import { AssetMetadata } from "../dex/asset_list";
import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "seiprotocol.seichain.dex";
/**
 * RegisterPairsProposal is a gov Content type for adding a new whitelisted token
 * pair to the dex module. It must specify a list of contract addresses and their respective
 * token pairs to be registered.
 */
export interface RegisterPairsProposal {
    title: string;
    description: string;
    batchcontractpair: BatchContractPair[];
}
export interface UpdateTickSizeProposal {
    title: string;
    description: string;
    tickSizeList: TickSize[];
}
/**
 * AddAssetMetadataProposal is a gov Content type for adding a new asset
 * to the dex module's asset list.
 */
export interface AddAssetMetadataProposal {
    title: string;
    description: string;
    assetList: AssetMetadata[];
}
export declare const RegisterPairsProposal: {
    encode(message: RegisterPairsProposal, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): RegisterPairsProposal;
    fromJSON(object: any): RegisterPairsProposal;
    toJSON(message: RegisterPairsProposal): unknown;
    fromPartial(object: DeepPartial<RegisterPairsProposal>): RegisterPairsProposal;
};
export declare const UpdateTickSizeProposal: {
    encode(message: UpdateTickSizeProposal, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): UpdateTickSizeProposal;
    fromJSON(object: any): UpdateTickSizeProposal;
    toJSON(message: UpdateTickSizeProposal): unknown;
    fromPartial(object: DeepPartial<UpdateTickSizeProposal>): UpdateTickSizeProposal;
};
export declare const AddAssetMetadataProposal: {
    encode(message: AddAssetMetadataProposal, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): AddAssetMetadataProposal;
    fromJSON(object: any): AddAssetMetadataProposal;
    toJSON(message: AddAssetMetadataProposal): unknown;
    fromPartial(object: DeepPartial<AddAssetMetadataProposal>): AddAssetMetadataProposal;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
