/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.dex";

/** Params defines the parameters for the module. */
export interface Params {
  priceSnapshotRetention: number;
  sudoCallGasPrice: string;
  beginBlockGasLimit: number;
  endBlockGasLimit: number;
  defaultGasPerOrder: number;
  defaultGasPerCancel: number;
  minRentDeposit: number;
  gasAllowancePerSettlement: number;
  minProcessableRent: number;
  orderBookEntriesPerLoad: number;
  contractUnsuspendCost: number;
  maxOrderPerPrice: number;
  maxPairsPerContract: number;
  defaultGasPerOrderDataByte: number;
}

const baseParams: object = {
  priceSnapshotRetention: 0,
  sudoCallGasPrice: "",
  beginBlockGasLimit: 0,
  endBlockGasLimit: 0,
  defaultGasPerOrder: 0,
  defaultGasPerCancel: 0,
  minRentDeposit: 0,
  gasAllowancePerSettlement: 0,
  minProcessableRent: 0,
  orderBookEntriesPerLoad: 0,
  contractUnsuspendCost: 0,
  maxOrderPerPrice: 0,
  maxPairsPerContract: 0,
  defaultGasPerOrderDataByte: 0,
};

export const Params = {
  encode(message: Params, writer: Writer = Writer.create()): Writer {
    if (message.priceSnapshotRetention !== 0) {
      writer.uint32(8).uint64(message.priceSnapshotRetention);
    }
    if (message.sudoCallGasPrice !== "") {
      writer.uint32(18).string(message.sudoCallGasPrice);
    }
    if (message.beginBlockGasLimit !== 0) {
      writer.uint32(24).uint64(message.beginBlockGasLimit);
    }
    if (message.endBlockGasLimit !== 0) {
      writer.uint32(32).uint64(message.endBlockGasLimit);
    }
    if (message.defaultGasPerOrder !== 0) {
      writer.uint32(40).uint64(message.defaultGasPerOrder);
    }
    if (message.defaultGasPerCancel !== 0) {
      writer.uint32(48).uint64(message.defaultGasPerCancel);
    }
    if (message.minRentDeposit !== 0) {
      writer.uint32(56).uint64(message.minRentDeposit);
    }
    if (message.gasAllowancePerSettlement !== 0) {
      writer.uint32(64).uint64(message.gasAllowancePerSettlement);
    }
    if (message.minProcessableRent !== 0) {
      writer.uint32(72).uint64(message.minProcessableRent);
    }
    if (message.orderBookEntriesPerLoad !== 0) {
      writer.uint32(80).uint64(message.orderBookEntriesPerLoad);
    }
    if (message.contractUnsuspendCost !== 0) {
      writer.uint32(88).uint64(message.contractUnsuspendCost);
    }
    if (message.maxOrderPerPrice !== 0) {
      writer.uint32(96).uint64(message.maxOrderPerPrice);
    }
    if (message.maxPairsPerContract !== 0) {
      writer.uint32(104).uint64(message.maxPairsPerContract);
    }
    if (message.defaultGasPerOrderDataByte !== 0) {
      writer.uint32(112).uint64(message.defaultGasPerOrderDataByte);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Params {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseParams } as Params;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.priceSnapshotRetention = longToNumber(
            reader.uint64() as Long
          );
          break;
        case 2:
          message.sudoCallGasPrice = reader.string();
          break;
        case 3:
          message.beginBlockGasLimit = longToNumber(reader.uint64() as Long);
          break;
        case 4:
          message.endBlockGasLimit = longToNumber(reader.uint64() as Long);
          break;
        case 5:
          message.defaultGasPerOrder = longToNumber(reader.uint64() as Long);
          break;
        case 6:
          message.defaultGasPerCancel = longToNumber(reader.uint64() as Long);
          break;
        case 7:
          message.minRentDeposit = longToNumber(reader.uint64() as Long);
          break;
        case 8:
          message.gasAllowancePerSettlement = longToNumber(
            reader.uint64() as Long
          );
          break;
        case 9:
          message.minProcessableRent = longToNumber(reader.uint64() as Long);
          break;
        case 10:
          message.orderBookEntriesPerLoad = longToNumber(
            reader.uint64() as Long
          );
          break;
        case 11:
          message.contractUnsuspendCost = longToNumber(reader.uint64() as Long);
          break;
        case 12:
          message.maxOrderPerPrice = longToNumber(reader.uint64() as Long);
          break;
        case 13:
          message.maxPairsPerContract = longToNumber(reader.uint64() as Long);
          break;
        case 14:
          message.defaultGasPerOrderDataByte = longToNumber(
            reader.uint64() as Long
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Params {
    const message = { ...baseParams } as Params;
    if (
      object.priceSnapshotRetention !== undefined &&
      object.priceSnapshotRetention !== null
    ) {
      message.priceSnapshotRetention = Number(object.priceSnapshotRetention);
    } else {
      message.priceSnapshotRetention = 0;
    }
    if (
      object.sudoCallGasPrice !== undefined &&
      object.sudoCallGasPrice !== null
    ) {
      message.sudoCallGasPrice = String(object.sudoCallGasPrice);
    } else {
      message.sudoCallGasPrice = "";
    }
    if (
      object.beginBlockGasLimit !== undefined &&
      object.beginBlockGasLimit !== null
    ) {
      message.beginBlockGasLimit = Number(object.beginBlockGasLimit);
    } else {
      message.beginBlockGasLimit = 0;
    }
    if (
      object.endBlockGasLimit !== undefined &&
      object.endBlockGasLimit !== null
    ) {
      message.endBlockGasLimit = Number(object.endBlockGasLimit);
    } else {
      message.endBlockGasLimit = 0;
    }
    if (
      object.defaultGasPerOrder !== undefined &&
      object.defaultGasPerOrder !== null
    ) {
      message.defaultGasPerOrder = Number(object.defaultGasPerOrder);
    } else {
      message.defaultGasPerOrder = 0;
    }
    if (
      object.defaultGasPerCancel !== undefined &&
      object.defaultGasPerCancel !== null
    ) {
      message.defaultGasPerCancel = Number(object.defaultGasPerCancel);
    } else {
      message.defaultGasPerCancel = 0;
    }
    if (object.minRentDeposit !== undefined && object.minRentDeposit !== null) {
      message.minRentDeposit = Number(object.minRentDeposit);
    } else {
      message.minRentDeposit = 0;
    }
    if (
      object.gasAllowancePerSettlement !== undefined &&
      object.gasAllowancePerSettlement !== null
    ) {
      message.gasAllowancePerSettlement = Number(
        object.gasAllowancePerSettlement
      );
    } else {
      message.gasAllowancePerSettlement = 0;
    }
    if (
      object.minProcessableRent !== undefined &&
      object.minProcessableRent !== null
    ) {
      message.minProcessableRent = Number(object.minProcessableRent);
    } else {
      message.minProcessableRent = 0;
    }
    if (
      object.orderBookEntriesPerLoad !== undefined &&
      object.orderBookEntriesPerLoad !== null
    ) {
      message.orderBookEntriesPerLoad = Number(object.orderBookEntriesPerLoad);
    } else {
      message.orderBookEntriesPerLoad = 0;
    }
    if (
      object.contractUnsuspendCost !== undefined &&
      object.contractUnsuspendCost !== null
    ) {
      message.contractUnsuspendCost = Number(object.contractUnsuspendCost);
    } else {
      message.contractUnsuspendCost = 0;
    }
    if (
      object.maxOrderPerPrice !== undefined &&
      object.maxOrderPerPrice !== null
    ) {
      message.maxOrderPerPrice = Number(object.maxOrderPerPrice);
    } else {
      message.maxOrderPerPrice = 0;
    }
    if (
      object.maxPairsPerContract !== undefined &&
      object.maxPairsPerContract !== null
    ) {
      message.maxPairsPerContract = Number(object.maxPairsPerContract);
    } else {
      message.maxPairsPerContract = 0;
    }
    if (
      object.defaultGasPerOrderDataByte !== undefined &&
      object.defaultGasPerOrderDataByte !== null
    ) {
      message.defaultGasPerOrderDataByte = Number(
        object.defaultGasPerOrderDataByte
      );
    } else {
      message.defaultGasPerOrderDataByte = 0;
    }
    return message;
  },

  toJSON(message: Params): unknown {
    const obj: any = {};
    message.priceSnapshotRetention !== undefined &&
      (obj.priceSnapshotRetention = message.priceSnapshotRetention);
    message.sudoCallGasPrice !== undefined &&
      (obj.sudoCallGasPrice = message.sudoCallGasPrice);
    message.beginBlockGasLimit !== undefined &&
      (obj.beginBlockGasLimit = message.beginBlockGasLimit);
    message.endBlockGasLimit !== undefined &&
      (obj.endBlockGasLimit = message.endBlockGasLimit);
    message.defaultGasPerOrder !== undefined &&
      (obj.defaultGasPerOrder = message.defaultGasPerOrder);
    message.defaultGasPerCancel !== undefined &&
      (obj.defaultGasPerCancel = message.defaultGasPerCancel);
    message.minRentDeposit !== undefined &&
      (obj.minRentDeposit = message.minRentDeposit);
    message.gasAllowancePerSettlement !== undefined &&
      (obj.gasAllowancePerSettlement = message.gasAllowancePerSettlement);
    message.minProcessableRent !== undefined &&
      (obj.minProcessableRent = message.minProcessableRent);
    message.orderBookEntriesPerLoad !== undefined &&
      (obj.orderBookEntriesPerLoad = message.orderBookEntriesPerLoad);
    message.contractUnsuspendCost !== undefined &&
      (obj.contractUnsuspendCost = message.contractUnsuspendCost);
    message.maxOrderPerPrice !== undefined &&
      (obj.maxOrderPerPrice = message.maxOrderPerPrice);
    message.maxPairsPerContract !== undefined &&
      (obj.maxPairsPerContract = message.maxPairsPerContract);
    message.defaultGasPerOrderDataByte !== undefined &&
      (obj.defaultGasPerOrderDataByte = message.defaultGasPerOrderDataByte);
    return obj;
  },

  fromPartial(object: DeepPartial<Params>): Params {
    const message = { ...baseParams } as Params;
    if (
      object.priceSnapshotRetention !== undefined &&
      object.priceSnapshotRetention !== null
    ) {
      message.priceSnapshotRetention = object.priceSnapshotRetention;
    } else {
      message.priceSnapshotRetention = 0;
    }
    if (
      object.sudoCallGasPrice !== undefined &&
      object.sudoCallGasPrice !== null
    ) {
      message.sudoCallGasPrice = object.sudoCallGasPrice;
    } else {
      message.sudoCallGasPrice = "";
    }
    if (
      object.beginBlockGasLimit !== undefined &&
      object.beginBlockGasLimit !== null
    ) {
      message.beginBlockGasLimit = object.beginBlockGasLimit;
    } else {
      message.beginBlockGasLimit = 0;
    }
    if (
      object.endBlockGasLimit !== undefined &&
      object.endBlockGasLimit !== null
    ) {
      message.endBlockGasLimit = object.endBlockGasLimit;
    } else {
      message.endBlockGasLimit = 0;
    }
    if (
      object.defaultGasPerOrder !== undefined &&
      object.defaultGasPerOrder !== null
    ) {
      message.defaultGasPerOrder = object.defaultGasPerOrder;
    } else {
      message.defaultGasPerOrder = 0;
    }
    if (
      object.defaultGasPerCancel !== undefined &&
      object.defaultGasPerCancel !== null
    ) {
      message.defaultGasPerCancel = object.defaultGasPerCancel;
    } else {
      message.defaultGasPerCancel = 0;
    }
    if (object.minRentDeposit !== undefined && object.minRentDeposit !== null) {
      message.minRentDeposit = object.minRentDeposit;
    } else {
      message.minRentDeposit = 0;
    }
    if (
      object.gasAllowancePerSettlement !== undefined &&
      object.gasAllowancePerSettlement !== null
    ) {
      message.gasAllowancePerSettlement = object.gasAllowancePerSettlement;
    } else {
      message.gasAllowancePerSettlement = 0;
    }
    if (
      object.minProcessableRent !== undefined &&
      object.minProcessableRent !== null
    ) {
      message.minProcessableRent = object.minProcessableRent;
    } else {
      message.minProcessableRent = 0;
    }
    if (
      object.orderBookEntriesPerLoad !== undefined &&
      object.orderBookEntriesPerLoad !== null
    ) {
      message.orderBookEntriesPerLoad = object.orderBookEntriesPerLoad;
    } else {
      message.orderBookEntriesPerLoad = 0;
    }
    if (
      object.contractUnsuspendCost !== undefined &&
      object.contractUnsuspendCost !== null
    ) {
      message.contractUnsuspendCost = object.contractUnsuspendCost;
    } else {
      message.contractUnsuspendCost = 0;
    }
    if (
      object.maxOrderPerPrice !== undefined &&
      object.maxOrderPerPrice !== null
    ) {
      message.maxOrderPerPrice = object.maxOrderPerPrice;
    } else {
      message.maxOrderPerPrice = 0;
    }
    if (
      object.maxPairsPerContract !== undefined &&
      object.maxPairsPerContract !== null
    ) {
      message.maxPairsPerContract = object.maxPairsPerContract;
    } else {
      message.maxPairsPerContract = 0;
    }
    if (
      object.defaultGasPerOrderDataByte !== undefined &&
      object.defaultGasPerOrderDataByte !== null
    ) {
      message.defaultGasPerOrderDataByte = object.defaultGasPerOrderDataByte;
    } else {
      message.defaultGasPerOrderDataByte = 0;
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
