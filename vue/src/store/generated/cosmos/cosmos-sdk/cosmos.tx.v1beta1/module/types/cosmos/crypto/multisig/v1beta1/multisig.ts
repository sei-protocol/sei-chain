/* eslint-disable */
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "cosmos.crypto.multisig.v1beta1";

/**
 * MultiSignature wraps the signatures from a multisig.LegacyAminoPubKey.
 * See cosmos.tx.v1betata1.ModeInfo.Multi for how to specify which signers
 * signed and with which modes.
 */
export interface MultiSignature {
  signatures: Uint8Array[];
}

/**
 * CompactBitArray is an implementation of a space efficient bit array.
 * This is used to ensure that the encoded data takes up a minimal amount of
 * space after proto encoding.
 * This is not thread safe, and is not intended for concurrent usage.
 */
export interface CompactBitArray {
  extra_bits_stored: number;
  elems: Uint8Array;
}

const baseMultiSignature: object = {};

export const MultiSignature = {
  encode(message: MultiSignature, writer: Writer = Writer.create()): Writer {
    for (const v of message.signatures) {
      writer.uint32(10).bytes(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MultiSignature {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMultiSignature } as MultiSignature;
    message.signatures = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.signatures.push(reader.bytes());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MultiSignature {
    const message = { ...baseMultiSignature } as MultiSignature;
    message.signatures = [];
    if (object.signatures !== undefined && object.signatures !== null) {
      for (const e of object.signatures) {
        message.signatures.push(bytesFromBase64(e));
      }
    }
    return message;
  },

  toJSON(message: MultiSignature): unknown {
    const obj: any = {};
    if (message.signatures) {
      obj.signatures = message.signatures.map((e) =>
        base64FromBytes(e !== undefined ? e : new Uint8Array())
      );
    } else {
      obj.signatures = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<MultiSignature>): MultiSignature {
    const message = { ...baseMultiSignature } as MultiSignature;
    message.signatures = [];
    if (object.signatures !== undefined && object.signatures !== null) {
      for (const e of object.signatures) {
        message.signatures.push(e);
      }
    }
    return message;
  },
};

const baseCompactBitArray: object = { extra_bits_stored: 0 };

export const CompactBitArray = {
  encode(message: CompactBitArray, writer: Writer = Writer.create()): Writer {
    if (message.extra_bits_stored !== 0) {
      writer.uint32(8).uint32(message.extra_bits_stored);
    }
    if (message.elems.length !== 0) {
      writer.uint32(18).bytes(message.elems);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): CompactBitArray {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseCompactBitArray } as CompactBitArray;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.extra_bits_stored = reader.uint32();
          break;
        case 2:
          message.elems = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): CompactBitArray {
    const message = { ...baseCompactBitArray } as CompactBitArray;
    if (
      object.extra_bits_stored !== undefined &&
      object.extra_bits_stored !== null
    ) {
      message.extra_bits_stored = Number(object.extra_bits_stored);
    } else {
      message.extra_bits_stored = 0;
    }
    if (object.elems !== undefined && object.elems !== null) {
      message.elems = bytesFromBase64(object.elems);
    }
    return message;
  },

  toJSON(message: CompactBitArray): unknown {
    const obj: any = {};
    message.extra_bits_stored !== undefined &&
      (obj.extra_bits_stored = message.extra_bits_stored);
    message.elems !== undefined &&
      (obj.elems = base64FromBytes(
        message.elems !== undefined ? message.elems : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<CompactBitArray>): CompactBitArray {
    const message = { ...baseCompactBitArray } as CompactBitArray;
    if (
      object.extra_bits_stored !== undefined &&
      object.extra_bits_stored !== null
    ) {
      message.extra_bits_stored = object.extra_bits_stored;
    } else {
      message.extra_bits_stored = 0;
    }
    if (object.elems !== undefined && object.elems !== null) {
      message.elems = object.elems;
    } else {
      message.elems = new Uint8Array();
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
