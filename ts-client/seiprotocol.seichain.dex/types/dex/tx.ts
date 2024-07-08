/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import { Order, Cancellation } from "../dex/order";
import { Coin } from "../cosmos/base/v1beta1/coin";
import { ContractInfoV2 } from "../dex/contract";
import { BatchContractPair } from "../dex/pair";
import { TickSize } from "../dex/tick_size";

export const protobufPackage = "seiprotocol.seichain.dex";

export interface MsgPlaceOrders {
  creator: string;
  orders: Order[];
  contractAddr: string;
  funds: Coin[];
}

export interface MsgPlaceOrdersResponse {
  orderIds: number[];
}

export interface MsgCancelOrders {
  creator: string;
  cancellations: Cancellation[];
  contractAddr: string;
}

export interface MsgCancelOrdersResponse {}

export interface MsgRegisterContract {
  creator: string;
  contract: ContractInfoV2 | undefined;
}

export interface MsgRegisterContractResponse {}

export interface MsgContractDepositRent {
  contractAddr: string;
  amount: number;
  sender: string;
}

export interface MsgContractDepositRentResponse {}

export interface MsgUnregisterContract {
  creator: string;
  contractAddr: string;
}

export interface MsgUnregisterContractResponse {}

export interface MsgRegisterPairs {
  creator: string;
  batchcontractpair: BatchContractPair[];
}

export interface MsgRegisterPairsResponse {}

export interface MsgUpdatePriceTickSize {
  creator: string;
  tickSizeList: TickSize[];
}

export interface MsgUpdateQuantityTickSize {
  creator: string;
  tickSizeList: TickSize[];
}

export interface MsgUpdateTickSizeResponse {}

export interface MsgUnsuspendContract {
  creator: string;
  contractAddr: string;
}

export interface MsgUnsuspendContractResponse {}

const baseMsgPlaceOrders: object = { creator: "", contractAddr: "" };

export const MsgPlaceOrders = {
  encode(message: MsgPlaceOrders, writer: Writer = Writer.create()): Writer {
    if (message.creator !== "") {
      writer.uint32(10).string(message.creator);
    }
    for (const v of message.orders) {
      Order.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    if (message.contractAddr !== "") {
      writer.uint32(26).string(message.contractAddr);
    }
    for (const v of message.funds) {
      Coin.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgPlaceOrders {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgPlaceOrders } as MsgPlaceOrders;
    message.orders = [];
    message.funds = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.creator = reader.string();
          break;
        case 2:
          message.orders.push(Order.decode(reader, reader.uint32()));
          break;
        case 3:
          message.contractAddr = reader.string();
          break;
        case 4:
          message.funds.push(Coin.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgPlaceOrders {
    const message = { ...baseMsgPlaceOrders } as MsgPlaceOrders;
    message.orders = [];
    message.funds = [];
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = String(object.creator);
    } else {
      message.creator = "";
    }
    if (object.orders !== undefined && object.orders !== null) {
      for (const e of object.orders) {
        message.orders.push(Order.fromJSON(e));
      }
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    if (object.funds !== undefined && object.funds !== null) {
      for (const e of object.funds) {
        message.funds.push(Coin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: MsgPlaceOrders): unknown {
    const obj: any = {};
    message.creator !== undefined && (obj.creator = message.creator);
    if (message.orders) {
      obj.orders = message.orders.map((e) => (e ? Order.toJSON(e) : undefined));
    } else {
      obj.orders = [];
    }
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    if (message.funds) {
      obj.funds = message.funds.map((e) => (e ? Coin.toJSON(e) : undefined));
    } else {
      obj.funds = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<MsgPlaceOrders>): MsgPlaceOrders {
    const message = { ...baseMsgPlaceOrders } as MsgPlaceOrders;
    message.orders = [];
    message.funds = [];
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = object.creator;
    } else {
      message.creator = "";
    }
    if (object.orders !== undefined && object.orders !== null) {
      for (const e of object.orders) {
        message.orders.push(Order.fromPartial(e));
      }
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    if (object.funds !== undefined && object.funds !== null) {
      for (const e of object.funds) {
        message.funds.push(Coin.fromPartial(e));
      }
    }
    return message;
  },
};

const baseMsgPlaceOrdersResponse: object = { orderIds: 0 };

export const MsgPlaceOrdersResponse = {
  encode(
    message: MsgPlaceOrdersResponse,
    writer: Writer = Writer.create()
  ): Writer {
    writer.uint32(10).fork();
    for (const v of message.orderIds) {
      writer.uint64(v);
    }
    writer.ldelim();
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgPlaceOrdersResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgPlaceOrdersResponse } as MsgPlaceOrdersResponse;
    message.orderIds = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          if ((tag & 7) === 2) {
            const end2 = reader.uint32() + reader.pos;
            while (reader.pos < end2) {
              message.orderIds.push(longToNumber(reader.uint64() as Long));
            }
          } else {
            message.orderIds.push(longToNumber(reader.uint64() as Long));
          }
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgPlaceOrdersResponse {
    const message = { ...baseMsgPlaceOrdersResponse } as MsgPlaceOrdersResponse;
    message.orderIds = [];
    if (object.orderIds !== undefined && object.orderIds !== null) {
      for (const e of object.orderIds) {
        message.orderIds.push(Number(e));
      }
    }
    return message;
  },

  toJSON(message: MsgPlaceOrdersResponse): unknown {
    const obj: any = {};
    if (message.orderIds) {
      obj.orderIds = message.orderIds.map((e) => e);
    } else {
      obj.orderIds = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgPlaceOrdersResponse>
  ): MsgPlaceOrdersResponse {
    const message = { ...baseMsgPlaceOrdersResponse } as MsgPlaceOrdersResponse;
    message.orderIds = [];
    if (object.orderIds !== undefined && object.orderIds !== null) {
      for (const e of object.orderIds) {
        message.orderIds.push(e);
      }
    }
    return message;
  },
};

const baseMsgCancelOrders: object = { creator: "", contractAddr: "" };

export const MsgCancelOrders = {
  encode(message: MsgCancelOrders, writer: Writer = Writer.create()): Writer {
    if (message.creator !== "") {
      writer.uint32(10).string(message.creator);
    }
    for (const v of message.cancellations) {
      Cancellation.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    if (message.contractAddr !== "") {
      writer.uint32(26).string(message.contractAddr);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgCancelOrders {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgCancelOrders } as MsgCancelOrders;
    message.cancellations = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.creator = reader.string();
          break;
        case 2:
          message.cancellations.push(
            Cancellation.decode(reader, reader.uint32())
          );
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

  fromJSON(object: any): MsgCancelOrders {
    const message = { ...baseMsgCancelOrders } as MsgCancelOrders;
    message.cancellations = [];
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = String(object.creator);
    } else {
      message.creator = "";
    }
    if (object.cancellations !== undefined && object.cancellations !== null) {
      for (const e of object.cancellations) {
        message.cancellations.push(Cancellation.fromJSON(e));
      }
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    return message;
  },

  toJSON(message: MsgCancelOrders): unknown {
    const obj: any = {};
    message.creator !== undefined && (obj.creator = message.creator);
    if (message.cancellations) {
      obj.cancellations = message.cancellations.map((e) =>
        e ? Cancellation.toJSON(e) : undefined
      );
    } else {
      obj.cancellations = [];
    }
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgCancelOrders>): MsgCancelOrders {
    const message = { ...baseMsgCancelOrders } as MsgCancelOrders;
    message.cancellations = [];
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = object.creator;
    } else {
      message.creator = "";
    }
    if (object.cancellations !== undefined && object.cancellations !== null) {
      for (const e of object.cancellations) {
        message.cancellations.push(Cancellation.fromPartial(e));
      }
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    return message;
  },
};

const baseMsgCancelOrdersResponse: object = {};

export const MsgCancelOrdersResponse = {
  encode(_: MsgCancelOrdersResponse, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgCancelOrdersResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgCancelOrdersResponse,
    } as MsgCancelOrdersResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(_: any): MsgCancelOrdersResponse {
    const message = {
      ...baseMsgCancelOrdersResponse,
    } as MsgCancelOrdersResponse;
    return message;
  },

  toJSON(_: MsgCancelOrdersResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgCancelOrdersResponse>
  ): MsgCancelOrdersResponse {
    const message = {
      ...baseMsgCancelOrdersResponse,
    } as MsgCancelOrdersResponse;
    return message;
  },
};

const baseMsgRegisterContract: object = { creator: "" };

export const MsgRegisterContract = {
  encode(
    message: MsgRegisterContract,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.creator !== "") {
      writer.uint32(10).string(message.creator);
    }
    if (message.contract !== undefined) {
      ContractInfoV2.encode(
        message.contract,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgRegisterContract {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgRegisterContract } as MsgRegisterContract;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.creator = reader.string();
          break;
        case 2:
          message.contract = ContractInfoV2.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgRegisterContract {
    const message = { ...baseMsgRegisterContract } as MsgRegisterContract;
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = String(object.creator);
    } else {
      message.creator = "";
    }
    if (object.contract !== undefined && object.contract !== null) {
      message.contract = ContractInfoV2.fromJSON(object.contract);
    } else {
      message.contract = undefined;
    }
    return message;
  },

  toJSON(message: MsgRegisterContract): unknown {
    const obj: any = {};
    message.creator !== undefined && (obj.creator = message.creator);
    message.contract !== undefined &&
      (obj.contract = message.contract
        ? ContractInfoV2.toJSON(message.contract)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgRegisterContract>): MsgRegisterContract {
    const message = { ...baseMsgRegisterContract } as MsgRegisterContract;
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = object.creator;
    } else {
      message.creator = "";
    }
    if (object.contract !== undefined && object.contract !== null) {
      message.contract = ContractInfoV2.fromPartial(object.contract);
    } else {
      message.contract = undefined;
    }
    return message;
  },
};

const baseMsgRegisterContractResponse: object = {};

export const MsgRegisterContractResponse = {
  encode(
    _: MsgRegisterContractResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgRegisterContractResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgRegisterContractResponse,
    } as MsgRegisterContractResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(_: any): MsgRegisterContractResponse {
    const message = {
      ...baseMsgRegisterContractResponse,
    } as MsgRegisterContractResponse;
    return message;
  },

  toJSON(_: MsgRegisterContractResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgRegisterContractResponse>
  ): MsgRegisterContractResponse {
    const message = {
      ...baseMsgRegisterContractResponse,
    } as MsgRegisterContractResponse;
    return message;
  },
};

const baseMsgContractDepositRent: object = {
  contractAddr: "",
  amount: 0,
  sender: "",
};

export const MsgContractDepositRent = {
  encode(
    message: MsgContractDepositRent,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.contractAddr !== "") {
      writer.uint32(10).string(message.contractAddr);
    }
    if (message.amount !== 0) {
      writer.uint32(16).uint64(message.amount);
    }
    if (message.sender !== "") {
      writer.uint32(26).string(message.sender);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgContractDepositRent {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgContractDepositRent } as MsgContractDepositRent;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.contractAddr = reader.string();
          break;
        case 2:
          message.amount = longToNumber(reader.uint64() as Long);
          break;
        case 3:
          message.sender = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgContractDepositRent {
    const message = { ...baseMsgContractDepositRent } as MsgContractDepositRent;
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    if (object.amount !== undefined && object.amount !== null) {
      message.amount = Number(object.amount);
    } else {
      message.amount = 0;
    }
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = String(object.sender);
    } else {
      message.sender = "";
    }
    return message;
  },

  toJSON(message: MsgContractDepositRent): unknown {
    const obj: any = {};
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    message.amount !== undefined && (obj.amount = message.amount);
    message.sender !== undefined && (obj.sender = message.sender);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgContractDepositRent>
  ): MsgContractDepositRent {
    const message = { ...baseMsgContractDepositRent } as MsgContractDepositRent;
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    if (object.amount !== undefined && object.amount !== null) {
      message.amount = object.amount;
    } else {
      message.amount = 0;
    }
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = object.sender;
    } else {
      message.sender = "";
    }
    return message;
  },
};

const baseMsgContractDepositRentResponse: object = {};

export const MsgContractDepositRentResponse = {
  encode(
    _: MsgContractDepositRentResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgContractDepositRentResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgContractDepositRentResponse,
    } as MsgContractDepositRentResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(_: any): MsgContractDepositRentResponse {
    const message = {
      ...baseMsgContractDepositRentResponse,
    } as MsgContractDepositRentResponse;
    return message;
  },

  toJSON(_: MsgContractDepositRentResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgContractDepositRentResponse>
  ): MsgContractDepositRentResponse {
    const message = {
      ...baseMsgContractDepositRentResponse,
    } as MsgContractDepositRentResponse;
    return message;
  },
};

const baseMsgUnregisterContract: object = { creator: "", contractAddr: "" };

export const MsgUnregisterContract = {
  encode(
    message: MsgUnregisterContract,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.creator !== "") {
      writer.uint32(10).string(message.creator);
    }
    if (message.contractAddr !== "") {
      writer.uint32(18).string(message.contractAddr);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgUnregisterContract {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgUnregisterContract } as MsgUnregisterContract;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.creator = reader.string();
          break;
        case 2:
          message.contractAddr = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgUnregisterContract {
    const message = { ...baseMsgUnregisterContract } as MsgUnregisterContract;
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
    return message;
  },

  toJSON(message: MsgUnregisterContract): unknown {
    const obj: any = {};
    message.creator !== undefined && (obj.creator = message.creator);
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgUnregisterContract>
  ): MsgUnregisterContract {
    const message = { ...baseMsgUnregisterContract } as MsgUnregisterContract;
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
    return message;
  },
};

const baseMsgUnregisterContractResponse: object = {};

export const MsgUnregisterContractResponse = {
  encode(
    _: MsgUnregisterContractResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgUnregisterContractResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgUnregisterContractResponse,
    } as MsgUnregisterContractResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(_: any): MsgUnregisterContractResponse {
    const message = {
      ...baseMsgUnregisterContractResponse,
    } as MsgUnregisterContractResponse;
    return message;
  },

  toJSON(_: MsgUnregisterContractResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgUnregisterContractResponse>
  ): MsgUnregisterContractResponse {
    const message = {
      ...baseMsgUnregisterContractResponse,
    } as MsgUnregisterContractResponse;
    return message;
  },
};

const baseMsgRegisterPairs: object = { creator: "" };

export const MsgRegisterPairs = {
  encode(message: MsgRegisterPairs, writer: Writer = Writer.create()): Writer {
    if (message.creator !== "") {
      writer.uint32(10).string(message.creator);
    }
    for (const v of message.batchcontractpair) {
      BatchContractPair.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgRegisterPairs {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgRegisterPairs } as MsgRegisterPairs;
    message.batchcontractpair = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.creator = reader.string();
          break;
        case 3:
          message.batchcontractpair.push(
            BatchContractPair.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgRegisterPairs {
    const message = { ...baseMsgRegisterPairs } as MsgRegisterPairs;
    message.batchcontractpair = [];
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = String(object.creator);
    } else {
      message.creator = "";
    }
    if (
      object.batchcontractpair !== undefined &&
      object.batchcontractpair !== null
    ) {
      for (const e of object.batchcontractpair) {
        message.batchcontractpair.push(BatchContractPair.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: MsgRegisterPairs): unknown {
    const obj: any = {};
    message.creator !== undefined && (obj.creator = message.creator);
    if (message.batchcontractpair) {
      obj.batchcontractpair = message.batchcontractpair.map((e) =>
        e ? BatchContractPair.toJSON(e) : undefined
      );
    } else {
      obj.batchcontractpair = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<MsgRegisterPairs>): MsgRegisterPairs {
    const message = { ...baseMsgRegisterPairs } as MsgRegisterPairs;
    message.batchcontractpair = [];
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = object.creator;
    } else {
      message.creator = "";
    }
    if (
      object.batchcontractpair !== undefined &&
      object.batchcontractpair !== null
    ) {
      for (const e of object.batchcontractpair) {
        message.batchcontractpair.push(BatchContractPair.fromPartial(e));
      }
    }
    return message;
  },
};

const baseMsgRegisterPairsResponse: object = {};

export const MsgRegisterPairsResponse = {
  encode(
    _: MsgRegisterPairsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgRegisterPairsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgRegisterPairsResponse,
    } as MsgRegisterPairsResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(_: any): MsgRegisterPairsResponse {
    const message = {
      ...baseMsgRegisterPairsResponse,
    } as MsgRegisterPairsResponse;
    return message;
  },

  toJSON(_: MsgRegisterPairsResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgRegisterPairsResponse>
  ): MsgRegisterPairsResponse {
    const message = {
      ...baseMsgRegisterPairsResponse,
    } as MsgRegisterPairsResponse;
    return message;
  },
};

const baseMsgUpdatePriceTickSize: object = { creator: "" };

export const MsgUpdatePriceTickSize = {
  encode(
    message: MsgUpdatePriceTickSize,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.creator !== "") {
      writer.uint32(10).string(message.creator);
    }
    for (const v of message.tickSizeList) {
      TickSize.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgUpdatePriceTickSize {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgUpdatePriceTickSize } as MsgUpdatePriceTickSize;
    message.tickSizeList = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.creator = reader.string();
          break;
        case 2:
          message.tickSizeList.push(TickSize.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgUpdatePriceTickSize {
    const message = { ...baseMsgUpdatePriceTickSize } as MsgUpdatePriceTickSize;
    message.tickSizeList = [];
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = String(object.creator);
    } else {
      message.creator = "";
    }
    if (object.tickSizeList !== undefined && object.tickSizeList !== null) {
      for (const e of object.tickSizeList) {
        message.tickSizeList.push(TickSize.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: MsgUpdatePriceTickSize): unknown {
    const obj: any = {};
    message.creator !== undefined && (obj.creator = message.creator);
    if (message.tickSizeList) {
      obj.tickSizeList = message.tickSizeList.map((e) =>
        e ? TickSize.toJSON(e) : undefined
      );
    } else {
      obj.tickSizeList = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgUpdatePriceTickSize>
  ): MsgUpdatePriceTickSize {
    const message = { ...baseMsgUpdatePriceTickSize } as MsgUpdatePriceTickSize;
    message.tickSizeList = [];
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = object.creator;
    } else {
      message.creator = "";
    }
    if (object.tickSizeList !== undefined && object.tickSizeList !== null) {
      for (const e of object.tickSizeList) {
        message.tickSizeList.push(TickSize.fromPartial(e));
      }
    }
    return message;
  },
};

const baseMsgUpdateQuantityTickSize: object = { creator: "" };

export const MsgUpdateQuantityTickSize = {
  encode(
    message: MsgUpdateQuantityTickSize,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.creator !== "") {
      writer.uint32(10).string(message.creator);
    }
    for (const v of message.tickSizeList) {
      TickSize.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgUpdateQuantityTickSize {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgUpdateQuantityTickSize,
    } as MsgUpdateQuantityTickSize;
    message.tickSizeList = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.creator = reader.string();
          break;
        case 2:
          message.tickSizeList.push(TickSize.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgUpdateQuantityTickSize {
    const message = {
      ...baseMsgUpdateQuantityTickSize,
    } as MsgUpdateQuantityTickSize;
    message.tickSizeList = [];
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = String(object.creator);
    } else {
      message.creator = "";
    }
    if (object.tickSizeList !== undefined && object.tickSizeList !== null) {
      for (const e of object.tickSizeList) {
        message.tickSizeList.push(TickSize.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: MsgUpdateQuantityTickSize): unknown {
    const obj: any = {};
    message.creator !== undefined && (obj.creator = message.creator);
    if (message.tickSizeList) {
      obj.tickSizeList = message.tickSizeList.map((e) =>
        e ? TickSize.toJSON(e) : undefined
      );
    } else {
      obj.tickSizeList = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgUpdateQuantityTickSize>
  ): MsgUpdateQuantityTickSize {
    const message = {
      ...baseMsgUpdateQuantityTickSize,
    } as MsgUpdateQuantityTickSize;
    message.tickSizeList = [];
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = object.creator;
    } else {
      message.creator = "";
    }
    if (object.tickSizeList !== undefined && object.tickSizeList !== null) {
      for (const e of object.tickSizeList) {
        message.tickSizeList.push(TickSize.fromPartial(e));
      }
    }
    return message;
  },
};

const baseMsgUpdateTickSizeResponse: object = {};

export const MsgUpdateTickSizeResponse = {
  encode(
    _: MsgUpdateTickSizeResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgUpdateTickSizeResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgUpdateTickSizeResponse,
    } as MsgUpdateTickSizeResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(_: any): MsgUpdateTickSizeResponse {
    const message = {
      ...baseMsgUpdateTickSizeResponse,
    } as MsgUpdateTickSizeResponse;
    return message;
  },

  toJSON(_: MsgUpdateTickSizeResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgUpdateTickSizeResponse>
  ): MsgUpdateTickSizeResponse {
    const message = {
      ...baseMsgUpdateTickSizeResponse,
    } as MsgUpdateTickSizeResponse;
    return message;
  },
};

const baseMsgUnsuspendContract: object = { creator: "", contractAddr: "" };

export const MsgUnsuspendContract = {
  encode(
    message: MsgUnsuspendContract,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.creator !== "") {
      writer.uint32(10).string(message.creator);
    }
    if (message.contractAddr !== "") {
      writer.uint32(18).string(message.contractAddr);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgUnsuspendContract {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgUnsuspendContract } as MsgUnsuspendContract;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.creator = reader.string();
          break;
        case 2:
          message.contractAddr = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgUnsuspendContract {
    const message = { ...baseMsgUnsuspendContract } as MsgUnsuspendContract;
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
    return message;
  },

  toJSON(message: MsgUnsuspendContract): unknown {
    const obj: any = {};
    message.creator !== undefined && (obj.creator = message.creator);
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgUnsuspendContract>): MsgUnsuspendContract {
    const message = { ...baseMsgUnsuspendContract } as MsgUnsuspendContract;
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
    return message;
  },
};

const baseMsgUnsuspendContractResponse: object = {};

export const MsgUnsuspendContractResponse = {
  encode(
    _: MsgUnsuspendContractResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgUnsuspendContractResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgUnsuspendContractResponse,
    } as MsgUnsuspendContractResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(_: any): MsgUnsuspendContractResponse {
    const message = {
      ...baseMsgUnsuspendContractResponse,
    } as MsgUnsuspendContractResponse;
    return message;
  },

  toJSON(_: MsgUnsuspendContractResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgUnsuspendContractResponse>
  ): MsgUnsuspendContractResponse {
    const message = {
      ...baseMsgUnsuspendContractResponse,
    } as MsgUnsuspendContractResponse;
    return message;
  },
};

/** Msg defines the Msg service. */
export interface Msg {
  PlaceOrders(request: MsgPlaceOrders): Promise<MsgPlaceOrdersResponse>;
  CancelOrders(request: MsgCancelOrders): Promise<MsgCancelOrdersResponse>;
  RegisterContract(
    request: MsgRegisterContract
  ): Promise<MsgRegisterContractResponse>;
  ContractDepositRent(
    request: MsgContractDepositRent
  ): Promise<MsgContractDepositRentResponse>;
  UnregisterContract(
    request: MsgUnregisterContract
  ): Promise<MsgUnregisterContractResponse>;
  RegisterPairs(request: MsgRegisterPairs): Promise<MsgRegisterPairsResponse>;
  UpdatePriceTickSize(
    request: MsgUpdatePriceTickSize
  ): Promise<MsgUpdateTickSizeResponse>;
  UpdateQuantityTickSize(
    request: MsgUpdateQuantityTickSize
  ): Promise<MsgUpdateTickSizeResponse>;
  /** privileged endpoints below */
  UnsuspendContract(
    request: MsgUnsuspendContract
  ): Promise<MsgUnsuspendContractResponse>;
}

export class MsgClientImpl implements Msg {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  PlaceOrders(request: MsgPlaceOrders): Promise<MsgPlaceOrdersResponse> {
    const data = MsgPlaceOrders.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Msg",
      "PlaceOrders",
      data
    );
    return promise.then((data) =>
      MsgPlaceOrdersResponse.decode(new Reader(data))
    );
  }

  CancelOrders(request: MsgCancelOrders): Promise<MsgCancelOrdersResponse> {
    const data = MsgCancelOrders.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Msg",
      "CancelOrders",
      data
    );
    return promise.then((data) =>
      MsgCancelOrdersResponse.decode(new Reader(data))
    );
  }

  RegisterContract(
    request: MsgRegisterContract
  ): Promise<MsgRegisterContractResponse> {
    const data = MsgRegisterContract.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Msg",
      "RegisterContract",
      data
    );
    return promise.then((data) =>
      MsgRegisterContractResponse.decode(new Reader(data))
    );
  }

  ContractDepositRent(
    request: MsgContractDepositRent
  ): Promise<MsgContractDepositRentResponse> {
    const data = MsgContractDepositRent.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Msg",
      "ContractDepositRent",
      data
    );
    return promise.then((data) =>
      MsgContractDepositRentResponse.decode(new Reader(data))
    );
  }

  UnregisterContract(
    request: MsgUnregisterContract
  ): Promise<MsgUnregisterContractResponse> {
    const data = MsgUnregisterContract.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Msg",
      "UnregisterContract",
      data
    );
    return promise.then((data) =>
      MsgUnregisterContractResponse.decode(new Reader(data))
    );
  }

  RegisterPairs(request: MsgRegisterPairs): Promise<MsgRegisterPairsResponse> {
    const data = MsgRegisterPairs.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Msg",
      "RegisterPairs",
      data
    );
    return promise.then((data) =>
      MsgRegisterPairsResponse.decode(new Reader(data))
    );
  }

  UpdatePriceTickSize(
    request: MsgUpdatePriceTickSize
  ): Promise<MsgUpdateTickSizeResponse> {
    const data = MsgUpdatePriceTickSize.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Msg",
      "UpdatePriceTickSize",
      data
    );
    return promise.then((data) =>
      MsgUpdateTickSizeResponse.decode(new Reader(data))
    );
  }

  UpdateQuantityTickSize(
    request: MsgUpdateQuantityTickSize
  ): Promise<MsgUpdateTickSizeResponse> {
    const data = MsgUpdateQuantityTickSize.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Msg",
      "UpdateQuantityTickSize",
      data
    );
    return promise.then((data) =>
      MsgUpdateTickSizeResponse.decode(new Reader(data))
    );
  }

  UnsuspendContract(
    request: MsgUnsuspendContract
  ): Promise<MsgUnsuspendContractResponse> {
    const data = MsgUnsuspendContract.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Msg",
      "UnsuspendContract",
      data
    );
    return promise.then((data) =>
      MsgUnsuspendContractResponse.decode(new Reader(data))
    );
  }
}

interface Rpc {
  request(
    service: string,
    method: string,
    data: Uint8Array
  ): Promise<Uint8Array>;
}

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
