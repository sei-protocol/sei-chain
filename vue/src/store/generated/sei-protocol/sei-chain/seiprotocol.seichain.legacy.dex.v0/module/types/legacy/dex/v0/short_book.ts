/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { OrderEntry } from "../../../legacy/dex/v0/order_entry";

export const protobufPackage = "seiprotocol.seichain.legacy.dex.v0";

export interface ShortBook {
  id: number;
  entry: OrderEntry | undefined;
}

const baseShortBook: object = { id: 0 };

export const ShortBook = {
  encode(message: ShortBook, writer: Writer = Writer.create()): Writer {
    if (message.id !== 0) {
      writer.uint32(8).uint64(message.id);
    }
    if (message.entry !== undefined) {
      OrderEntry.encode(message.entry, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ShortBook {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseShortBook } as ShortBook;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.id = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.entry = OrderEntry.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ShortBook {
    const message = { ...baseShortBook } as ShortBook;
    if (object.id !== undefined && object.id !== null) {
      message.id = Number(object.id);
    } else {
      message.id = 0;
    }
    if (object.entry !== undefined && object.entry !== null) {
      message.entry = OrderEntry.fromJSON(object.entry);
    } else {
      message.entry = undefined;
    }
    return message;
  },

  toJSON(message: ShortBook): unknown {
    const obj: any = {};
    message.id !== undefined && (obj.id = message.id);
    message.entry !== undefined &&
      (obj.entry = message.entry
        ? OrderEntry.toJSON(message.entry)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<ShortBook>): ShortBook {
    const message = { ...baseShortBook } as ShortBook;
    if (object.id !== undefined && object.id !== null) {
      message.id = object.id;
    } else {
      message.id = 0;
    }
    if (object.entry !== undefined && object.entry !== null) {
      message.entry = OrderEntry.fromPartial(object.entry);
    } else {
      message.entry = undefined;
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
