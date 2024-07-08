/* eslint-disable */
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "sei.evm.v1";

/** Params defines the parameters for the module. */
export interface Params {
  /**
   * string base_denom = 1 [
   *   (gogoproto.moretags)   = "yaml:\"base_denom\"",
   *   (gogoproto.jsontag) = "base_denom"
   * ];
   */
  priorityNormalizer: string;
  baseFeePerGas: string;
  minimumFeePerGas: string;
  /**
   * ChainConfig chain_config = 5 [(gogoproto.moretags) = "yaml:\"chain_config\"", (gogoproto.nullable) = false];
   *   string chain_id = 6 [
   *   (gogoproto.moretags)   = "yaml:\"chain_id\"",
   *   (gogoproto.customtype) = "github.com/cosmos/cosmos-sdk/types.Int",
   *   (gogoproto.nullable)   = false,
   *   (gogoproto.jsontag) = "chain_id"
   * ];
   * repeated string whitelisted_codehashes_bank_send = 7 [
   *   (gogoproto.moretags)   = "yaml:\"whitelisted_codehashes_bank_send\"",
   *   (gogoproto.jsontag) = "whitelisted_codehashes_bank_send"
   * ];
   */
  whitelistedCwCodeHashesForDelegateCall: Uint8Array[];
}

const baseParams: object = {
  priorityNormalizer: "",
  baseFeePerGas: "",
  minimumFeePerGas: "",
};

export const Params = {
  encode(message: Params, writer: Writer = Writer.create()): Writer {
    if (message.priorityNormalizer !== "") {
      writer.uint32(18).string(message.priorityNormalizer);
    }
    if (message.baseFeePerGas !== "") {
      writer.uint32(26).string(message.baseFeePerGas);
    }
    if (message.minimumFeePerGas !== "") {
      writer.uint32(34).string(message.minimumFeePerGas);
    }
    for (const v of message.whitelistedCwCodeHashesForDelegateCall) {
      writer.uint32(66).bytes(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Params {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseParams } as Params;
    message.whitelistedCwCodeHashesForDelegateCall = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 2:
          message.priorityNormalizer = reader.string();
          break;
        case 3:
          message.baseFeePerGas = reader.string();
          break;
        case 4:
          message.minimumFeePerGas = reader.string();
          break;
        case 8:
          message.whitelistedCwCodeHashesForDelegateCall.push(reader.bytes());
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
    message.whitelistedCwCodeHashesForDelegateCall = [];
    if (
      object.priorityNormalizer !== undefined &&
      object.priorityNormalizer !== null
    ) {
      message.priorityNormalizer = String(object.priorityNormalizer);
    } else {
      message.priorityNormalizer = "";
    }
    if (object.baseFeePerGas !== undefined && object.baseFeePerGas !== null) {
      message.baseFeePerGas = String(object.baseFeePerGas);
    } else {
      message.baseFeePerGas = "";
    }
    if (
      object.minimumFeePerGas !== undefined &&
      object.minimumFeePerGas !== null
    ) {
      message.minimumFeePerGas = String(object.minimumFeePerGas);
    } else {
      message.minimumFeePerGas = "";
    }
    if (
      object.whitelistedCwCodeHashesForDelegateCall !== undefined &&
      object.whitelistedCwCodeHashesForDelegateCall !== null
    ) {
      for (const e of object.whitelistedCwCodeHashesForDelegateCall) {
        message.whitelistedCwCodeHashesForDelegateCall.push(bytesFromBase64(e));
      }
    }
    return message;
  },

  toJSON(message: Params): unknown {
    const obj: any = {};
    message.priorityNormalizer !== undefined &&
      (obj.priorityNormalizer = message.priorityNormalizer);
    message.baseFeePerGas !== undefined &&
      (obj.baseFeePerGas = message.baseFeePerGas);
    message.minimumFeePerGas !== undefined &&
      (obj.minimumFeePerGas = message.minimumFeePerGas);
    if (message.whitelistedCwCodeHashesForDelegateCall) {
      obj.whitelistedCwCodeHashesForDelegateCall = message.whitelistedCwCodeHashesForDelegateCall.map(
        (e) => base64FromBytes(e !== undefined ? e : new Uint8Array())
      );
    } else {
      obj.whitelistedCwCodeHashesForDelegateCall = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<Params>): Params {
    const message = { ...baseParams } as Params;
    message.whitelistedCwCodeHashesForDelegateCall = [];
    if (
      object.priorityNormalizer !== undefined &&
      object.priorityNormalizer !== null
    ) {
      message.priorityNormalizer = object.priorityNormalizer;
    } else {
      message.priorityNormalizer = "";
    }
    if (object.baseFeePerGas !== undefined && object.baseFeePerGas !== null) {
      message.baseFeePerGas = object.baseFeePerGas;
    } else {
      message.baseFeePerGas = "";
    }
    if (
      object.minimumFeePerGas !== undefined &&
      object.minimumFeePerGas !== null
    ) {
      message.minimumFeePerGas = object.minimumFeePerGas;
    } else {
      message.minimumFeePerGas = "";
    }
    if (
      object.whitelistedCwCodeHashesForDelegateCall !== undefined &&
      object.whitelistedCwCodeHashesForDelegateCall !== null
    ) {
      for (const e of object.whitelistedCwCodeHashesForDelegateCall) {
        message.whitelistedCwCodeHashesForDelegateCall.push(e);
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
