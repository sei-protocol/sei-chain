/* eslint-disable */
import { OrderEntry } from "../dex/order_entry";
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.dex";

export interface ShortBook {
  price: string;
  entry: OrderEntry | undefined;
}

const baseShortBook: object = { price: "" };

export const ShortBook = {
  encode(message: ShortBook, writer: Writer = Writer.create()): Writer {
    if (message.price !== "") {
      writer.uint32(10).string(message.price);
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
          message.price = reader.string();
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
    if (object.price !== undefined && object.price !== null) {
      message.price = String(object.price);
    } else {
      message.price = "";
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
    message.price !== undefined && (obj.price = message.price);
    message.entry !== undefined &&
      (obj.entry = message.entry
        ? OrderEntry.toJSON(message.entry)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<ShortBook>): ShortBook {
    const message = { ...baseShortBook } as ShortBook;
    if (object.price !== undefined && object.price !== null) {
      message.price = object.price;
    } else {
      message.price = "";
    }
    if (object.entry !== undefined && object.entry !== null) {
      message.entry = OrderEntry.fromPartial(object.entry);
    } else {
      message.entry = undefined;
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
