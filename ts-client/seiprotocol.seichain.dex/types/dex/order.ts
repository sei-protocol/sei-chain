/* eslint-disable */
import {
  OrderStatus,
  OrderType,
  PositionDirection,
  CancellationInitiator,
  orderStatusFromJSON,
  orderTypeFromJSON,
  positionDirectionFromJSON,
  orderStatusToJSON,
  orderTypeToJSON,
  positionDirectionToJSON,
  cancellationInitiatorFromJSON,
  cancellationInitiatorToJSON,
} from "../dex/enums";
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.dex";

export interface Order {
  id: number;
  status: OrderStatus;
  account: string;
  contractAddr: string;
  price: string;
  quantity: string;
  priceDenom: string;
  assetDenom: string;
  orderType: OrderType;
  positionDirection: PositionDirection;
  data: string;
  statusDescription: string;
  nominal: string;
  triggerPrice: string;
  triggerStatus: boolean;
}

export interface Cancellation {
  id: number;
  initiator: CancellationInitiator;
  creator: string;
  contractAddr: string;
  priceDenom: string;
  assetDenom: string;
  positionDirection: PositionDirection;
  price: string;
}

export interface ActiveOrders {
  ids: number[];
}

const baseOrder: object = {
  id: 0,
  status: 0,
  account: "",
  contractAddr: "",
  price: "",
  quantity: "",
  priceDenom: "",
  assetDenom: "",
  orderType: 0,
  positionDirection: 0,
  data: "",
  statusDescription: "",
  nominal: "",
  triggerPrice: "",
  triggerStatus: false,
};

export const Order = {
  encode(message: Order, writer: Writer = Writer.create()): Writer {
    if (message.id !== 0) {
      writer.uint32(8).uint64(message.id);
    }
    if (message.status !== 0) {
      writer.uint32(16).int32(message.status);
    }
    if (message.account !== "") {
      writer.uint32(26).string(message.account);
    }
    if (message.contractAddr !== "") {
      writer.uint32(34).string(message.contractAddr);
    }
    if (message.price !== "") {
      writer.uint32(42).string(message.price);
    }
    if (message.quantity !== "") {
      writer.uint32(50).string(message.quantity);
    }
    if (message.priceDenom !== "") {
      writer.uint32(58).string(message.priceDenom);
    }
    if (message.assetDenom !== "") {
      writer.uint32(66).string(message.assetDenom);
    }
    if (message.orderType !== 0) {
      writer.uint32(72).int32(message.orderType);
    }
    if (message.positionDirection !== 0) {
      writer.uint32(80).int32(message.positionDirection);
    }
    if (message.data !== "") {
      writer.uint32(90).string(message.data);
    }
    if (message.statusDescription !== "") {
      writer.uint32(98).string(message.statusDescription);
    }
    if (message.nominal !== "") {
      writer.uint32(106).string(message.nominal);
    }
    if (message.triggerPrice !== "") {
      writer.uint32(114).string(message.triggerPrice);
    }
    if (message.triggerStatus === true) {
      writer.uint32(120).bool(message.triggerStatus);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Order {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseOrder } as Order;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.id = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.status = reader.int32() as any;
          break;
        case 3:
          message.account = reader.string();
          break;
        case 4:
          message.contractAddr = reader.string();
          break;
        case 5:
          message.price = reader.string();
          break;
        case 6:
          message.quantity = reader.string();
          break;
        case 7:
          message.priceDenom = reader.string();
          break;
        case 8:
          message.assetDenom = reader.string();
          break;
        case 9:
          message.orderType = reader.int32() as any;
          break;
        case 10:
          message.positionDirection = reader.int32() as any;
          break;
        case 11:
          message.data = reader.string();
          break;
        case 12:
          message.statusDescription = reader.string();
          break;
        case 13:
          message.nominal = reader.string();
          break;
        case 14:
          message.triggerPrice = reader.string();
          break;
        case 15:
          message.triggerStatus = reader.bool();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Order {
    const message = { ...baseOrder } as Order;
    if (object.id !== undefined && object.id !== null) {
      message.id = Number(object.id);
    } else {
      message.id = 0;
    }
    if (object.status !== undefined && object.status !== null) {
      message.status = orderStatusFromJSON(object.status);
    } else {
      message.status = 0;
    }
    if (object.account !== undefined && object.account !== null) {
      message.account = String(object.account);
    } else {
      message.account = "";
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    if (object.price !== undefined && object.price !== null) {
      message.price = String(object.price);
    } else {
      message.price = "";
    }
    if (object.quantity !== undefined && object.quantity !== null) {
      message.quantity = String(object.quantity);
    } else {
      message.quantity = "";
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
    if (object.orderType !== undefined && object.orderType !== null) {
      message.orderType = orderTypeFromJSON(object.orderType);
    } else {
      message.orderType = 0;
    }
    if (
      object.positionDirection !== undefined &&
      object.positionDirection !== null
    ) {
      message.positionDirection = positionDirectionFromJSON(
        object.positionDirection
      );
    } else {
      message.positionDirection = 0;
    }
    if (object.data !== undefined && object.data !== null) {
      message.data = String(object.data);
    } else {
      message.data = "";
    }
    if (
      object.statusDescription !== undefined &&
      object.statusDescription !== null
    ) {
      message.statusDescription = String(object.statusDescription);
    } else {
      message.statusDescription = "";
    }
    if (object.nominal !== undefined && object.nominal !== null) {
      message.nominal = String(object.nominal);
    } else {
      message.nominal = "";
    }
    if (object.triggerPrice !== undefined && object.triggerPrice !== null) {
      message.triggerPrice = String(object.triggerPrice);
    } else {
      message.triggerPrice = "";
    }
    if (object.triggerStatus !== undefined && object.triggerStatus !== null) {
      message.triggerStatus = Boolean(object.triggerStatus);
    } else {
      message.triggerStatus = false;
    }
    return message;
  },

  toJSON(message: Order): unknown {
    const obj: any = {};
    message.id !== undefined && (obj.id = message.id);
    message.status !== undefined &&
      (obj.status = orderStatusToJSON(message.status));
    message.account !== undefined && (obj.account = message.account);
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    message.price !== undefined && (obj.price = message.price);
    message.quantity !== undefined && (obj.quantity = message.quantity);
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    message.orderType !== undefined &&
      (obj.orderType = orderTypeToJSON(message.orderType));
    message.positionDirection !== undefined &&
      (obj.positionDirection = positionDirectionToJSON(
        message.positionDirection
      ));
    message.data !== undefined && (obj.data = message.data);
    message.statusDescription !== undefined &&
      (obj.statusDescription = message.statusDescription);
    message.nominal !== undefined && (obj.nominal = message.nominal);
    message.triggerPrice !== undefined &&
      (obj.triggerPrice = message.triggerPrice);
    message.triggerStatus !== undefined &&
      (obj.triggerStatus = message.triggerStatus);
    return obj;
  },

  fromPartial(object: DeepPartial<Order>): Order {
    const message = { ...baseOrder } as Order;
    if (object.id !== undefined && object.id !== null) {
      message.id = object.id;
    } else {
      message.id = 0;
    }
    if (object.status !== undefined && object.status !== null) {
      message.status = object.status;
    } else {
      message.status = 0;
    }
    if (object.account !== undefined && object.account !== null) {
      message.account = object.account;
    } else {
      message.account = "";
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    if (object.price !== undefined && object.price !== null) {
      message.price = object.price;
    } else {
      message.price = "";
    }
    if (object.quantity !== undefined && object.quantity !== null) {
      message.quantity = object.quantity;
    } else {
      message.quantity = "";
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
    if (object.orderType !== undefined && object.orderType !== null) {
      message.orderType = object.orderType;
    } else {
      message.orderType = 0;
    }
    if (
      object.positionDirection !== undefined &&
      object.positionDirection !== null
    ) {
      message.positionDirection = object.positionDirection;
    } else {
      message.positionDirection = 0;
    }
    if (object.data !== undefined && object.data !== null) {
      message.data = object.data;
    } else {
      message.data = "";
    }
    if (
      object.statusDescription !== undefined &&
      object.statusDescription !== null
    ) {
      message.statusDescription = object.statusDescription;
    } else {
      message.statusDescription = "";
    }
    if (object.nominal !== undefined && object.nominal !== null) {
      message.nominal = object.nominal;
    } else {
      message.nominal = "";
    }
    if (object.triggerPrice !== undefined && object.triggerPrice !== null) {
      message.triggerPrice = object.triggerPrice;
    } else {
      message.triggerPrice = "";
    }
    if (object.triggerStatus !== undefined && object.triggerStatus !== null) {
      message.triggerStatus = object.triggerStatus;
    } else {
      message.triggerStatus = false;
    }
    return message;
  },
};

const baseCancellation: object = {
  id: 0,
  initiator: 0,
  creator: "",
  contractAddr: "",
  priceDenom: "",
  assetDenom: "",
  positionDirection: 0,
  price: "",
};

export const Cancellation = {
  encode(message: Cancellation, writer: Writer = Writer.create()): Writer {
    if (message.id !== 0) {
      writer.uint32(8).uint64(message.id);
    }
    if (message.initiator !== 0) {
      writer.uint32(16).int32(message.initiator);
    }
    if (message.creator !== "") {
      writer.uint32(26).string(message.creator);
    }
    if (message.contractAddr !== "") {
      writer.uint32(34).string(message.contractAddr);
    }
    if (message.priceDenom !== "") {
      writer.uint32(42).string(message.priceDenom);
    }
    if (message.assetDenom !== "") {
      writer.uint32(50).string(message.assetDenom);
    }
    if (message.positionDirection !== 0) {
      writer.uint32(56).int32(message.positionDirection);
    }
    if (message.price !== "") {
      writer.uint32(66).string(message.price);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Cancellation {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseCancellation } as Cancellation;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.id = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.initiator = reader.int32() as any;
          break;
        case 3:
          message.creator = reader.string();
          break;
        case 4:
          message.contractAddr = reader.string();
          break;
        case 5:
          message.priceDenom = reader.string();
          break;
        case 6:
          message.assetDenom = reader.string();
          break;
        case 7:
          message.positionDirection = reader.int32() as any;
          break;
        case 8:
          message.price = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Cancellation {
    const message = { ...baseCancellation } as Cancellation;
    if (object.id !== undefined && object.id !== null) {
      message.id = Number(object.id);
    } else {
      message.id = 0;
    }
    if (object.initiator !== undefined && object.initiator !== null) {
      message.initiator = cancellationInitiatorFromJSON(object.initiator);
    } else {
      message.initiator = 0;
    }
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = String(object.creator);
    } else {
      message.creator = "";
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
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
    if (
      object.positionDirection !== undefined &&
      object.positionDirection !== null
    ) {
      message.positionDirection = positionDirectionFromJSON(
        object.positionDirection
      );
    } else {
      message.positionDirection = 0;
    }
    if (object.price !== undefined && object.price !== null) {
      message.price = String(object.price);
    } else {
      message.price = "";
    }
    return message;
  },

  toJSON(message: Cancellation): unknown {
    const obj: any = {};
    message.id !== undefined && (obj.id = message.id);
    message.initiator !== undefined &&
      (obj.initiator = cancellationInitiatorToJSON(message.initiator));
    message.creator !== undefined && (obj.creator = message.creator);
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    message.positionDirection !== undefined &&
      (obj.positionDirection = positionDirectionToJSON(
        message.positionDirection
      ));
    message.price !== undefined && (obj.price = message.price);
    return obj;
  },

  fromPartial(object: DeepPartial<Cancellation>): Cancellation {
    const message = { ...baseCancellation } as Cancellation;
    if (object.id !== undefined && object.id !== null) {
      message.id = object.id;
    } else {
      message.id = 0;
    }
    if (object.initiator !== undefined && object.initiator !== null) {
      message.initiator = object.initiator;
    } else {
      message.initiator = 0;
    }
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = object.creator;
    } else {
      message.creator = "";
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
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
    if (
      object.positionDirection !== undefined &&
      object.positionDirection !== null
    ) {
      message.positionDirection = object.positionDirection;
    } else {
      message.positionDirection = 0;
    }
    if (object.price !== undefined && object.price !== null) {
      message.price = object.price;
    } else {
      message.price = "";
    }
    return message;
  },
};

const baseActiveOrders: object = { ids: 0 };

export const ActiveOrders = {
  encode(message: ActiveOrders, writer: Writer = Writer.create()): Writer {
    writer.uint32(10).fork();
    for (const v of message.ids) {
      writer.uint64(v);
    }
    writer.ldelim();
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ActiveOrders {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseActiveOrders } as ActiveOrders;
    message.ids = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          if ((tag & 7) === 2) {
            const end2 = reader.uint32() + reader.pos;
            while (reader.pos < end2) {
              message.ids.push(longToNumber(reader.uint64() as Long));
            }
          } else {
            message.ids.push(longToNumber(reader.uint64() as Long));
          }
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ActiveOrders {
    const message = { ...baseActiveOrders } as ActiveOrders;
    message.ids = [];
    if (object.ids !== undefined && object.ids !== null) {
      for (const e of object.ids) {
        message.ids.push(Number(e));
      }
    }
    return message;
  },

  toJSON(message: ActiveOrders): unknown {
    const obj: any = {};
    if (message.ids) {
      obj.ids = message.ids.map((e) => e);
    } else {
      obj.ids = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<ActiveOrders>): ActiveOrders {
    const message = { ...baseActiveOrders } as ActiveOrders;
    message.ids = [];
    if (object.ids !== undefined && object.ids !== null) {
      for (const e of object.ids) {
        message.ids.push(e);
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
