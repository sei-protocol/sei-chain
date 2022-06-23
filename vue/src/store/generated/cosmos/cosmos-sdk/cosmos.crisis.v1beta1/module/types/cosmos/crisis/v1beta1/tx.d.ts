import { Reader, Writer } from "protobufjs/minimal";
export declare const protobufPackage = "cosmos.crisis.v1beta1";
/** MsgVerifyInvariant represents a message to verify a particular invariance. */
export interface MsgVerifyInvariant {
    sender: string;
    invariantModuleName: string;
    invariantRoute: string;
}
/** MsgVerifyInvariantResponse defines the Msg/VerifyInvariant response type. */
export interface MsgVerifyInvariantResponse {
}
export declare const MsgVerifyInvariant: {
    encode(message: MsgVerifyInvariant, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgVerifyInvariant;
    fromJSON(object: any): MsgVerifyInvariant;
    toJSON(message: MsgVerifyInvariant): unknown;
    fromPartial(object: DeepPartial<MsgVerifyInvariant>): MsgVerifyInvariant;
};
export declare const MsgVerifyInvariantResponse: {
    encode(_: MsgVerifyInvariantResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgVerifyInvariantResponse;
    fromJSON(_: any): MsgVerifyInvariantResponse;
    toJSON(_: MsgVerifyInvariantResponse): unknown;
    fromPartial(_: DeepPartial<MsgVerifyInvariantResponse>): MsgVerifyInvariantResponse;
};
/** Msg defines the bank Msg service. */
export interface Msg {
    /** VerifyInvariant defines a method to verify a particular invariance. */
    VerifyInvariant(request: MsgVerifyInvariant): Promise<MsgVerifyInvariantResponse>;
}
export declare class MsgClientImpl implements Msg {
    private readonly rpc;
    constructor(rpc: Rpc);
    VerifyInvariant(request: MsgVerifyInvariant): Promise<MsgVerifyInvariantResponse>;
}
interface Rpc {
    request(service: string, method: string, data: Uint8Array): Promise<Uint8Array>;
}
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
