/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "sei.evm.v1";

export interface Log {
  address: string;
  topics: string[];
  data: Uint8Array;
  index: number;
}

export interface Receipt {
  txType: number;
  cumulativeGasUsed: number;
  contractAddress: string;
  txHashHex: string;
  gasUsed: number;
  effectiveGasPrice: number;
  blockNumber: number;
  transactionIndex: number;
  status: number;
  from: string;
  to: string;
  vmError: string;
  logs: Log[];
  logsBloom: Uint8Array;
}

const baseLog: object = { address: "", topics: "", index: 0 };

export const Log = {
  encode(message: Log, writer: Writer = Writer.create()): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    for (const v of message.topics) {
      writer.uint32(18).string(v!);
    }
    if (message.data.length !== 0) {
      writer.uint32(26).bytes(message.data);
    }
    if (message.index !== 0) {
      writer.uint32(32).uint32(message.index);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Log {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseLog } as Log;
    message.topics = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
          break;
        case 2:
          message.topics.push(reader.string());
          break;
        case 3:
          message.data = reader.bytes();
          break;
        case 4:
          message.index = reader.uint32();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Log {
    const message = { ...baseLog } as Log;
    message.topics = [];
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (object.topics !== undefined && object.topics !== null) {
      for (const e of object.topics) {
        message.topics.push(String(e));
      }
    }
    if (object.data !== undefined && object.data !== null) {
      message.data = bytesFromBase64(object.data);
    }
    if (object.index !== undefined && object.index !== null) {
      message.index = Number(object.index);
    } else {
      message.index = 0;
    }
    return message;
  },

  toJSON(message: Log): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    if (message.topics) {
      obj.topics = message.topics.map((e) => e);
    } else {
      obj.topics = [];
    }
    message.data !== undefined &&
      (obj.data = base64FromBytes(
        message.data !== undefined ? message.data : new Uint8Array()
      ));
    message.index !== undefined && (obj.index = message.index);
    return obj;
  },

  fromPartial(object: DeepPartial<Log>): Log {
    const message = { ...baseLog } as Log;
    message.topics = [];
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.topics !== undefined && object.topics !== null) {
      for (const e of object.topics) {
        message.topics.push(e);
      }
    }
    if (object.data !== undefined && object.data !== null) {
      message.data = object.data;
    } else {
      message.data = new Uint8Array();
    }
    if (object.index !== undefined && object.index !== null) {
      message.index = object.index;
    } else {
      message.index = 0;
    }
    return message;
  },
};

const baseReceipt: object = {
  txType: 0,
  cumulativeGasUsed: 0,
  contractAddress: "",
  txHashHex: "",
  gasUsed: 0,
  effectiveGasPrice: 0,
  blockNumber: 0,
  transactionIndex: 0,
  status: 0,
  from: "",
  to: "",
  vmError: "",
};

export const Receipt = {
  encode(message: Receipt, writer: Writer = Writer.create()): Writer {
    if (message.txType !== 0) {
      writer.uint32(8).uint32(message.txType);
    }
    if (message.cumulativeGasUsed !== 0) {
      writer.uint32(16).uint64(message.cumulativeGasUsed);
    }
    if (message.contractAddress !== "") {
      writer.uint32(26).string(message.contractAddress);
    }
    if (message.txHashHex !== "") {
      writer.uint32(34).string(message.txHashHex);
    }
    if (message.gasUsed !== 0) {
      writer.uint32(40).uint64(message.gasUsed);
    }
    if (message.effectiveGasPrice !== 0) {
      writer.uint32(48).uint64(message.effectiveGasPrice);
    }
    if (message.blockNumber !== 0) {
      writer.uint32(56).uint64(message.blockNumber);
    }
    if (message.transactionIndex !== 0) {
      writer.uint32(64).uint32(message.transactionIndex);
    }
    if (message.status !== 0) {
      writer.uint32(72).uint32(message.status);
    }
    if (message.from !== "") {
      writer.uint32(82).string(message.from);
    }
    if (message.to !== "") {
      writer.uint32(90).string(message.to);
    }
    if (message.vmError !== "") {
      writer.uint32(98).string(message.vmError);
    }
    for (const v of message.logs) {
      Log.encode(v!, writer.uint32(106).fork()).ldelim();
    }
    if (message.logsBloom.length !== 0) {
      writer.uint32(114).bytes(message.logsBloom);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Receipt {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseReceipt } as Receipt;
    message.logs = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.txType = reader.uint32();
          break;
        case 2:
          message.cumulativeGasUsed = longToNumber(reader.uint64() as Long);
          break;
        case 3:
          message.contractAddress = reader.string();
          break;
        case 4:
          message.txHashHex = reader.string();
          break;
        case 5:
          message.gasUsed = longToNumber(reader.uint64() as Long);
          break;
        case 6:
          message.effectiveGasPrice = longToNumber(reader.uint64() as Long);
          break;
        case 7:
          message.blockNumber = longToNumber(reader.uint64() as Long);
          break;
        case 8:
          message.transactionIndex = reader.uint32();
          break;
        case 9:
          message.status = reader.uint32();
          break;
        case 10:
          message.from = reader.string();
          break;
        case 11:
          message.to = reader.string();
          break;
        case 12:
          message.vmError = reader.string();
          break;
        case 13:
          message.logs.push(Log.decode(reader, reader.uint32()));
          break;
        case 14:
          message.logsBloom = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Receipt {
    const message = { ...baseReceipt } as Receipt;
    message.logs = [];
    if (object.txType !== undefined && object.txType !== null) {
      message.txType = Number(object.txType);
    } else {
      message.txType = 0;
    }
    if (
      object.cumulativeGasUsed !== undefined &&
      object.cumulativeGasUsed !== null
    ) {
      message.cumulativeGasUsed = Number(object.cumulativeGasUsed);
    } else {
      message.cumulativeGasUsed = 0;
    }
    if (
      object.contractAddress !== undefined &&
      object.contractAddress !== null
    ) {
      message.contractAddress = String(object.contractAddress);
    } else {
      message.contractAddress = "";
    }
    if (object.txHashHex !== undefined && object.txHashHex !== null) {
      message.txHashHex = String(object.txHashHex);
    } else {
      message.txHashHex = "";
    }
    if (object.gasUsed !== undefined && object.gasUsed !== null) {
      message.gasUsed = Number(object.gasUsed);
    } else {
      message.gasUsed = 0;
    }
    if (
      object.effectiveGasPrice !== undefined &&
      object.effectiveGasPrice !== null
    ) {
      message.effectiveGasPrice = Number(object.effectiveGasPrice);
    } else {
      message.effectiveGasPrice = 0;
    }
    if (object.blockNumber !== undefined && object.blockNumber !== null) {
      message.blockNumber = Number(object.blockNumber);
    } else {
      message.blockNumber = 0;
    }
    if (
      object.transactionIndex !== undefined &&
      object.transactionIndex !== null
    ) {
      message.transactionIndex = Number(object.transactionIndex);
    } else {
      message.transactionIndex = 0;
    }
    if (object.status !== undefined && object.status !== null) {
      message.status = Number(object.status);
    } else {
      message.status = 0;
    }
    if (object.from !== undefined && object.from !== null) {
      message.from = String(object.from);
    } else {
      message.from = "";
    }
    if (object.to !== undefined && object.to !== null) {
      message.to = String(object.to);
    } else {
      message.to = "";
    }
    if (object.vmError !== undefined && object.vmError !== null) {
      message.vmError = String(object.vmError);
    } else {
      message.vmError = "";
    }
    if (object.logs !== undefined && object.logs !== null) {
      for (const e of object.logs) {
        message.logs.push(Log.fromJSON(e));
      }
    }
    if (object.logsBloom !== undefined && object.logsBloom !== null) {
      message.logsBloom = bytesFromBase64(object.logsBloom);
    }
    return message;
  },

  toJSON(message: Receipt): unknown {
    const obj: any = {};
    message.txType !== undefined && (obj.txType = message.txType);
    message.cumulativeGasUsed !== undefined &&
      (obj.cumulativeGasUsed = message.cumulativeGasUsed);
    message.contractAddress !== undefined &&
      (obj.contractAddress = message.contractAddress);
    message.txHashHex !== undefined && (obj.txHashHex = message.txHashHex);
    message.gasUsed !== undefined && (obj.gasUsed = message.gasUsed);
    message.effectiveGasPrice !== undefined &&
      (obj.effectiveGasPrice = message.effectiveGasPrice);
    message.blockNumber !== undefined &&
      (obj.blockNumber = message.blockNumber);
    message.transactionIndex !== undefined &&
      (obj.transactionIndex = message.transactionIndex);
    message.status !== undefined && (obj.status = message.status);
    message.from !== undefined && (obj.from = message.from);
    message.to !== undefined && (obj.to = message.to);
    message.vmError !== undefined && (obj.vmError = message.vmError);
    if (message.logs) {
      obj.logs = message.logs.map((e) => (e ? Log.toJSON(e) : undefined));
    } else {
      obj.logs = [];
    }
    message.logsBloom !== undefined &&
      (obj.logsBloom = base64FromBytes(
        message.logsBloom !== undefined ? message.logsBloom : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<Receipt>): Receipt {
    const message = { ...baseReceipt } as Receipt;
    message.logs = [];
    if (object.txType !== undefined && object.txType !== null) {
      message.txType = object.txType;
    } else {
      message.txType = 0;
    }
    if (
      object.cumulativeGasUsed !== undefined &&
      object.cumulativeGasUsed !== null
    ) {
      message.cumulativeGasUsed = object.cumulativeGasUsed;
    } else {
      message.cumulativeGasUsed = 0;
    }
    if (
      object.contractAddress !== undefined &&
      object.contractAddress !== null
    ) {
      message.contractAddress = object.contractAddress;
    } else {
      message.contractAddress = "";
    }
    if (object.txHashHex !== undefined && object.txHashHex !== null) {
      message.txHashHex = object.txHashHex;
    } else {
      message.txHashHex = "";
    }
    if (object.gasUsed !== undefined && object.gasUsed !== null) {
      message.gasUsed = object.gasUsed;
    } else {
      message.gasUsed = 0;
    }
    if (
      object.effectiveGasPrice !== undefined &&
      object.effectiveGasPrice !== null
    ) {
      message.effectiveGasPrice = object.effectiveGasPrice;
    } else {
      message.effectiveGasPrice = 0;
    }
    if (object.blockNumber !== undefined && object.blockNumber !== null) {
      message.blockNumber = object.blockNumber;
    } else {
      message.blockNumber = 0;
    }
    if (
      object.transactionIndex !== undefined &&
      object.transactionIndex !== null
    ) {
      message.transactionIndex = object.transactionIndex;
    } else {
      message.transactionIndex = 0;
    }
    if (object.status !== undefined && object.status !== null) {
      message.status = object.status;
    } else {
      message.status = 0;
    }
    if (object.from !== undefined && object.from !== null) {
      message.from = object.from;
    } else {
      message.from = "";
    }
    if (object.to !== undefined && object.to !== null) {
      message.to = object.to;
    } else {
      message.to = "";
    }
    if (object.vmError !== undefined && object.vmError !== null) {
      message.vmError = object.vmError;
    } else {
      message.vmError = "";
    }
    if (object.logs !== undefined && object.logs !== null) {
      for (const e of object.logs) {
        message.logs.push(Log.fromPartial(e));
      }
    }
    if (object.logsBloom !== undefined && object.logsBloom !== null) {
      message.logsBloom = object.logsBloom;
    } else {
      message.logsBloom = new Uint8Array();
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

const atob: (b64: string) => string =
  globalThis.atob ||
  ((b64) => globalThis.Buffer.from(b64, "base64").toString("binary"));
function bytesFromBase64(b64: string): Uint8Array {
  const bin = atob(b64);
  const arr = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; ++i) {
    arr[i] = bin.charCodeAt(i);
  }
  return arr;
}

const btoa: (bin: string) => string =
  globalThis.btoa ||
  ((bin) => globalThis.Buffer.from(bin, "binary").toString("base64"));
function base64FromBytes(arr: Uint8Array): string {
  const bin: string[] = [];
  for (let i = 0; i < arr.byteLength; ++i) {
    bin.push(String.fromCharCode(arr[i]));
  }
  return btoa(bin.join(""));
}

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
