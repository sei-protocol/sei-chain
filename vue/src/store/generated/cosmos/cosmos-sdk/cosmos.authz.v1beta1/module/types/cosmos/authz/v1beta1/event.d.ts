import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "cosmos.authz.v1beta1";
/** Since: cosmos-sdk 0.43 */
/** EventGrant is emitted on Msg/Grant */
export interface EventGrant {
    /** Msg type URL for which an autorization is granted */
    msgTypeUrl: string;
    /** Granter account address */
    granter: string;
    /** Grantee account address */
    grantee: string;
}
/** EventRevoke is emitted on Msg/Revoke */
export interface EventRevoke {
    /** Msg type URL for which an autorization is revoked */
    msgTypeUrl: string;
    /** Granter account address */
    granter: string;
    /** Grantee account address */
    grantee: string;
}
export declare const EventGrant: {
    encode(message: EventGrant, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): EventGrant;
    fromJSON(object: any): EventGrant;
    toJSON(message: EventGrant): unknown;
    fromPartial(object: DeepPartial<EventGrant>): EventGrant;
};
export declare const EventRevoke: {
    encode(message: EventRevoke, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): EventRevoke;
    fromJSON(object: any): EventRevoke;
    toJSON(message: EventRevoke): unknown;
    fromPartial(object: DeepPartial<EventRevoke>): EventRevoke;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
