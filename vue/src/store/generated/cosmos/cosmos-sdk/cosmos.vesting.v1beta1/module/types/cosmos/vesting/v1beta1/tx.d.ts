import { Reader, Writer } from "protobufjs/minimal";
import { Coin } from "../../../cosmos/base/v1beta1/coin";
export declare const protobufPackage = "cosmos.vesting.v1beta1";
/**
 * MsgCreateVestingAccount defines a message that enables creating a vesting
 * account.
 */
export interface MsgCreateVestingAccount {
    fromAddress: string;
    toAddress: string;
    amount: Coin[];
    endTime: number;
    delayed: boolean;
}
/** MsgCreateVestingAccountResponse defines the Msg/CreateVestingAccount response type. */
export interface MsgCreateVestingAccountResponse {
}
export declare const MsgCreateVestingAccount: {
    encode(message: MsgCreateVestingAccount, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgCreateVestingAccount;
    fromJSON(object: any): MsgCreateVestingAccount;
    toJSON(message: MsgCreateVestingAccount): unknown;
    fromPartial(object: DeepPartial<MsgCreateVestingAccount>): MsgCreateVestingAccount;
};
export declare const MsgCreateVestingAccountResponse: {
    encode(_: MsgCreateVestingAccountResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgCreateVestingAccountResponse;
    fromJSON(_: any): MsgCreateVestingAccountResponse;
    toJSON(_: MsgCreateVestingAccountResponse): unknown;
    fromPartial(_: DeepPartial<MsgCreateVestingAccountResponse>): MsgCreateVestingAccountResponse;
};
/** Msg defines the bank Msg service. */
export interface Msg {
    /**
     * CreateVestingAccount defines a method that enables creating a vesting
     * account.
     */
    CreateVestingAccount(request: MsgCreateVestingAccount): Promise<MsgCreateVestingAccountResponse>;
}
export declare class MsgClientImpl implements Msg {
    private readonly rpc;
    constructor(rpc: Rpc);
    CreateVestingAccount(request: MsgCreateVestingAccount): Promise<MsgCreateVestingAccountResponse>;
}
interface Rpc {
    request(service: string, method: string, data: Uint8Array): Promise<Uint8Array>;
}
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
