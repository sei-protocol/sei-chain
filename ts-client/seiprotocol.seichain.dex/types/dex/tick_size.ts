/* eslint-disable */
import { Pair } from "../dex/pair";
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.dex";

export interface TickSize {
  pair: Pair | undefined;
  ticksize: string;
  contractAddr: string;
}

const baseTickSize: object = { ticksize: "", contractAddr: "" };

export const TickSize = {
  encode(message: TickSize, writer: Writer = Writer.create()): Writer {
    if (message.pair !== undefined) {
      Pair.encode(message.pair, writer.uint32(10).fork()).ldelim();
    }
    if (message.ticksize !== "") {
      writer.uint32(18).string(message.ticksize);
    }
    if (message.contractAddr !== "") {
      writer.uint32(26).string(message.contractAddr);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): TickSize {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseTickSize } as TickSize;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.pair = Pair.decode(reader, reader.uint32());
          break;
        case 2:
          message.ticksize = reader.string();
          break;
        case 3:
          message.contractAddr = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): TickSize {
    const message = { ...baseTickSize } as TickSize;
    if (object.pair !== undefined && object.pair !== null) {
      message.pair = Pair.fromJSON(object.pair);
    } else {
      message.pair = undefined;
    }
    if (object.ticksize !== undefined && object.ticksize !== null) {
      message.ticksize = String(object.ticksize);
    } else {
      message.ticksize = "";
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    return message;
  },

  toJSON(message: TickSize): unknown {
    const obj: any = {};
    message.pair !== undefined &&
      (obj.pair = message.pair ? Pair.toJSON(message.pair) : undefined);
    message.ticksize !== undefined && (obj.ticksize = message.ticksize);
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    return obj;
  },

  fromPartial(object: DeepPartial<TickSize>): TickSize {
    const message = { ...baseTickSize } as TickSize;
    if (object.pair !== undefined && object.pair !== null) {
      message.pair = Pair.fromPartial(object.pair);
    } else {
      message.pair = undefined;
    }
    if (object.ticksize !== undefined && object.ticksize !== null) {
      message.ticksize = object.ticksize;
    } else {
      message.ticksize = "";
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
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
