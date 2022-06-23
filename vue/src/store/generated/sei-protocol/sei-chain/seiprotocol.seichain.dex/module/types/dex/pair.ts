/* eslint-disable */
import { Denom, denomFromJSON, denomToJSON } from "../dex/enums";
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.dex";

export interface Pair {
  priceDenom: Denom;
  assetDenom: Denom;
}

const basePair: object = { priceDenom: 0, assetDenom: 0 };

export const Pair = {
  encode(message: Pair, writer: Writer = Writer.create()): Writer {
    if (message.priceDenom !== 0) {
      writer.uint32(8).int32(message.priceDenom);
    }
    if (message.assetDenom !== 0) {
      writer.uint32(16).int32(message.assetDenom);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Pair {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePair } as Pair;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.priceDenom = reader.int32() as any;
          break;
        case 2:
          message.assetDenom = reader.int32() as any;
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Pair {
    const message = { ...basePair } as Pair;
    if (object.priceDenom !== undefined && object.priceDenom !== null) {
      message.priceDenom = denomFromJSON(object.priceDenom);
    } else {
      message.priceDenom = 0;
    }
    if (object.assetDenom !== undefined && object.assetDenom !== null) {
      message.assetDenom = denomFromJSON(object.assetDenom);
    } else {
      message.assetDenom = 0;
    }
    return message;
  },

  toJSON(message: Pair): unknown {
    const obj: any = {};
    message.priceDenom !== undefined &&
      (obj.priceDenom = denomToJSON(message.priceDenom));
    message.assetDenom !== undefined &&
      (obj.assetDenom = denomToJSON(message.assetDenom));
    return obj;
  },

  fromPartial(object: DeepPartial<Pair>): Pair {
    const message = { ...basePair } as Pair;
    if (object.priceDenom !== undefined && object.priceDenom !== null) {
      message.priceDenom = object.priceDenom;
    } else {
      message.priceDenom = 0;
    }
    if (object.assetDenom !== undefined && object.assetDenom !== null) {
      message.assetDenom = object.assetDenom;
    } else {
      message.assetDenom = 0;
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
