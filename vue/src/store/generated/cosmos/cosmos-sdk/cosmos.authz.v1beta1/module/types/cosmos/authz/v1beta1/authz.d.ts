import { Any } from "../../../google/protobuf/any";
import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "cosmos.authz.v1beta1";
/** Since: cosmos-sdk 0.43 */
/**
 * GenericAuthorization gives the grantee unrestricted permissions to execute
 * the provided method on behalf of the granter's account.
 */
export interface GenericAuthorization {
    /** Msg, identified by it's type URL, to grant unrestricted permissions to execute */
    msg: string;
}
/**
 * Grant gives permissions to execute
 * the provide method with expiration time.
 */
export interface Grant {
    authorization: Any | undefined;
    expiration: Date | undefined;
}
/**
 * GrantAuthorization extends a grant with both the addresses of the grantee and granter.
 * It is used in genesis.proto and query.proto
 *
 * Since: cosmos-sdk 0.45.2
 */
export interface GrantAuthorization {
    granter: string;
    grantee: string;
    authorization: Any | undefined;
    expiration: Date | undefined;
}
export declare const GenericAuthorization: {
    encode(message: GenericAuthorization, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): GenericAuthorization;
    fromJSON(object: any): GenericAuthorization;
    toJSON(message: GenericAuthorization): unknown;
    fromPartial(object: DeepPartial<GenericAuthorization>): GenericAuthorization;
};
export declare const Grant: {
    encode(message: Grant, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): Grant;
    fromJSON(object: any): Grant;
    toJSON(message: Grant): unknown;
    fromPartial(object: DeepPartial<Grant>): Grant;
};
export declare const GrantAuthorization: {
    encode(message: GrantAuthorization, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): GrantAuthorization;
    fromJSON(object: any): GrantAuthorization;
    toJSON(message: GrantAuthorization): unknown;
    fromPartial(object: DeepPartial<GrantAuthorization>): GrantAuthorization;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
