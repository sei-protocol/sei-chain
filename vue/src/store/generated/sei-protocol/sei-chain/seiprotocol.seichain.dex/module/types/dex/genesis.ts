/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Params } from "../dex/params";
import { LongBook } from "../dex/long_book";
import { ShortBook } from "../dex/short_book";
import { Twap } from "../dex/twap";
import { TickSize } from "../dex/tick_size";

export const protobufPackage = "seiprotocol.seichain.dex";

/** GenesisState defines the dex module's genesis state. */
export interface GenesisState {
  params: Params | undefined;
  longBookList: LongBook[];
  shortBookList: ShortBook[];
  twapList: Twap[];
  /** if null, then no restriction, todo(zw) should set it to not nullable? */
  tickSizeList: TickSize[];
  /** this line is used by starport scaffolding # genesis/proto/state */
  lastEpoch: number;
}

const baseGenesisState: object = { lastEpoch: 0 };

export const GenesisState = {
  encode(message: GenesisState, writer: Writer = Writer.create()): Writer {
    if (message.params !== undefined) {
      Params.encode(message.params, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.longBookList) {
      LongBook.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.shortBookList) {
      ShortBook.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    for (const v of message.twapList) {
      Twap.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    for (const v of message.tickSizeList) {
      TickSize.encode(v!, writer.uint32(42).fork()).ldelim();
    }
    if (message.lastEpoch !== 0) {
      writer.uint32(48).uint64(message.lastEpoch);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisState } as GenesisState;
    message.longBookList = [];
    message.shortBookList = [];
    message.twapList = [];
    message.tickSizeList = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.params = Params.decode(reader, reader.uint32());
          break;
        case 2:
          message.longBookList.push(LongBook.decode(reader, reader.uint32()));
          break;
        case 3:
          message.shortBookList.push(ShortBook.decode(reader, reader.uint32()));
          break;
        case 4:
          message.twapList.push(Twap.decode(reader, reader.uint32()));
          break;
        case 5:
          message.tickSizeList.push(TickSize.decode(reader, reader.uint32()));
          break;
        case 6:
          message.lastEpoch = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.longBookList = [];
    message.shortBookList = [];
    message.twapList = [];
    message.tickSizeList = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromJSON(object.params);
    } else {
      message.params = undefined;
    }
    if (object.longBookList !== undefined && object.longBookList !== null) {
      for (const e of object.longBookList) {
        message.longBookList.push(LongBook.fromJSON(e));
      }
    }
    if (object.shortBookList !== undefined && object.shortBookList !== null) {
      for (const e of object.shortBookList) {
        message.shortBookList.push(ShortBook.fromJSON(e));
      }
    }
    if (object.twapList !== undefined && object.twapList !== null) {
      for (const e of object.twapList) {
        message.twapList.push(Twap.fromJSON(e));
      }
    }
    if (object.tickSizeList !== undefined && object.tickSizeList !== null) {
      for (const e of object.tickSizeList) {
        message.tickSizeList.push(TickSize.fromJSON(e));
      }
    }
    if (object.lastEpoch !== undefined && object.lastEpoch !== null) {
      message.lastEpoch = Number(object.lastEpoch);
    } else {
      message.lastEpoch = 0;
    }
    return message;
  },

  toJSON(message: GenesisState): unknown {
    const obj: any = {};
    message.params !== undefined &&
      (obj.params = message.params ? Params.toJSON(message.params) : undefined);
    if (message.longBookList) {
      obj.longBookList = message.longBookList.map((e) =>
        e ? LongBook.toJSON(e) : undefined
      );
    } else {
      obj.longBookList = [];
    }
    if (message.shortBookList) {
      obj.shortBookList = message.shortBookList.map((e) =>
        e ? ShortBook.toJSON(e) : undefined
      );
    } else {
      obj.shortBookList = [];
    }
    if (message.twapList) {
      obj.twapList = message.twapList.map((e) =>
        e ? Twap.toJSON(e) : undefined
      );
    } else {
      obj.twapList = [];
    }
    if (message.tickSizeList) {
      obj.tickSizeList = message.tickSizeList.map((e) =>
        e ? TickSize.toJSON(e) : undefined
      );
    } else {
      obj.tickSizeList = [];
    }
    message.lastEpoch !== undefined && (obj.lastEpoch = message.lastEpoch);
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisState>): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.longBookList = [];
    message.shortBookList = [];
    message.twapList = [];
    message.tickSizeList = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromPartial(object.params);
    } else {
      message.params = undefined;
    }
    if (object.longBookList !== undefined && object.longBookList !== null) {
      for (const e of object.longBookList) {
        message.longBookList.push(LongBook.fromPartial(e));
      }
    }
    if (object.shortBookList !== undefined && object.shortBookList !== null) {
      for (const e of object.shortBookList) {
        message.shortBookList.push(ShortBook.fromPartial(e));
      }
    }
    if (object.twapList !== undefined && object.twapList !== null) {
      for (const e of object.twapList) {
        message.twapList.push(Twap.fromPartial(e));
      }
    }
    if (object.tickSizeList !== undefined && object.tickSizeList !== null) {
      for (const e of object.tickSizeList) {
        message.tickSizeList.push(TickSize.fromPartial(e));
      }
    }
    if (object.lastEpoch !== undefined && object.lastEpoch !== null) {
      message.lastEpoch = object.lastEpoch;
    } else {
      message.lastEpoch = 0;
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
