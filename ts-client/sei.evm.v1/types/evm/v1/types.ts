/* eslint-disable */
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "sei.evm.v1";

export interface Whitelist {
  hashes: string[];
}

export interface DeferredInfo {
  txIndex: number;
  txHash: Uint8Array;
  txBloom: Uint8Array;
  surplus: string;
  error: string;
}

const baseWhitelist: object = { hashes: "" };

export const Whitelist = {
  encode(message: Whitelist, writer: Writer = Writer.create()): Writer {
    for (const v of message.hashes) {
      writer.uint32(10).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Whitelist {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseWhitelist } as Whitelist;
    message.hashes = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.hashes.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Whitelist {
    const message = { ...baseWhitelist } as Whitelist;
    message.hashes = [];
    if (object.hashes !== undefined && object.hashes !== null) {
      for (const e of object.hashes) {
        message.hashes.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: Whitelist): unknown {
    const obj: any = {};
    if (message.hashes) {
      obj.hashes = message.hashes.map((e) => e);
    } else {
      obj.hashes = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<Whitelist>): Whitelist {
    const message = { ...baseWhitelist } as Whitelist;
    message.hashes = [];
    if (object.hashes !== undefined && object.hashes !== null) {
      for (const e of object.hashes) {
        message.hashes.push(e);
      }
    }
    return message;
  },
};

const baseDeferredInfo: object = { txIndex: 0, surplus: "", error: "" };

export const DeferredInfo = {
  encode(message: DeferredInfo, writer: Writer = Writer.create()): Writer {
    if (message.txIndex !== 0) {
      writer.uint32(8).uint32(message.txIndex);
    }
    if (message.txHash.length !== 0) {
      writer.uint32(18).bytes(message.txHash);
    }
    if (message.txBloom.length !== 0) {
      writer.uint32(26).bytes(message.txBloom);
    }
    if (message.surplus !== "") {
      writer.uint32(34).string(message.surplus);
    }
    if (message.error !== "") {
      writer.uint32(42).string(message.error);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): DeferredInfo {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDeferredInfo } as DeferredInfo;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.txIndex = reader.uint32();
          break;
        case 2:
          message.txHash = reader.bytes();
          break;
        case 3:
          message.txBloom = reader.bytes();
          break;
        case 4:
          message.surplus = reader.string();
          break;
        case 5:
          message.error = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): DeferredInfo {
    const message = { ...baseDeferredInfo } as DeferredInfo;
    if (object.txIndex !== undefined && object.txIndex !== null) {
      message.txIndex = Number(object.txIndex);
    } else {
      message.txIndex = 0;
    }
    if (object.txHash !== undefined && object.txHash !== null) {
      message.txHash = bytesFromBase64(object.txHash);
    }
    if (object.txBloom !== undefined && object.txBloom !== null) {
      message.txBloom = bytesFromBase64(object.txBloom);
    }
    if (object.surplus !== undefined && object.surplus !== null) {
      message.surplus = String(object.surplus);
    } else {
      message.surplus = "";
    }
    if (object.error !== undefined && object.error !== null) {
      message.error = String(object.error);
    } else {
      message.error = "";
    }
    return message;
  },

  toJSON(message: DeferredInfo): unknown {
    const obj: any = {};
    message.txIndex !== undefined && (obj.txIndex = message.txIndex);
    message.txHash !== undefined &&
      (obj.txHash = base64FromBytes(
        message.txHash !== undefined ? message.txHash : new Uint8Array()
      ));
    message.txBloom !== undefined &&
      (obj.txBloom = base64FromBytes(
        message.txBloom !== undefined ? message.txBloom : new Uint8Array()
      ));
    message.surplus !== undefined && (obj.surplus = message.surplus);
    message.error !== undefined && (obj.error = message.error);
    return obj;
  },

  fromPartial(object: DeepPartial<DeferredInfo>): DeferredInfo {
    const message = { ...baseDeferredInfo } as DeferredInfo;
    if (object.txIndex !== undefined && object.txIndex !== null) {
      message.txIndex = object.txIndex;
    } else {
      message.txIndex = 0;
    }
    if (object.txHash !== undefined && object.txHash !== null) {
      message.txHash = object.txHash;
    } else {
      message.txHash = new Uint8Array();
    }
    if (object.txBloom !== undefined && object.txBloom !== null) {
      message.txBloom = object.txBloom;
    } else {
      message.txBloom = new Uint8Array();
    }
    if (object.surplus !== undefined && object.surplus !== null) {
      message.surplus = object.surplus;
    } else {
      message.surplus = "";
    }
    if (object.error !== undefined && object.error !== null) {
      message.error = object.error;
    } else {
      message.error = "";
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
