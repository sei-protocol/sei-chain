import { Coin } from "../../../cosmos/base/v1beta1/coin";
import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "cosmos.bank.v1beta1";
/**
 * SendAuthorization allows the grantee to spend up to spend_limit coins from
 * the granter's account.
 *
 * Since: cosmos-sdk 0.43
 */
export interface SendAuthorization {
    spendLimit: Coin[];
}
export declare const SendAuthorization: {
    encode(message: SendAuthorization, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): SendAuthorization;
    fromJSON(object: any): SendAuthorization;
    toJSON(message: SendAuthorization): unknown;
    fromPartial(object: DeepPartial<SendAuthorization>): SendAuthorization;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
