import { Reader, Writer } from "protobufjs/minimal";
import { Grant } from "../../../cosmos/authz/v1beta1/authz";
import { Any } from "../../../google/protobuf/any";
export declare const protobufPackage = "cosmos.authz.v1beta1";
/** Since: cosmos-sdk 0.43 */
/**
 * MsgGrant is a request type for Grant method. It declares authorization to the grantee
 * on behalf of the granter with the provided expiration time.
 */
export interface MsgGrant {
    granter: string;
    grantee: string;
    grant: Grant | undefined;
}
/** MsgExecResponse defines the Msg/MsgExecResponse response type. */
export interface MsgExecResponse {
    results: Uint8Array[];
}
/**
 * MsgExec attempts to execute the provided messages using
 * authorizations granted to the grantee. Each message should have only
 * one signer corresponding to the granter of the authorization.
 */
export interface MsgExec {
    grantee: string;
    /**
     * Authorization Msg requests to execute. Each msg must implement Authorization interface
     * The x/authz will try to find a grant matching (msg.signers[0], grantee, MsgTypeURL(msg))
     * triple and validate it.
     */
    msgs: Any[];
}
/** MsgGrantResponse defines the Msg/MsgGrant response type. */
export interface MsgGrantResponse {
}
/**
 * MsgRevoke revokes any authorization with the provided sdk.Msg type on the
 * granter's account with that has been granted to the grantee.
 */
export interface MsgRevoke {
    granter: string;
    grantee: string;
    msgTypeUrl: string;
}
/** MsgRevokeResponse defines the Msg/MsgRevokeResponse response type. */
export interface MsgRevokeResponse {
}
export declare const MsgGrant: {
    encode(message: MsgGrant, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgGrant;
    fromJSON(object: any): MsgGrant;
    toJSON(message: MsgGrant): unknown;
    fromPartial(object: DeepPartial<MsgGrant>): MsgGrant;
};
export declare const MsgExecResponse: {
    encode(message: MsgExecResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgExecResponse;
    fromJSON(object: any): MsgExecResponse;
    toJSON(message: MsgExecResponse): unknown;
    fromPartial(object: DeepPartial<MsgExecResponse>): MsgExecResponse;
};
export declare const MsgExec: {
    encode(message: MsgExec, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgExec;
    fromJSON(object: any): MsgExec;
    toJSON(message: MsgExec): unknown;
    fromPartial(object: DeepPartial<MsgExec>): MsgExec;
};
export declare const MsgGrantResponse: {
    encode(_: MsgGrantResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgGrantResponse;
    fromJSON(_: any): MsgGrantResponse;
    toJSON(_: MsgGrantResponse): unknown;
    fromPartial(_: DeepPartial<MsgGrantResponse>): MsgGrantResponse;
};
export declare const MsgRevoke: {
    encode(message: MsgRevoke, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgRevoke;
    fromJSON(object: any): MsgRevoke;
    toJSON(message: MsgRevoke): unknown;
    fromPartial(object: DeepPartial<MsgRevoke>): MsgRevoke;
};
export declare const MsgRevokeResponse: {
    encode(_: MsgRevokeResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgRevokeResponse;
    fromJSON(_: any): MsgRevokeResponse;
    toJSON(_: MsgRevokeResponse): unknown;
    fromPartial(_: DeepPartial<MsgRevokeResponse>): MsgRevokeResponse;
};
/** Msg defines the authz Msg service. */
export interface Msg {
    /**
     * Grant grants the provided authorization to the grantee on the granter's
     * account with the provided expiration time. If there is already a grant
     * for the given (granter, grantee, Authorization) triple, then the grant
     * will be overwritten.
     */
    Grant(request: MsgGrant): Promise<MsgGrantResponse>;
    /**
     * Exec attempts to execute the provided messages using
     * authorizations granted to the grantee. Each message should have only
     * one signer corresponding to the granter of the authorization.
     */
    Exec(request: MsgExec): Promise<MsgExecResponse>;
    /**
     * Revoke revokes any authorization corresponding to the provided method name on the
     * granter's account that has been granted to the grantee.
     */
    Revoke(request: MsgRevoke): Promise<MsgRevokeResponse>;
}
export declare class MsgClientImpl implements Msg {
    private readonly rpc;
    constructor(rpc: Rpc);
    Grant(request: MsgGrant): Promise<MsgGrantResponse>;
    Exec(request: MsgExec): Promise<MsgExecResponse>;
    Revoke(request: MsgRevoke): Promise<MsgRevokeResponse>;
}
interface Rpc {
    request(service: string, method: string, data: Uint8Array): Promise<Uint8Array>;
}
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
