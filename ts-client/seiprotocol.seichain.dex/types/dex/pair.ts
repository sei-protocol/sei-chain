/* eslint-disable */
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.dex";

export interface Pair {
  priceDenom: string;
  assetDenom: string;
  priceTicksize: string;
  quantityTicksize: string;
}

export interface BatchContractPair {
  contractAddr: string;
  pairs: Pair[];
}

const basePair: object = {
  priceDenom: "",
  assetDenom: "",
  priceTicksize: "",
  quantityTicksize: "",
};

export const Pair = {
  encode(message: Pair, writer: Writer = Writer.create()): Writer {
    if (message.priceDenom !== "") {
      writer.uint32(10).string(message.priceDenom);
    }
    if (message.assetDenom !== "") {
      writer.uint32(18).string(message.assetDenom);
    }
    if (message.priceTicksize !== "") {
      writer.uint32(26).string(message.priceTicksize);
    }
    if (message.quantityTicksize !== "") {
      writer.uint32(34).string(message.quantityTicksize);
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
          message.priceDenom = reader.string();
          break;
        case 2:
          message.assetDenom = reader.string();
          break;
        case 3:
          message.priceTicksize = reader.string();
          break;
        case 4:
          message.quantityTicksize = reader.string();
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
      message.priceDenom = String(object.priceDenom);
    } else {
      message.priceDenom = "";
    }
    if (object.assetDenom !== undefined && object.assetDenom !== null) {
      message.assetDenom = String(object.assetDenom);
    } else {
      message.assetDenom = "";
    }
    if (object.priceTicksize !== undefined && object.priceTicksize !== null) {
      message.priceTicksize = String(object.priceTicksize);
    } else {
      message.priceTicksize = "";
    }
    if (
      object.quantityTicksize !== undefined &&
      object.quantityTicksize !== null
    ) {
      message.quantityTicksize = String(object.quantityTicksize);
    } else {
      message.quantityTicksize = "";
    }
    return message;
  },

  toJSON(message: Pair): unknown {
    const obj: any = {};
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    message.priceTicksize !== undefined &&
      (obj.priceTicksize = message.priceTicksize);
    message.quantityTicksize !== undefined &&
      (obj.quantityTicksize = message.quantityTicksize);
    return obj;
  },

  fromPartial(object: DeepPartial<Pair>): Pair {
    const message = { ...basePair } as Pair;
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
    if (object.priceTicksize !== undefined && object.priceTicksize !== null) {
      message.priceTicksize = object.priceTicksize;
    } else {
      message.priceTicksize = "";
    }
    if (
      object.quantityTicksize !== undefined &&
      object.quantityTicksize !== null
    ) {
      message.quantityTicksize = object.quantityTicksize;
    } else {
      message.quantityTicksize = "";
    }
    return message;
  },
};

const baseBatchContractPair: object = { contractAddr: "" };

export const BatchContractPair = {
  encode(message: BatchContractPair, writer: Writer = Writer.create()): Writer {
    if (message.contractAddr !== "") {
      writer.uint32(10).string(message.contractAddr);
    }
    for (const v of message.pairs) {
      Pair.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): BatchContractPair {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseBatchContractPair } as BatchContractPair;
    message.pairs = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.contractAddr = reader.string();
          break;
        case 2:
          message.pairs.push(Pair.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): BatchContractPair {
    const message = { ...baseBatchContractPair } as BatchContractPair;
    message.pairs = [];
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    if (object.pairs !== undefined && object.pairs !== null) {
      for (const e of object.pairs) {
        message.pairs.push(Pair.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: BatchContractPair): unknown {
    const obj: any = {};
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    if (message.pairs) {
      obj.pairs = message.pairs.map((e) => (e ? Pair.toJSON(e) : undefined));
    } else {
      obj.pairs = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<BatchContractPair>): BatchContractPair {
    const message = { ...baseBatchContractPair } as BatchContractPair;
    message.pairs = [];
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    if (object.pairs !== undefined && object.pairs !== null) {
      for (const e of object.pairs) {
        message.pairs.push(Pair.fromPartial(e));
      }
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
