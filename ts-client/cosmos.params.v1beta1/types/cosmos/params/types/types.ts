/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { DecCoin } from "../../../cosmos/base/v1beta1/coin";

export const protobufPackage = "cosmos.params.v1beta1";

/** Defines fee params that are controlled through governance */
export interface FeesParams {
  globalMinimumGasPrices: DecCoin[];
}

export interface CosmosGasParams {
  cosmosGasMultiplierNumerator: number;
  cosmosGasMultiplierDenominator: number;
}

export interface GenesisState {
  feesParams: FeesParams | undefined;
  cosmosGasParams: CosmosGasParams | undefined;
}

const baseFeesParams: object = {};

export const FeesParams = {
  encode(message: FeesParams, writer: Writer = Writer.create()): Writer {
    for (const v of message.globalMinimumGasPrices) {
      DecCoin.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): FeesParams {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseFeesParams } as FeesParams;
    message.globalMinimumGasPrices = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.globalMinimumGasPrices.push(
            DecCoin.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): FeesParams {
    const message = { ...baseFeesParams } as FeesParams;
    message.globalMinimumGasPrices = [];
    if (
      object.globalMinimumGasPrices !== undefined &&
      object.globalMinimumGasPrices !== null
    ) {
      for (const e of object.globalMinimumGasPrices) {
        message.globalMinimumGasPrices.push(DecCoin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: FeesParams): unknown {
    const obj: any = {};
    if (message.globalMinimumGasPrices) {
      obj.globalMinimumGasPrices = message.globalMinimumGasPrices.map((e) =>
        e ? DecCoin.toJSON(e) : undefined
      );
    } else {
      obj.globalMinimumGasPrices = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<FeesParams>): FeesParams {
    const message = { ...baseFeesParams } as FeesParams;
    message.globalMinimumGasPrices = [];
    if (
      object.globalMinimumGasPrices !== undefined &&
      object.globalMinimumGasPrices !== null
    ) {
      for (const e of object.globalMinimumGasPrices) {
        message.globalMinimumGasPrices.push(DecCoin.fromPartial(e));
      }
    }
    return message;
  },
};

const baseCosmosGasParams: object = {
  cosmosGasMultiplierNumerator: 0,
  cosmosGasMultiplierDenominator: 0,
};

export const CosmosGasParams = {
  encode(message: CosmosGasParams, writer: Writer = Writer.create()): Writer {
    if (message.cosmosGasMultiplierNumerator !== 0) {
      writer.uint32(8).uint64(message.cosmosGasMultiplierNumerator);
    }
    if (message.cosmosGasMultiplierDenominator !== 0) {
      writer.uint32(16).uint64(message.cosmosGasMultiplierDenominator);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): CosmosGasParams {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseCosmosGasParams } as CosmosGasParams;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.cosmosGasMultiplierNumerator = longToNumber(
            reader.uint64() as Long
          );
          break;
        case 2:
          message.cosmosGasMultiplierDenominator = longToNumber(
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

  fromJSON(object: any): CosmosGasParams {
    const message = { ...baseCosmosGasParams } as CosmosGasParams;
    if (
      object.cosmosGasMultiplierNumerator !== undefined &&
      object.cosmosGasMultiplierNumerator !== null
    ) {
      message.cosmosGasMultiplierNumerator = Number(
        object.cosmosGasMultiplierNumerator
      );
    } else {
      message.cosmosGasMultiplierNumerator = 0;
    }
    if (
      object.cosmosGasMultiplierDenominator !== undefined &&
      object.cosmosGasMultiplierDenominator !== null
    ) {
      message.cosmosGasMultiplierDenominator = Number(
        object.cosmosGasMultiplierDenominator
      );
    } else {
      message.cosmosGasMultiplierDenominator = 0;
    }
    return message;
  },

  toJSON(message: CosmosGasParams): unknown {
    const obj: any = {};
    message.cosmosGasMultiplierNumerator !== undefined &&
      (obj.cosmosGasMultiplierNumerator = message.cosmosGasMultiplierNumerator);
    message.cosmosGasMultiplierDenominator !== undefined &&
      (obj.cosmosGasMultiplierDenominator =
        message.cosmosGasMultiplierDenominator);
    return obj;
  },

  fromPartial(object: DeepPartial<CosmosGasParams>): CosmosGasParams {
    const message = { ...baseCosmosGasParams } as CosmosGasParams;
    if (
      object.cosmosGasMultiplierNumerator !== undefined &&
      object.cosmosGasMultiplierNumerator !== null
    ) {
      message.cosmosGasMultiplierNumerator =
        object.cosmosGasMultiplierNumerator;
    } else {
      message.cosmosGasMultiplierNumerator = 0;
    }
    if (
      object.cosmosGasMultiplierDenominator !== undefined &&
      object.cosmosGasMultiplierDenominator !== null
    ) {
      message.cosmosGasMultiplierDenominator =
        object.cosmosGasMultiplierDenominator;
    } else {
      message.cosmosGasMultiplierDenominator = 0;
    }
    return message;
  },
};

const baseGenesisState: object = {};

export const GenesisState = {
  encode(message: GenesisState, writer: Writer = Writer.create()): Writer {
    if (message.feesParams !== undefined) {
      FeesParams.encode(message.feesParams, writer.uint32(10).fork()).ldelim();
    }
    if (message.cosmosGasParams !== undefined) {
      CosmosGasParams.encode(
        message.cosmosGasParams,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisState } as GenesisState;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.feesParams = FeesParams.decode(reader, reader.uint32());
          break;
        case 2:
          message.cosmosGasParams = CosmosGasParams.decode(
            reader,
            reader.uint32()
          );
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
    if (object.feesParams !== undefined && object.feesParams !== null) {
      message.feesParams = FeesParams.fromJSON(object.feesParams);
    } else {
      message.feesParams = undefined;
    }
    if (
      object.cosmosGasParams !== undefined &&
      object.cosmosGasParams !== null
    ) {
      message.cosmosGasParams = CosmosGasParams.fromJSON(
        object.cosmosGasParams
      );
    } else {
      message.cosmosGasParams = undefined;
    }
    return message;
  },

  toJSON(message: GenesisState): unknown {
    const obj: any = {};
    message.feesParams !== undefined &&
      (obj.feesParams = message.feesParams
        ? FeesParams.toJSON(message.feesParams)
        : undefined);
    message.cosmosGasParams !== undefined &&
      (obj.cosmosGasParams = message.cosmosGasParams
        ? CosmosGasParams.toJSON(message.cosmosGasParams)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisState>): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    if (object.feesParams !== undefined && object.feesParams !== null) {
      message.feesParams = FeesParams.fromPartial(object.feesParams);
    } else {
      message.feesParams = undefined;
    }
    if (
      object.cosmosGasParams !== undefined &&
      object.cosmosGasParams !== null
    ) {
      message.cosmosGasParams = CosmosGasParams.fromPartial(
        object.cosmosGasParams
      );
    } else {
      message.cosmosGasParams = undefined;
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
