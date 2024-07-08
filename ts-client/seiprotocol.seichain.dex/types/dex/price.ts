/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Pair } from "../dex/pair";

export const protobufPackage = "seiprotocol.seichain.dex";

export interface Price {
  snapshotTimestampInSeconds: number;
  price: string;
  pair: Pair | undefined;
}

export interface PriceCandlestick {
  beginTimestamp: number;
  endTimestamp: number;
  open: string;
  high: string;
  low: string;
  close: string;
  volume: string;
}

const basePrice: object = { snapshotTimestampInSeconds: 0, price: "" };

export const Price = {
  encode(message: Price, writer: Writer = Writer.create()): Writer {
    if (message.snapshotTimestampInSeconds !== 0) {
      writer.uint32(8).uint64(message.snapshotTimestampInSeconds);
    }
    if (message.price !== "") {
      writer.uint32(18).string(message.price);
    }
    if (message.pair !== undefined) {
      Pair.encode(message.pair, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Price {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePrice } as Price;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.snapshotTimestampInSeconds = longToNumber(
            reader.uint64() as Long
          );
          break;
        case 2:
          message.price = reader.string();
          break;
        case 3:
          message.pair = Pair.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Price {
    const message = { ...basePrice } as Price;
    if (
      object.snapshotTimestampInSeconds !== undefined &&
      object.snapshotTimestampInSeconds !== null
    ) {
      message.snapshotTimestampInSeconds = Number(
        object.snapshotTimestampInSeconds
      );
    } else {
      message.snapshotTimestampInSeconds = 0;
    }
    if (object.price !== undefined && object.price !== null) {
      message.price = String(object.price);
    } else {
      message.price = "";
    }
    if (object.pair !== undefined && object.pair !== null) {
      message.pair = Pair.fromJSON(object.pair);
    } else {
      message.pair = undefined;
    }
    return message;
  },

  toJSON(message: Price): unknown {
    const obj: any = {};
    message.snapshotTimestampInSeconds !== undefined &&
      (obj.snapshotTimestampInSeconds = message.snapshotTimestampInSeconds);
    message.price !== undefined && (obj.price = message.price);
    message.pair !== undefined &&
      (obj.pair = message.pair ? Pair.toJSON(message.pair) : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<Price>): Price {
    const message = { ...basePrice } as Price;
    if (
      object.snapshotTimestampInSeconds !== undefined &&
      object.snapshotTimestampInSeconds !== null
    ) {
      message.snapshotTimestampInSeconds = object.snapshotTimestampInSeconds;
    } else {
      message.snapshotTimestampInSeconds = 0;
    }
    if (object.price !== undefined && object.price !== null) {
      message.price = object.price;
    } else {
      message.price = "";
    }
    if (object.pair !== undefined && object.pair !== null) {
      message.pair = Pair.fromPartial(object.pair);
    } else {
      message.pair = undefined;
    }
    return message;
  },
};

const basePriceCandlestick: object = {
  beginTimestamp: 0,
  endTimestamp: 0,
  open: "",
  high: "",
  low: "",
  close: "",
  volume: "",
};

export const PriceCandlestick = {
  encode(message: PriceCandlestick, writer: Writer = Writer.create()): Writer {
    if (message.beginTimestamp !== 0) {
      writer.uint32(8).uint64(message.beginTimestamp);
    }
    if (message.endTimestamp !== 0) {
      writer.uint32(16).uint64(message.endTimestamp);
    }
    if (message.open !== "") {
      writer.uint32(26).string(message.open);
    }
    if (message.high !== "") {
      writer.uint32(34).string(message.high);
    }
    if (message.low !== "") {
      writer.uint32(42).string(message.low);
    }
    if (message.close !== "") {
      writer.uint32(50).string(message.close);
    }
    if (message.volume !== "") {
      writer.uint32(58).string(message.volume);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): PriceCandlestick {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePriceCandlestick } as PriceCandlestick;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.beginTimestamp = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.endTimestamp = longToNumber(reader.uint64() as Long);
          break;
        case 3:
          message.open = reader.string();
          break;
        case 4:
          message.high = reader.string();
          break;
        case 5:
          message.low = reader.string();
          break;
        case 6:
          message.close = reader.string();
          break;
        case 7:
          message.volume = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): PriceCandlestick {
    const message = { ...basePriceCandlestick } as PriceCandlestick;
    if (object.beginTimestamp !== undefined && object.beginTimestamp !== null) {
      message.beginTimestamp = Number(object.beginTimestamp);
    } else {
      message.beginTimestamp = 0;
    }
    if (object.endTimestamp !== undefined && object.endTimestamp !== null) {
      message.endTimestamp = Number(object.endTimestamp);
    } else {
      message.endTimestamp = 0;
    }
    if (object.open !== undefined && object.open !== null) {
      message.open = String(object.open);
    } else {
      message.open = "";
    }
    if (object.high !== undefined && object.high !== null) {
      message.high = String(object.high);
    } else {
      message.high = "";
    }
    if (object.low !== undefined && object.low !== null) {
      message.low = String(object.low);
    } else {
      message.low = "";
    }
    if (object.close !== undefined && object.close !== null) {
      message.close = String(object.close);
    } else {
      message.close = "";
    }
    if (object.volume !== undefined && object.volume !== null) {
      message.volume = String(object.volume);
    } else {
      message.volume = "";
    }
    return message;
  },

  toJSON(message: PriceCandlestick): unknown {
    const obj: any = {};
    message.beginTimestamp !== undefined &&
      (obj.beginTimestamp = message.beginTimestamp);
    message.endTimestamp !== undefined &&
      (obj.endTimestamp = message.endTimestamp);
    message.open !== undefined && (obj.open = message.open);
    message.high !== undefined && (obj.high = message.high);
    message.low !== undefined && (obj.low = message.low);
    message.close !== undefined && (obj.close = message.close);
    message.volume !== undefined && (obj.volume = message.volume);
    return obj;
  },

  fromPartial(object: DeepPartial<PriceCandlestick>): PriceCandlestick {
    const message = { ...basePriceCandlestick } as PriceCandlestick;
    if (object.beginTimestamp !== undefined && object.beginTimestamp !== null) {
      message.beginTimestamp = object.beginTimestamp;
    } else {
      message.beginTimestamp = 0;
    }
    if (object.endTimestamp !== undefined && object.endTimestamp !== null) {
      message.endTimestamp = object.endTimestamp;
    } else {
      message.endTimestamp = 0;
    }
    if (object.open !== undefined && object.open !== null) {
      message.open = object.open;
    } else {
      message.open = "";
    }
    if (object.high !== undefined && object.high !== null) {
      message.high = object.high;
    } else {
      message.high = "";
    }
    if (object.low !== undefined && object.low !== null) {
      message.low = object.low;
    } else {
      message.low = "";
    }
    if (object.close !== undefined && object.close !== null) {
      message.close = object.close;
    } else {
      message.close = "";
    }
    if (object.volume !== undefined && object.volume !== null) {
      message.volume = object.volume;
    } else {
      message.volume = "";
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
