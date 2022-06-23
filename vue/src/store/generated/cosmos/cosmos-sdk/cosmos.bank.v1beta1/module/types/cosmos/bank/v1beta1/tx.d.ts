import { Reader, Writer } from "protobufjs/minimal";
import { Coin } from "../../../cosmos/base/v1beta1/coin";
import { Input, Output } from "../../../cosmos/bank/v1beta1/bank";
export declare const protobufPackage = "cosmos.bank.v1beta1";
/** MsgSend represents a message to send coins from one account to another. */
export interface MsgSend {
    fromAddress: string;
    toAddress: string;
    amount: Coin[];
}
/** MsgSendResponse defines the Msg/Send response type. */
export interface MsgSendResponse {
}
/** MsgMultiSend represents an arbitrary multi-in, multi-out send message. */
export interface MsgMultiSend {
    inputs: Input[];
    outputs: Output[];
}
/** MsgMultiSendResponse defines the Msg/MultiSend response type. */
export interface MsgMultiSendResponse {
}
export declare const MsgSend: {
    encode(message: MsgSend, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgSend;
    fromJSON(object: any): MsgSend;
    toJSON(message: MsgSend): unknown;
    fromPartial(object: DeepPartial<MsgSend>): MsgSend;
};
export declare const MsgSendResponse: {
    encode(_: MsgSendResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgSendResponse;
    fromJSON(_: any): MsgSendResponse;
    toJSON(_: MsgSendResponse): unknown;
    fromPartial(_: DeepPartial<MsgSendResponse>): MsgSendResponse;
};
export declare const MsgMultiSend: {
    encode(message: MsgMultiSend, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgMultiSend;
    fromJSON(object: any): MsgMultiSend;
    toJSON(message: MsgMultiSend): unknown;
    fromPartial(object: DeepPartial<MsgMultiSend>): MsgMultiSend;
};
export declare const MsgMultiSendResponse: {
    encode(_: MsgMultiSendResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgMultiSendResponse;
    fromJSON(_: any): MsgMultiSendResponse;
    toJSON(_: MsgMultiSendResponse): unknown;
    fromPartial(_: DeepPartial<MsgMultiSendResponse>): MsgMultiSendResponse;
};
/** Msg defines the bank Msg service. */
export interface Msg {
    /** Send defines a method for sending coins from one account to another account. */
    Send(request: MsgSend): Promise<MsgSendResponse>;
    /** MultiSend defines a method for sending coins from some accounts to other accounts. */
    MultiSend(request: MsgMultiSend): Promise<MsgMultiSendResponse>;
}
export declare class MsgClientImpl implements Msg {
    private readonly rpc;
    constructor(rpc: Rpc);
    Send(request: MsgSend): Promise<MsgSendResponse>;
    MultiSend(request: MsgMultiSend): Promise<MsgMultiSendResponse>;
}
interface Rpc {
    request(service: string, method: string, data: Uint8Array): Promise<Uint8Array>;
}
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
