/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "sei.evm.v1";

/**
 * XXTime fields indicate upgrade timestamps. For example, a ShanghaiTime
 * of 42198537129 means the chain upgraded to the Shanghai version at timestamp 42198537129.
 * A value of 0 means the upgrade is included in the genesis of the EVM on Sei.
 * -1 means upgrade not reached yet.
 */
export interface ChainConfig {
  cancunTime: number;
  pragueTime: number;
  verkleTime: number;
}

const baseChainConfig: object = { cancunTime: 0, pragueTime: 0, verkleTime: 0 };

export const ChainConfig = {
  encode(message: ChainConfig, writer: Writer = Writer.create()): Writer {
    if (message.cancunTime !== 0) {
      writer.uint32(8).int64(message.cancunTime);
    }
    if (message.pragueTime !== 0) {
      writer.uint32(16).int64(message.pragueTime);
    }
    if (message.verkleTime !== 0) {
      writer.uint32(24).int64(message.verkleTime);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ChainConfig {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseChainConfig } as ChainConfig;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.cancunTime = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.pragueTime = longToNumber(reader.int64() as Long);
          break;
        case 3:
          message.verkleTime = longToNumber(reader.int64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ChainConfig {
    const message = { ...baseChainConfig } as ChainConfig;
    if (object.cancunTime !== undefined && object.cancunTime !== null) {
      message.cancunTime = Number(object.cancunTime);
    } else {
      message.cancunTime = 0;
    }
    if (object.pragueTime !== undefined && object.pragueTime !== null) {
      message.pragueTime = Number(object.pragueTime);
    } else {
      message.pragueTime = 0;
    }
    if (object.verkleTime !== undefined && object.verkleTime !== null) {
      message.verkleTime = Number(object.verkleTime);
    } else {
      message.verkleTime = 0;
    }
    return message;
  },

  toJSON(message: ChainConfig): unknown {
    const obj: any = {};
    message.cancunTime !== undefined && (obj.cancunTime = message.cancunTime);
    message.pragueTime !== undefined && (obj.pragueTime = message.pragueTime);
    message.verkleTime !== undefined && (obj.verkleTime = message.verkleTime);
    return obj;
  },

  fromPartial(object: DeepPartial<ChainConfig>): ChainConfig {
    const message = { ...baseChainConfig } as ChainConfig;
    if (object.cancunTime !== undefined && object.cancunTime !== null) {
      message.cancunTime = object.cancunTime;
    } else {
      message.cancunTime = 0;
    }
    if (object.pragueTime !== undefined && object.pragueTime !== null) {
      message.pragueTime = object.pragueTime;
    } else {
      message.pragueTime = 0;
    }
    if (object.verkleTime !== undefined && object.verkleTime !== null) {
      message.verkleTime = object.verkleTime;
    } else {
      message.verkleTime = 0;
    }
    return message;
  },
};

declare var self: any | undefined;
declare var window: any | undefined;
var globalThis: any = (() => {
  if (typeof globalThis !== "undefined") return globalThis;
  if (typeof self !== "undefined") return self;
  if (typeof window !== "undefined") return window;
  if (typeof global !== "undefined") return global;
  throw "Unable to locate global object";
})();

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

function longToNumber(long: Long): number {
  if (long.gt(Number.MAX_SAFE_INTEGER)) {
    throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
  }
  return long.toNumber();
}

if (util.Long !== Long) {
  util.Long = Long as any;
  configure();
}
