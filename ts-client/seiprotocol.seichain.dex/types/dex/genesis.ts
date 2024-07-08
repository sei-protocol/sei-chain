/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Params } from "../dex/params";
import { ContractInfoV2 } from "../dex/contract";
import { LongBook } from "../dex/long_book";
import { ShortBook } from "../dex/short_book";
import { Order } from "../dex/order";
import { Pair } from "../dex/pair";
import { Price } from "../dex/price";

export const protobufPackage = "seiprotocol.seichain.dex";

/** GenesisState defines the dex module's genesis state. */
export interface GenesisState {
  params: Params | undefined;
  contractState: ContractState[];
  /** this line is used by starport scaffolding # genesis/proto/state */
  lastEpoch: number;
}

export interface ContractState {
  contractInfo: ContractInfoV2 | undefined;
  longBookList: LongBook[];
  shortBookList: ShortBook[];
  triggeredOrdersList: Order[];
  pairList: Pair[];
  priceList: ContractPairPrices[];
  nextOrderId: number;
}

export interface ContractPairPrices {
  pricePair: Pair | undefined;
  prices: Price[];
}

const baseGenesisState: object = { lastEpoch: 0 };

export const GenesisState = {
  encode(message: GenesisState, writer: Writer = Writer.create()): Writer {
    if (message.params !== undefined) {
      Params.encode(message.params, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.contractState) {
      ContractState.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    if (message.lastEpoch !== 0) {
      writer.uint32(24).uint64(message.lastEpoch);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisState } as GenesisState;
    message.contractState = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.params = Params.decode(reader, reader.uint32());
          break;
        case 2:
          message.contractState.push(
            ContractState.decode(reader, reader.uint32())
          );
          break;
        case 3:
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
    message.contractState = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromJSON(object.params);
    } else {
      message.params = undefined;
    }
    if (object.contractState !== undefined && object.contractState !== null) {
      for (const e of object.contractState) {
        message.contractState.push(ContractState.fromJSON(e));
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
    if (message.contractState) {
      obj.contractState = message.contractState.map((e) =>
        e ? ContractState.toJSON(e) : undefined
      );
    } else {
      obj.contractState = [];
    }
    message.lastEpoch !== undefined && (obj.lastEpoch = message.lastEpoch);
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisState>): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.contractState = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromPartial(object.params);
    } else {
      message.params = undefined;
    }
    if (object.contractState !== undefined && object.contractState !== null) {
      for (const e of object.contractState) {
        message.contractState.push(ContractState.fromPartial(e));
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

const baseContractState: object = { nextOrderId: 0 };

export const ContractState = {
  encode(message: ContractState, writer: Writer = Writer.create()): Writer {
    if (message.contractInfo !== undefined) {
      ContractInfoV2.encode(
        message.contractInfo,
        writer.uint32(10).fork()
      ).ldelim();
    }
    for (const v of message.longBookList) {
      LongBook.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.shortBookList) {
      ShortBook.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    for (const v of message.triggeredOrdersList) {
      Order.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    for (const v of message.pairList) {
      Pair.encode(v!, writer.uint32(42).fork()).ldelim();
    }
    for (const v of message.priceList) {
      ContractPairPrices.encode(v!, writer.uint32(50).fork()).ldelim();
    }
    if (message.nextOrderId !== 0) {
      writer.uint32(56).uint64(message.nextOrderId);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ContractState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseContractState } as ContractState;
    message.longBookList = [];
    message.shortBookList = [];
    message.triggeredOrdersList = [];
    message.pairList = [];
    message.priceList = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.contractInfo = ContractInfoV2.decode(reader, reader.uint32());
          break;
        case 2:
          message.longBookList.push(LongBook.decode(reader, reader.uint32()));
          break;
        case 3:
          message.shortBookList.push(ShortBook.decode(reader, reader.uint32()));
          break;
        case 4:
          message.triggeredOrdersList.push(
            Order.decode(reader, reader.uint32())
          );
          break;
        case 5:
          message.pairList.push(Pair.decode(reader, reader.uint32()));
          break;
        case 6:
          message.priceList.push(
            ContractPairPrices.decode(reader, reader.uint32())
          );
          break;
        case 7:
          message.nextOrderId = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ContractState {
    const message = { ...baseContractState } as ContractState;
    message.longBookList = [];
    message.shortBookList = [];
    message.triggeredOrdersList = [];
    message.pairList = [];
    message.priceList = [];
    if (object.contractInfo !== undefined && object.contractInfo !== null) {
      message.contractInfo = ContractInfoV2.fromJSON(object.contractInfo);
    } else {
      message.contractInfo = undefined;
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
    if (
      object.triggeredOrdersList !== undefined &&
      object.triggeredOrdersList !== null
    ) {
      for (const e of object.triggeredOrdersList) {
        message.triggeredOrdersList.push(Order.fromJSON(e));
      }
    }
    if (object.pairList !== undefined && object.pairList !== null) {
      for (const e of object.pairList) {
        message.pairList.push(Pair.fromJSON(e));
      }
    }
    if (object.priceList !== undefined && object.priceList !== null) {
      for (const e of object.priceList) {
        message.priceList.push(ContractPairPrices.fromJSON(e));
      }
    }
    if (object.nextOrderId !== undefined && object.nextOrderId !== null) {
      message.nextOrderId = Number(object.nextOrderId);
    } else {
      message.nextOrderId = 0;
    }
    return message;
  },

  toJSON(message: ContractState): unknown {
    const obj: any = {};
    message.contractInfo !== undefined &&
      (obj.contractInfo = message.contractInfo
        ? ContractInfoV2.toJSON(message.contractInfo)
        : undefined);
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
    if (message.triggeredOrdersList) {
      obj.triggeredOrdersList = message.triggeredOrdersList.map((e) =>
        e ? Order.toJSON(e) : undefined
      );
    } else {
      obj.triggeredOrdersList = [];
    }
    if (message.pairList) {
      obj.pairList = message.pairList.map((e) =>
        e ? Pair.toJSON(e) : undefined
      );
    } else {
      obj.pairList = [];
    }
    if (message.priceList) {
      obj.priceList = message.priceList.map((e) =>
        e ? ContractPairPrices.toJSON(e) : undefined
      );
    } else {
      obj.priceList = [];
    }
    message.nextOrderId !== undefined &&
      (obj.nextOrderId = message.nextOrderId);
    return obj;
  },

  fromPartial(object: DeepPartial<ContractState>): ContractState {
    const message = { ...baseContractState } as ContractState;
    message.longBookList = [];
    message.shortBookList = [];
    message.triggeredOrdersList = [];
    message.pairList = [];
    message.priceList = [];
    if (object.contractInfo !== undefined && object.contractInfo !== null) {
      message.contractInfo = ContractInfoV2.fromPartial(object.contractInfo);
    } else {
      message.contractInfo = undefined;
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
    if (
      object.triggeredOrdersList !== undefined &&
      object.triggeredOrdersList !== null
    ) {
      for (const e of object.triggeredOrdersList) {
        message.triggeredOrdersList.push(Order.fromPartial(e));
      }
    }
    if (object.pairList !== undefined && object.pairList !== null) {
      for (const e of object.pairList) {
        message.pairList.push(Pair.fromPartial(e));
      }
    }
    if (object.priceList !== undefined && object.priceList !== null) {
      for (const e of object.priceList) {
        message.priceList.push(ContractPairPrices.fromPartial(e));
      }
    }
    if (object.nextOrderId !== undefined && object.nextOrderId !== null) {
      message.nextOrderId = object.nextOrderId;
    } else {
      message.nextOrderId = 0;
    }
    return message;
  },
};

const baseContractPairPrices: object = {};

export const ContractPairPrices = {
  encode(
    message: ContractPairPrices,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.pricePair !== undefined) {
      Pair.encode(message.pricePair, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.prices) {
      Price.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ContractPairPrices {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseContractPairPrices } as ContractPairPrices;
    message.prices = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.pricePair = Pair.decode(reader, reader.uint32());
          break;
        case 2:
          message.prices.push(Price.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ContractPairPrices {
    const message = { ...baseContractPairPrices } as ContractPairPrices;
    message.prices = [];
    if (object.pricePair !== undefined && object.pricePair !== null) {
      message.pricePair = Pair.fromJSON(object.pricePair);
    } else {
      message.pricePair = undefined;
    }
    if (object.prices !== undefined && object.prices !== null) {
      for (const e of object.prices) {
        message.prices.push(Price.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: ContractPairPrices): unknown {
    const obj: any = {};
    message.pricePair !== undefined &&
      (obj.pricePair = message.pricePair
        ? Pair.toJSON(message.pricePair)
        : undefined);
    if (message.prices) {
      obj.prices = message.prices.map((e) => (e ? Price.toJSON(e) : undefined));
    } else {
      obj.prices = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<ContractPairPrices>): ContractPairPrices {
    const message = { ...baseContractPairPrices } as ContractPairPrices;
    message.prices = [];
    if (object.pricePair !== undefined && object.pricePair !== null) {
      message.pricePair = Pair.fromPartial(object.pricePair);
    } else {
      message.pricePair = undefined;
    }
    if (object.prices !== undefined && object.prices !== null) {
      for (const e of object.prices) {
        message.prices.push(Price.fromPartial(e));
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
