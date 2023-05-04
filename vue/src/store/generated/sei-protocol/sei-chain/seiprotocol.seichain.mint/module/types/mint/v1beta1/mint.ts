/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.mint";

/** Minter represents the minting state. */
export interface Minter {
  /** current epoch provisions */
  epoch_provisions: string;
}

/** Params holds parameters for the mint module. */
export interface Params {
  /** type of coin to mint */
  mint_denom: string;
  /** epoch provisions from the first epoch */
  genesis_epoch_provisions: string;
  /** number of epochs to take to reduce rewards */
  reduction_period_in_epochs: number;
  /** reduction multiplier to execute on each period */
  reduction_factor: string;
}

const baseMinter: object = { epoch_provisions: "" };

export const Minter = {
  encode(message: Minter, writer: Writer = Writer.create()): Writer {
    if (message.epoch_provisions !== "") {
      writer.uint32(10).string(message.epoch_provisions);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Minter {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMinter } as Minter;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.epoch_provisions = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Minter {
    const message = { ...baseMinter } as Minter;
    if (
      object.epoch_provisions !== undefined &&
      object.epoch_provisions !== null
    ) {
      message.epoch_provisions = String(object.epoch_provisions);
    } else {
      message.epoch_provisions = "";
    }
    return message;
  },

  toJSON(message: Minter): unknown {
    const obj: any = {};
    message.epoch_provisions !== undefined &&
      (obj.epoch_provisions = message.epoch_provisions);
    return obj;
  },

  fromPartial(object: DeepPartial<Minter>): Minter {
    const message = { ...baseMinter } as Minter;
    if (
      object.epoch_provisions !== undefined &&
      object.epoch_provisions !== null
    ) {
      message.epoch_provisions = object.epoch_provisions;
    } else {
      message.epoch_provisions = "";
    }
    return message;
  },
};

const baseParams: object = {
  mint_denom: "",
  genesis_epoch_provisions: "",
  reduction_period_in_epochs: 0,
  reduction_factor: "",
};

export const Params = {
  encode(message: Params, writer: Writer = Writer.create()): Writer {
    if (message.mint_denom !== "") {
      writer.uint32(10).string(message.mint_denom);
    }
    if (message.genesis_epoch_provisions !== "") {
      writer.uint32(18).string(message.genesis_epoch_provisions);
    }
    if (message.reduction_period_in_epochs !== 0) {
      writer.uint32(24).int64(message.reduction_period_in_epochs);
    }
    if (message.reduction_factor !== "") {
      writer.uint32(34).string(message.reduction_factor);
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
          message.mint_denom = reader.string();
          break;
        case 2:
          message.genesis_epoch_provisions = reader.string();
          break;
        case 3:
          message.reduction_period_in_epochs = longToNumber(
            reader.int64() as Long
          );
          break;
        case 4:
          message.reduction_factor = reader.string();
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
    if (object.mint_denom !== undefined && object.mint_denom !== null) {
      message.mint_denom = String(object.mint_denom);
    } else {
      message.mint_denom = "";
    }
    if (
      object.genesis_epoch_provisions !== undefined &&
      object.genesis_epoch_provisions !== null
    ) {
      message.genesis_epoch_provisions = String(
        object.genesis_epoch_provisions
      );
    } else {
      message.genesis_epoch_provisions = "";
    }
    if (
      object.reduction_period_in_epochs !== undefined &&
      object.reduction_period_in_epochs !== null
    ) {
      message.reduction_period_in_epochs = Number(
        object.reduction_period_in_epochs
      );
    } else {
      message.reduction_period_in_epochs = 0;
    }
    if (
      object.reduction_factor !== undefined &&
      object.reduction_factor !== null
    ) {
      message.reduction_factor = String(object.reduction_factor);
    } else {
      message.reduction_factor = "";
    }
    return message;
  },

  toJSON(message: Params): unknown {
    const obj: any = {};
    message.mint_denom !== undefined && (obj.mint_denom = message.mint_denom);
    message.genesis_epoch_provisions !== undefined &&
      (obj.genesis_epoch_provisions = message.genesis_epoch_provisions);
    message.reduction_period_in_epochs !== undefined &&
      (obj.reduction_period_in_epochs = message.reduction_period_in_epochs);
    message.reduction_factor !== undefined &&
      (obj.reduction_factor = message.reduction_factor);
    return obj;
  },

  fromPartial(object: DeepPartial<Params>): Params {
    const message = { ...baseParams } as Params;
    if (object.mint_denom !== undefined && object.mint_denom !== null) {
      message.mint_denom = object.mint_denom;
    } else {
      message.mint_denom = "";
    }
    if (
      object.genesis_epoch_provisions !== undefined &&
      object.genesis_epoch_provisions !== null
    ) {
      message.genesis_epoch_provisions = object.genesis_epoch_provisions;
    } else {
      message.genesis_epoch_provisions = "";
    }
    if (
      object.reduction_period_in_epochs !== undefined &&
      object.reduction_period_in_epochs !== null
    ) {
      message.reduction_period_in_epochs = object.reduction_period_in_epochs;
    } else {
      message.reduction_period_in_epochs = 0;
    }
    if (
      object.reduction_factor !== undefined &&
      object.reduction_factor !== null
    ) {
      message.reduction_factor = object.reduction_factor;
    } else {
      message.reduction_factor = "";
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
