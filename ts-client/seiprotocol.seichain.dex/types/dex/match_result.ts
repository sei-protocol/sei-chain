/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Order, Cancellation } from "../dex/order";
import { SettlementEntry } from "../dex/settlement";

export const protobufPackage = "seiprotocol.seichain.dex";

export interface MatchResult {
  height: number;
  contractAddr: string;
  orders: Order[];
  settlements: SettlementEntry[];
  cancellations: Cancellation[];
}

const baseMatchResult: object = { height: 0, contractAddr: "" };

export const MatchResult = {
  encode(message: MatchResult, writer: Writer = Writer.create()): Writer {
    if (message.height !== 0) {
      writer.uint32(8).int64(message.height);
    }
    if (message.contractAddr !== "") {
      writer.uint32(18).string(message.contractAddr);
    }
    for (const v of message.orders) {
      Order.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    for (const v of message.settlements) {
      SettlementEntry.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    for (const v of message.cancellations) {
      Cancellation.encode(v!, writer.uint32(42).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MatchResult {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMatchResult } as MatchResult;
    message.orders = [];
    message.settlements = [];
    message.cancellations = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.height = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.contractAddr = reader.string();
          break;
        case 3:
          message.orders.push(Order.decode(reader, reader.uint32()));
          break;
        case 4:
          message.settlements.push(
            SettlementEntry.decode(reader, reader.uint32())
          );
          break;
        case 5:
          message.cancellations.push(
            Cancellation.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MatchResult {
    const message = { ...baseMatchResult } as MatchResult;
    message.orders = [];
    message.settlements = [];
    message.cancellations = [];
    if (object.height !== undefined && object.height !== null) {
      message.height = Number(object.height);
    } else {
      message.height = 0;
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    if (object.orders !== undefined && object.orders !== null) {
      for (const e of object.orders) {
        message.orders.push(Order.fromJSON(e));
      }
    }
    if (object.settlements !== undefined && object.settlements !== null) {
      for (const e of object.settlements) {
        message.settlements.push(SettlementEntry.fromJSON(e));
      }
    }
    if (object.cancellations !== undefined && object.cancellations !== null) {
      for (const e of object.cancellations) {
        message.cancellations.push(Cancellation.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: MatchResult): unknown {
    const obj: any = {};
    message.height !== undefined && (obj.height = message.height);
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    if (message.orders) {
      obj.orders = message.orders.map((e) => (e ? Order.toJSON(e) : undefined));
    } else {
      obj.orders = [];
    }
    if (message.settlements) {
      obj.settlements = message.settlements.map((e) =>
        e ? SettlementEntry.toJSON(e) : undefined
      );
    } else {
      obj.settlements = [];
    }
    if (message.cancellations) {
      obj.cancellations = message.cancellations.map((e) =>
        e ? Cancellation.toJSON(e) : undefined
      );
    } else {
      obj.cancellations = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<MatchResult>): MatchResult {
    const message = { ...baseMatchResult } as MatchResult;
    message.orders = [];
    message.settlements = [];
    message.cancellations = [];
    if (object.height !== undefined && object.height !== null) {
      message.height = object.height;
    } else {
      message.height = 0;
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    if (object.orders !== undefined && object.orders !== null) {
      for (const e of object.orders) {
        message.orders.push(Order.fromPartial(e));
      }
    }
    if (object.settlements !== undefined && object.settlements !== null) {
      for (const e of object.settlements) {
        message.settlements.push(SettlementEntry.fromPartial(e));
      }
    }
    if (object.cancellations !== undefined && object.cancellations !== null) {
      for (const e of object.cancellations) {
        message.cancellations.push(Cancellation.fromPartial(e));
      }
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
