/* eslint-disable */
import { Coin } from "../../../cosmos/base/v1beta1/coin";
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "cosmos.bank.v1beta1";

/**
 * SendAuthorization allows the grantee to spend up to spend_limit coins from
 * the granter's account.
 *
 * Since: cosmos-sdk 0.43
 */
export interface SendAuthorization {
  spendLimit: Coin[];
}

const baseSendAuthorization: object = {};

export const SendAuthorization = {
  encode(message: SendAuthorization, writer: Writer = Writer.create()): Writer {
    for (const v of message.spendLimit) {
      Coin.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): SendAuthorization {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseSendAuthorization } as SendAuthorization;
    message.spendLimit = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.spendLimit.push(Coin.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): SendAuthorization {
    const message = { ...baseSendAuthorization } as SendAuthorization;
    message.spendLimit = [];
    if (object.spendLimit !== undefined && object.spendLimit !== null) {
      for (const e of object.spendLimit) {
        message.spendLimit.push(Coin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: SendAuthorization): unknown {
    const obj: any = {};
    if (message.spendLimit) {
      obj.spendLimit = message.spendLimit.map((e) =>
        e ? Coin.toJSON(e) : undefined
      );
    } else {
      obj.spendLimit = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<SendAuthorization>): SendAuthorization {
    const message = { ...baseSendAuthorization } as SendAuthorization;
    message.spendLimit = [];
    if (object.spendLimit !== undefined && object.spendLimit !== null) {
      for (const e of object.spendLimit) {
        message.spendLimit.push(Coin.fromPartial(e));
      }
    }
    return message;
  },
};

type Builtin = Date | Function | Uint8Array | string | number | undefined;
export type DeepPartial<T> = T extends Builtin
  ? T
  : T extends Array<infer U>
  ? Array<DeepPartial<U>>
  : T extends ReadonlyArray<infer U>
  ? ReadonlyArray<DeepPartial<U>>
  : T extends {}
  ? { [K in keyof T]?: DeepPartial<T[K]> }
  : Partial<T>;
