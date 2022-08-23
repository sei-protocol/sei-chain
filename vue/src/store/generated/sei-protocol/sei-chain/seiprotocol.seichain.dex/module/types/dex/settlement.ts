/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.dex";

export interface SettlementEntry {
  account: string;
  priceDenom: string;
  assetDenom: string;
  quantity: string;
  executionCostOrProceed: string;
  expectedCostOrProceed: string;
  positionDirection: string;
  orderType: string;
  orderId: number;
  timestamp: number;
  height: number;
  settlementId: number;
}

export interface Settlements {
  epoch: number;
  entries: SettlementEntry[];
}

const baseSettlementEntry: object = {
  account: "",
  priceDenom: "",
  assetDenom: "",
  quantity: "",
  executionCostOrProceed: "",
  expectedCostOrProceed: "",
  positionDirection: "",
  orderType: "",
  orderId: 0,
  timestamp: 0,
  height: 0,
  settlementId: 0,
};

export const SettlementEntry = {
  encode(message: SettlementEntry, writer: Writer = Writer.create()): Writer {
    if (message.account !== "") {
      writer.uint32(10).string(message.account);
    }
    if (message.priceDenom !== "") {
      writer.uint32(18).string(message.priceDenom);
    }
    if (message.assetDenom !== "") {
      writer.uint32(26).string(message.assetDenom);
    }
    if (message.quantity !== "") {
      writer.uint32(34).string(message.quantity);
    }
    if (message.executionCostOrProceed !== "") {
      writer.uint32(42).string(message.executionCostOrProceed);
    }
    if (message.expectedCostOrProceed !== "") {
      writer.uint32(50).string(message.expectedCostOrProceed);
    }
    if (message.positionDirection !== "") {
      writer.uint32(58).string(message.positionDirection);
    }
    if (message.orderType !== "") {
      writer.uint32(66).string(message.orderType);
    }
    if (message.orderId !== 0) {
      writer.uint32(72).uint64(message.orderId);
    }
    if (message.timestamp !== 0) {
      writer.uint32(80).uint64(message.timestamp);
    }
    if (message.height !== 0) {
      writer.uint32(88).uint64(message.height);
    }
    if (message.settlementId !== 0) {
      writer.uint32(96).uint64(message.settlementId);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): SettlementEntry {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseSettlementEntry } as SettlementEntry;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.account = reader.string();
          break;
        case 2:
          message.priceDenom = reader.string();
          break;
        case 3:
          message.assetDenom = reader.string();
          break;
        case 4:
          message.quantity = reader.string();
          break;
        case 5:
          message.executionCostOrProceed = reader.string();
          break;
        case 6:
          message.expectedCostOrProceed = reader.string();
          break;
        case 7:
          message.positionDirection = reader.string();
          break;
        case 8:
          message.orderType = reader.string();
          break;
        case 9:
          message.orderId = longToNumber(reader.uint64() as Long);
          break;
        case 10:
          message.timestamp = longToNumber(reader.uint64() as Long);
          break;
        case 11:
          message.height = longToNumber(reader.uint64() as Long);
          break;
        case 12:
          message.settlementId = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): SettlementEntry {
    const message = { ...baseSettlementEntry } as SettlementEntry;
    if (object.account !== undefined && object.account !== null) {
      message.account = String(object.account);
    } else {
      message.account = "";
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
    if (object.quantity !== undefined && object.quantity !== null) {
      message.quantity = String(object.quantity);
    } else {
      message.quantity = "";
    }
    if (
      object.executionCostOrProceed !== undefined &&
      object.executionCostOrProceed !== null
    ) {
      message.executionCostOrProceed = String(object.executionCostOrProceed);
    } else {
      message.executionCostOrProceed = "";
    }
    if (
      object.expectedCostOrProceed !== undefined &&
      object.expectedCostOrProceed !== null
    ) {
      message.expectedCostOrProceed = String(object.expectedCostOrProceed);
    } else {
      message.expectedCostOrProceed = "";
    }
    if (
      object.positionDirection !== undefined &&
      object.positionDirection !== null
    ) {
      message.positionDirection = String(object.positionDirection);
    } else {
      message.positionDirection = "";
    }
    if (object.orderType !== undefined && object.orderType !== null) {
      message.orderType = String(object.orderType);
    } else {
      message.orderType = "";
    }
    if (object.orderId !== undefined && object.orderId !== null) {
      message.orderId = Number(object.orderId);
    } else {
      message.orderId = 0;
    }
    if (object.timestamp !== undefined && object.timestamp !== null) {
      message.timestamp = Number(object.timestamp);
    } else {
      message.timestamp = 0;
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = Number(object.height);
    } else {
      message.height = 0;
    }
    if (object.settlementId !== undefined && object.settlementId !== null) {
      message.settlementId = Number(object.settlementId);
    } else {
      message.settlementId = 0;
    }
    return message;
  },

  toJSON(message: SettlementEntry): unknown {
    const obj: any = {};
    message.account !== undefined && (obj.account = message.account);
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    message.quantity !== undefined && (obj.quantity = message.quantity);
    message.executionCostOrProceed !== undefined &&
      (obj.executionCostOrProceed = message.executionCostOrProceed);
    message.expectedCostOrProceed !== undefined &&
      (obj.expectedCostOrProceed = message.expectedCostOrProceed);
    message.positionDirection !== undefined &&
      (obj.positionDirection = message.positionDirection);
    message.orderType !== undefined && (obj.orderType = message.orderType);
    message.orderId !== undefined && (obj.orderId = message.orderId);
    message.timestamp !== undefined && (obj.timestamp = message.timestamp);
    message.height !== undefined && (obj.height = message.height);
    message.settlementId !== undefined &&
      (obj.settlementId = message.settlementId);
    return obj;
  },

  fromPartial(object: DeepPartial<SettlementEntry>): SettlementEntry {
    const message = { ...baseSettlementEntry } as SettlementEntry;
    if (object.account !== undefined && object.account !== null) {
      message.account = object.account;
    } else {
      message.account = "";
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
    if (object.quantity !== undefined && object.quantity !== null) {
      message.quantity = object.quantity;
    } else {
      message.quantity = "";
    }
    if (
      object.executionCostOrProceed !== undefined &&
      object.executionCostOrProceed !== null
    ) {
      message.executionCostOrProceed = object.executionCostOrProceed;
    } else {
      message.executionCostOrProceed = "";
    }
    if (
      object.expectedCostOrProceed !== undefined &&
      object.expectedCostOrProceed !== null
    ) {
      message.expectedCostOrProceed = object.expectedCostOrProceed;
    } else {
      message.expectedCostOrProceed = "";
    }
    if (
      object.positionDirection !== undefined &&
      object.positionDirection !== null
    ) {
      message.positionDirection = object.positionDirection;
    } else {
      message.positionDirection = "";
    }
    if (object.orderType !== undefined && object.orderType !== null) {
      message.orderType = object.orderType;
    } else {
      message.orderType = "";
    }
    if (object.orderId !== undefined && object.orderId !== null) {
      message.orderId = object.orderId;
    } else {
      message.orderId = 0;
    }
    if (object.timestamp !== undefined && object.timestamp !== null) {
      message.timestamp = object.timestamp;
    } else {
      message.timestamp = 0;
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = object.height;
    } else {
      message.height = 0;
    }
    if (object.settlementId !== undefined && object.settlementId !== null) {
      message.settlementId = object.settlementId;
    } else {
      message.settlementId = 0;
    }
    return message;
  },
};

const baseSettlements: object = { epoch: 0 };

export const Settlements = {
  encode(message: Settlements, writer: Writer = Writer.create()): Writer {
    if (message.epoch !== 0) {
      writer.uint32(8).int64(message.epoch);
    }
    for (const v of message.entries) {
      SettlementEntry.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Settlements {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseSettlements } as Settlements;
    message.entries = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.epoch = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.entries.push(SettlementEntry.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Settlements {
    const message = { ...baseSettlements } as Settlements;
    message.entries = [];
    if (object.epoch !== undefined && object.epoch !== null) {
      message.epoch = Number(object.epoch);
    } else {
      message.epoch = 0;
    }
    if (object.entries !== undefined && object.entries !== null) {
      for (const e of object.entries) {
        message.entries.push(SettlementEntry.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: Settlements): unknown {
    const obj: any = {};
    message.epoch !== undefined && (obj.epoch = message.epoch);
    if (message.entries) {
      obj.entries = message.entries.map((e) =>
        e ? SettlementEntry.toJSON(e) : undefined
      );
    } else {
      obj.entries = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<Settlements>): Settlements {
    const message = { ...baseSettlements } as Settlements;
    message.entries = [];
    if (object.epoch !== undefined && object.epoch !== null) {
      message.epoch = object.epoch;
    } else {
      message.epoch = 0;
    }
    if (object.entries !== undefined && object.entries !== null) {
      for (const e of object.entries) {
        message.entries.push(SettlementEntry.fromPartial(e));
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
