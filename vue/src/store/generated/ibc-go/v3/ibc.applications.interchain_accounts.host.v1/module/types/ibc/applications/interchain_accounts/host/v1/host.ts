/* eslint-disable */
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "ibc.applications.interchain_accounts.host.v1";

/**
 * Params defines the set of on-chain interchain accounts parameters.
 * The following parameters may be used to disable the host submodule.
 */
export interface Params {
  /** host_enabled enables or disables the host submodule. */
  host_enabled: boolean;
  /** allow_messages defines a list of sdk message typeURLs allowed to be executed on a host chain. */
  allow_messages: string[];
}

const baseParams: object = { host_enabled: false, allow_messages: "" };

export const Params = {
  encode(message: Params, writer: Writer = Writer.create()): Writer {
    if (message.host_enabled === true) {
      writer.uint32(8).bool(message.host_enabled);
    }
    for (const v of message.allow_messages) {
      writer.uint32(18).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Params {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseParams } as Params;
    message.allow_messages = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.host_enabled = reader.bool();
          break;
        case 2:
          message.allow_messages.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Params {
    const message = { ...baseParams } as Params;
    message.allow_messages = [];
    if (object.host_enabled !== undefined && object.host_enabled !== null) {
      message.host_enabled = Boolean(object.host_enabled);
    } else {
      message.host_enabled = false;
    }
    if (object.allow_messages !== undefined && object.allow_messages !== null) {
      for (const e of object.allow_messages) {
        message.allow_messages.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: Params): unknown {
    const obj: any = {};
    message.host_enabled !== undefined &&
      (obj.host_enabled = message.host_enabled);
    if (message.allow_messages) {
      obj.allow_messages = message.allow_messages.map((e) => e);
    } else {
      obj.allow_messages = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<Params>): Params {
    const message = { ...baseParams } as Params;
    message.allow_messages = [];
    if (object.host_enabled !== undefined && object.host_enabled !== null) {
      message.host_enabled = object.host_enabled;
    } else {
      message.host_enabled = false;
    }
    if (object.allow_messages !== undefined && object.allow_messages !== null) {
      for (const e of object.allow_messages) {
        message.allow_messages.push(e);
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
