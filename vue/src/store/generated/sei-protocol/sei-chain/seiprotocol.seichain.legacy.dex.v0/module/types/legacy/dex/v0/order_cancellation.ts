/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.legacy.dex.v0";

export interface OrderCancellation {
  long: boolean;
  price: number;
  quantity: number;
  priceDenom: string;
  assetDenom: string;
  open: boolean;
  leverage: string;
}

const baseOrderCancellation: object = {
  long: false,
  price: 0,
  quantity: 0,
  priceDenom: "",
  assetDenom: "",
  open: false,
  leverage: "",
};

export const OrderCancellation = {
  encode(message: OrderCancellation, writer: Writer = Writer.create()): Writer {
    if (message.long === true) {
      writer.uint32(8).bool(message.long);
    }
    if (message.price !== 0) {
      writer.uint32(16).uint64(message.price);
    }
    if (message.quantity !== 0) {
      writer.uint32(24).uint64(message.quantity);
    }
    if (message.priceDenom !== "") {
      writer.uint32(34).string(message.priceDenom);
    }
    if (message.assetDenom !== "") {
      writer.uint32(42).string(message.assetDenom);
    }
    if (message.open === true) {
      writer.uint32(48).bool(message.open);
    }
    if (message.leverage !== "") {
      writer.uint32(58).string(message.leverage);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): OrderCancellation {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseOrderCancellation } as OrderCancellation;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.long = reader.bool();
          break;
        case 2:
          message.price = longToNumber(reader.uint64() as Long);
          break;
        case 3:
          message.quantity = longToNumber(reader.uint64() as Long);
          break;
        case 4:
          message.priceDenom = reader.string();
          break;
        case 5:
          message.assetDenom = reader.string();
          break;
        case 6:
          message.open = reader.bool();
          break;
        case 7:
          message.leverage = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): OrderCancellation {
    const message = { ...baseOrderCancellation } as OrderCancellation;
    if (object.long !== undefined && object.long !== null) {
      message.long = Boolean(object.long);
    } else {
      message.long = false;
    }
    if (object.price !== undefined && object.price !== null) {
      message.price = Number(object.price);
    } else {
      message.price = 0;
    }
    if (object.quantity !== undefined && object.quantity !== null) {
      message.quantity = Number(object.quantity);
    } else {
      message.quantity = 0;
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
    if (object.open !== undefined && object.open !== null) {
      message.open = Boolean(object.open);
    } else {
      message.open = false;
    }
    if (object.leverage !== undefined && object.leverage !== null) {
      message.leverage = String(object.leverage);
    } else {
      message.leverage = "";
    }
    return message;
  },

  toJSON(message: OrderCancellation): unknown {
    const obj: any = {};
    message.long !== undefined && (obj.long = message.long);
    message.price !== undefined && (obj.price = message.price);
    message.quantity !== undefined && (obj.quantity = message.quantity);
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    message.open !== undefined && (obj.open = message.open);
    message.leverage !== undefined && (obj.leverage = message.leverage);
    return obj;
  },

  fromPartial(object: DeepPartial<OrderCancellation>): OrderCancellation {
    const message = { ...baseOrderCancellation } as OrderCancellation;
    if (object.long !== undefined && object.long !== null) {
      message.long = object.long;
    } else {
      message.long = false;
    }
    if (object.price !== undefined && object.price !== null) {
      message.price = object.price;
    } else {
      message.price = 0;
    }
    if (object.quantity !== undefined && object.quantity !== null) {
      message.quantity = object.quantity;
    } else {
      message.quantity = 0;
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
    if (object.open !== undefined && object.open !== null) {
      message.open = object.open;
    } else {
      message.open = false;
    }
    if (object.leverage !== undefined && object.leverage !== null) {
      message.leverage = object.leverage;
    } else {
      message.leverage = "";
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
