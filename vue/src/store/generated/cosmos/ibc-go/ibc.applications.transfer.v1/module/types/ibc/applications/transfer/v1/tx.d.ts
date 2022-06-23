import { Reader, Writer } from "protobufjs/minimal";
import { Coin } from "../../../../cosmos/base/v1beta1/coin";
import { Height } from "../../../../ibc/core/client/v1/client";
export declare const protobufPackage = "ibc.applications.transfer.v1";
/**
 * MsgTransfer defines a msg to transfer fungible tokens (i.e Coins) between
 * ICS20 enabled chains. See ICS Spec here:
 * https://github.com/cosmos/ibc/tree/master/spec/app/ics-020-fungible-token-transfer#data-structures
 */
export interface MsgTransfer {
    /** the port on which the packet will be sent */
    sourcePort: string;
    /** the channel by which the packet will be sent */
    sourceChannel: string;
    /** the tokens to be transferred */
    token: Coin | undefined;
    /** the sender address */
    sender: string;
    /** the recipient address on the destination chain */
    receiver: string;
    /**
     * Timeout height relative to the current block height.
     * The timeout is disabled when set to 0.
     */
    timeoutHeight: Height | undefined;
    /**
     * Timeout timestamp in absolute nanoseconds since unix epoch.
     * The timeout is disabled when set to 0.
     */
    timeoutTimestamp: number;
}
/** MsgTransferResponse defines the Msg/Transfer response type. */
export interface MsgTransferResponse {
}
export declare const MsgTransfer: {
    encode(message: MsgTransfer, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgTransfer;
    fromJSON(object: any): MsgTransfer;
    toJSON(message: MsgTransfer): unknown;
    fromPartial(object: DeepPartial<MsgTransfer>): MsgTransfer;
};
export declare const MsgTransferResponse: {
    encode(_: MsgTransferResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgTransferResponse;
    fromJSON(_: any): MsgTransferResponse;
    toJSON(_: MsgTransferResponse): unknown;
    fromPartial(_: DeepPartial<MsgTransferResponse>): MsgTransferResponse;
};
/** Msg defines the ibc/transfer Msg service. */
export interface Msg {
    /** Transfer defines a rpc handler method for MsgTransfer. */
    Transfer(request: MsgTransfer): Promise<MsgTransferResponse>;
}
export declare class MsgClientImpl implements Msg {
    private readonly rpc;
    constructor(rpc: Rpc);
    Transfer(request: MsgTransfer): Promise<MsgTransferResponse>;
}
interface Rpc {
    request(service: string, method: string, data: Uint8Array): Promise<Uint8Array>;
}
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
