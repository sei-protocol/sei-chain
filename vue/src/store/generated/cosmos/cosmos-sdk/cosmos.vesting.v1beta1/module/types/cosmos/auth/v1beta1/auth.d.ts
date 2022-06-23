import { Writer, Reader } from "protobufjs/minimal";
import { Any } from "../../../google/protobuf/any";
export declare const protobufPackage = "cosmos.auth.v1beta1";
/**
 * BaseAccount defines a base account type. It contains all the necessary fields
 * for basic account functionality. Any custom account type should extend this
 * type for additional functionality (e.g. vesting).
 */
export interface BaseAccount {
    address: string;
    pubKey: Any | undefined;
    accountNumber: number;
    sequence: number;
}
/** ModuleAccount defines an account for modules that holds coins on a pool. */
export interface ModuleAccount {
    baseAccount: BaseAccount | undefined;
    name: string;
    permissions: string[];
}
/** Params defines the parameters for the auth module. */
export interface Params {
    maxMemoCharacters: number;
    txSigLimit: number;
    txSizeCostPerByte: number;
    sigVerifyCostEd25519: number;
    sigVerifyCostSecp256k1: number;
}
export declare const BaseAccount: {
    encode(message: BaseAccount, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): BaseAccount;
    fromJSON(object: any): BaseAccount;
    toJSON(message: BaseAccount): unknown;
    fromPartial(object: DeepPartial<BaseAccount>): BaseAccount;
};
export declare const ModuleAccount: {
    encode(message: ModuleAccount, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): ModuleAccount;
    fromJSON(object: any): ModuleAccount;
    toJSON(message: ModuleAccount): unknown;
    fromPartial(object: DeepPartial<ModuleAccount>): ModuleAccount;
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
