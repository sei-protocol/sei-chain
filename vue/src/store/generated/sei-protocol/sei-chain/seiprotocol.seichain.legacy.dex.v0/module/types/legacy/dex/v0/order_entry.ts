/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.legacy.dex.v0";

export interface OrderEntry {
  price: number;
  quantity: number;
  allocationCreator: string[];
  allocation: number[];
  priceDenom: string;
  assetDenom: string;
}

const baseOrderEntry: object = {
  price: 0,
  quantity: 0,
  allocationCreator: "",
  allocation: 0,
  priceDenom: "",
  assetDenom: "",
};

export const OrderEntry = {
  encode(message: OrderEntry, writer: Writer = Writer.create()): Writer {
    if (message.price !== 0) {
      writer.uint32(8).uint64(message.price);
    }
    if (message.quantity !== 0) {
      writer.uint32(16).uint64(message.quantity);
    }
    for (const v of message.allocationCreator) {
      writer.uint32(26).string(v!);
    }
    writer.uint32(34).fork();
    for (const v of message.allocation) {
      writer.uint64(v);
    }
    writer.ldelim();
    if (message.priceDenom !== "") {
      writer.uint32(42).string(message.priceDenom);
    }
    if (message.assetDenom !== "") {
      writer.uint32(50).string(message.assetDenom);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): OrderEntry {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseOrderEntry } as OrderEntry;
    message.allocationCreator = [];
    message.allocation = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.price = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.quantity = longToNumber(reader.uint64() as Long);
          break;
        case 3:
          message.allocationCreator.push(reader.string());
          break;
        case 4:
          if ((tag & 7) === 2) {
            const end2 = reader.uint32() + reader.pos;
            while (reader.pos < end2) {
              message.allocation.push(longToNumber(reader.uint64() as Long));
            }
          } else {
            message.allocation.push(longToNumber(reader.uint64() as Long));
          }
          break;
        case 5:
          message.priceDenom = reader.string();
          break;
        case 6:
          message.assetDenom = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): OrderEntry {
    const message = { ...baseOrderEntry } as OrderEntry;
    message.allocationCreator = [];
    message.allocation = [];
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
    if (
      object.allocationCreator !== undefined &&
      object.allocationCreator !== null
    ) {
      for (const e of object.allocationCreator) {
        message.allocationCreator.push(String(e));
      }
    }
    if (object.allocation !== undefined && object.allocation !== null) {
      for (const e of object.allocation) {
        message.allocation.push(Number(e));
      }
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

  toJSON(message: OrderEntry): unknown {
    const obj: any = {};
    message.price !== undefined && (obj.price = message.price);
    message.quantity !== undefined && (obj.quantity = message.quantity);
    if (message.allocationCreator) {
      obj.allocationCreator = message.allocationCreator.map((e) => e);
    } else {
      obj.allocationCreator = [];
    }
    if (message.allocation) {
      obj.allocation = message.allocation.map((e) => e);
    } else {
      obj.allocation = [];
    }
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    return obj;
  },

  fromPartial(object: DeepPartial<OrderEntry>): OrderEntry {
    const message = { ...baseOrderEntry } as OrderEntry;
    message.allocationCreator = [];
    message.allocation = [];
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
    if (
      object.allocationCreator !== undefined &&
      object.allocationCreator !== null
    ) {
      for (const e of object.allocationCreator) {
        message.allocationCreator.push(e);
      }
    }
    if (object.allocation !== undefined && object.allocation !== null) {
      for (const e of object.allocation) {
        message.allocation.push(e);
      }
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
