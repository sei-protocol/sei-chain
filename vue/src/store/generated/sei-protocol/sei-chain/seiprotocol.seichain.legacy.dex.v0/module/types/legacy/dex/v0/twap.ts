/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.legacy.dex.v0";

export interface Twap {
  lastEpoch: number;
  prices: number[];
  twapPrice: number;
  priceDenom: string;
  assetDenom: string;
}

const baseTwap: object = {
  lastEpoch: 0,
  prices: 0,
  twapPrice: 0,
  priceDenom: "",
  assetDenom: "",
};

export const Twap = {
  encode(message: Twap, writer: Writer = Writer.create()): Writer {
    if (message.lastEpoch !== 0) {
      writer.uint32(8).uint64(message.lastEpoch);
    }
    writer.uint32(18).fork();
    for (const v of message.prices) {
      writer.uint64(v);
    }
    writer.ldelim();
    if (message.twapPrice !== 0) {
      writer.uint32(24).uint64(message.twapPrice);
    }
    if (message.priceDenom !== "") {
      writer.uint32(34).string(message.priceDenom);
    }
    if (message.assetDenom !== "") {
      writer.uint32(42).string(message.assetDenom);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Twap {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseTwap } as Twap;
    message.prices = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.lastEpoch = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          if ((tag & 7) === 2) {
            const end2 = reader.uint32() + reader.pos;
            while (reader.pos < end2) {
              message.prices.push(longToNumber(reader.uint64() as Long));
            }
          } else {
            message.prices.push(longToNumber(reader.uint64() as Long));
          }
          break;
        case 3:
          message.twapPrice = longToNumber(reader.uint64() as Long);
          break;
        case 4:
          message.priceDenom = reader.string();
          break;
        case 5:
          message.assetDenom = reader.string();
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
    message.prices = [];
    if (object.lastEpoch !== undefined && object.lastEpoch !== null) {
      message.lastEpoch = Number(object.lastEpoch);
    } else {
      message.lastEpoch = 0;
    }
    if (object.prices !== undefined && object.prices !== null) {
      for (const e of object.prices) {
        message.prices.push(Number(e));
      }
    }
    if (object.twapPrice !== undefined && object.twapPrice !== null) {
      message.twapPrice = Number(object.twapPrice);
    } else {
      message.twapPrice = 0;
    }
    if (object.priceDenom !== undefined && object.priceDenom !== null) {
      message.priceDenom = String(object.priceDenom);
    } else {
      message.priceDenom = "";
    }
    if (object.assetDenom !== undefined && object.assetDenom !== null) {
      message.assetDenom = String(object.assetDenom);
    } else {
      message.assetDenom = "";
    }
    return message;
  },

  toJSON(message: Twap): unknown {
    const obj: any = {};
    message.lastEpoch !== undefined && (obj.lastEpoch = message.lastEpoch);
    if (message.prices) {
      obj.prices = message.prices.map((e) => e);
    } else {
      obj.prices = [];
    }
    message.twapPrice !== undefined && (obj.twapPrice = message.twapPrice);
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    return obj;
  },

  fromPartial(object: DeepPartial<Twap>): Twap {
    const message = { ...baseTwap } as Twap;
    message.prices = [];
    if (object.lastEpoch !== undefined && object.lastEpoch !== null) {
      message.lastEpoch = object.lastEpoch;
    } else {
      message.lastEpoch = 0;
    }
    if (object.prices !== undefined && object.prices !== null) {
      for (const e of object.prices) {
        message.prices.push(e);
      }
    }
    if (object.twapPrice !== undefined && object.twapPrice !== null) {
      message.twapPrice = object.twapPrice;
    } else {
      message.twapPrice = 0;
    }
    if (object.priceDenom !== undefined && object.priceDenom !== null) {
      message.priceDenom = object.priceDenom;
    } else {
      message.priceDenom = "";
    }
    if (object.assetDenom !== undefined && object.assetDenom !== null) {
      message.assetDenom = object.assetDenom;
    } else {
      message.assetDenom = "";
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
