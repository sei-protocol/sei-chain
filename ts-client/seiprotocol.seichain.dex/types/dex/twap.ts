/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Pair } from "../dex/pair";

export const protobufPackage = "seiprotocol.seichain.dex";

export interface Twap {
  pair: Pair | undefined;
  twap: string;
  lookbackSeconds: number;
}

const baseTwap: object = { twap: "", lookbackSeconds: 0 };

export const Twap = {
  encode(message: Twap, writer: Writer = Writer.create()): Writer {
    if (message.pair !== undefined) {
      Pair.encode(message.pair, writer.uint32(10).fork()).ldelim();
    }
    if (message.twap !== "") {
      writer.uint32(18).string(message.twap);
    }
    if (message.lookbackSeconds !== 0) {
      writer.uint32(24).uint64(message.lookbackSeconds);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Twap {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseTwap } as Twap;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.pair = Pair.decode(reader, reader.uint32());
          break;
        case 2:
          message.twap = reader.string();
          break;
        case 3:
          message.lookbackSeconds = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Twap {
    const message = { ...baseTwap } as Twap;
    if (object.pair !== undefined && object.pair !== null) {
      message.pair = Pair.fromJSON(object.pair);
    } else {
      message.pair = undefined;
    }
    if (object.twap !== undefined && object.twap !== null) {
      message.twap = String(object.twap);
    } else {
      message.twap = "";
    }
    if (
      object.lookbackSeconds !== undefined &&
      object.lookbackSeconds !== null
    ) {
      message.lookbackSeconds = Number(object.lookbackSeconds);
    } else {
      message.lookbackSeconds = 0;
    }
    return message;
  },

  toJSON(message: Twap): unknown {
    const obj: any = {};
    message.pair !== undefined &&
      (obj.pair = message.pair ? Pair.toJSON(message.pair) : undefined);
    message.twap !== undefined && (obj.twap = message.twap);
    message.lookbackSeconds !== undefined &&
      (obj.lookbackSeconds = message.lookbackSeconds);
    return obj;
  },

  fromPartial(object: DeepPartial<Twap>): Twap {
    const message = { ...baseTwap } as Twap;
    if (object.pair !== undefined && object.pair !== null) {
      message.pair = Pair.fromPartial(object.pair);
    } else {
      message.pair = undefined;
    }
    if (object.twap !== undefined && object.twap !== null) {
      message.twap = object.twap;
    } else {
      message.twap = "";
    }
    if (
      object.lookbackSeconds !== undefined &&
      object.lookbackSeconds !== null
    ) {
      message.lookbackSeconds = object.lookbackSeconds;
    } else {
      message.lookbackSeconds = 0;
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
