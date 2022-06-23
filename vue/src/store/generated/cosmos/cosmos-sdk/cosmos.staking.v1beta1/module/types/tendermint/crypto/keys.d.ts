import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "tendermint.crypto";
/** PublicKey defines the keys available for use with Tendermint Validators */
export interface PublicKey {
    ed25519: Uint8Array | undefined;
    secp256k1: Uint8Array | undefined;
}
export declare const PublicKey: {
    encode(message: PublicKey, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): PublicKey;
    fromJSON(object: any): PublicKey;
    toJSON(message: PublicKey): unknown;
    fromPartial(object: DeepPartial<PublicKey>): PublicKey;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
