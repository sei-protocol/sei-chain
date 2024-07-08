/* eslint-disable */
import { Timestamp } from "../../../google/protobuf/timestamp";
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Duration } from "../../../google/protobuf/duration";

export const protobufPackage = "cosmos.slashing.v1beta1";

export interface ValidatorSigningInfoLegacyMissedHeights {
  address: string;
  /** Height at which validator was first a candidate OR was unjailed */
  startHeight: number;
  /** Timestamp until which the validator is jailed due to liveness downtime. */
  jailedUntil: Date | undefined;
  /**
   * Whether or not a validator has been tombstoned (killed out of validator set). It is set
   * once the validator commits an equivocation or for any other configured misbehiavor.
   */
  tombstoned: boolean;
  /**
   * A counter kept to avoid unnecessary array reads.
   * Note that `Sum(MissedBlocksBitArray)` always equals `MissedBlocksCounter`.
   */
  missedBlocksCounter: number;
}

/**
 * ValidatorSigningInfo defines a validator's signing info for monitoring their
 * liveness activity.
 */
export interface ValidatorSigningInfo {
  address: string;
  /** Height at which validator was first a candidate OR was unjailed */
  startHeight: number;
  /**
   * Index which is incremented each time the validator was a bonded
   * in a block and may have signed a precommit or not. This in conjunction with the
   * `SignedBlocksWindow` param determines the index in the `MissedBlocksBitArray`.
   */
  indexOffset: number;
  /** Timestamp until which the validator is jailed due to liveness downtime. */
  jailedUntil: Date | undefined;
  /**
   * Whether or not a validator has been tombstoned (killed out of validator set). It is set
   * once the validator commits an equivocation or for any other configured misbehiavor.
   */
  tombstoned: boolean;
  /**
   * A counter kept to avoid unnecessary array reads.
   * Note that `Sum(MissedBlocksBitArray)` always equals `MissedBlocksCounter`.
   */
  missedBlocksCounter: number;
}

/** Stores a sliding window of the last `signed_blocks_window` blocks indicating whether the validator missed the block */
export interface ValidatorMissedBlockArrayLegacyMissedHeights {
  address: string;
  /** Array of contains the heights when the validator missed the block */
  missedHeights: number[];
}

/** Stores a sliding window of the last `signed_blocks_window` blocks indicating whether the validator missed the block */
export interface ValidatorMissedBlockArray {
  address: string;
  /** store this in case window size changes but doesn't affect number of bit groups */
  windowSize: number;
  /** Array of contains the missed block bits packed into uint64s */
  missedBlocks: number[];
}

/** Params represents the parameters used for by the slashing module. */
export interface Params {
  signedBlocksWindow: number;
  minSignedPerWindow: Uint8Array;
  downtimeJailDuration: Duration | undefined;
  slashFractionDoubleSign: Uint8Array;
  slashFractionDowntime: Uint8Array;
}

const baseValidatorSigningInfoLegacyMissedHeights: object = {
  address: "",
  startHeight: 0,
  tombstoned: false,
  missedBlocksCounter: 0,
};

export const ValidatorSigningInfoLegacyMissedHeights = {
  encode(
    message: ValidatorSigningInfoLegacyMissedHeights,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    if (message.startHeight !== 0) {
      writer.uint32(16).int64(message.startHeight);
    }
    if (message.jailedUntil !== undefined) {
      Timestamp.encode(
        toTimestamp(message.jailedUntil),
        writer.uint32(26).fork()
      ).ldelim();
    }
    if (message.tombstoned === true) {
      writer.uint32(32).bool(message.tombstoned);
    }
    if (message.missedBlocksCounter !== 0) {
      writer.uint32(40).int64(message.missedBlocksCounter);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ValidatorSigningInfoLegacyMissedHeights {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseValidatorSigningInfoLegacyMissedHeights,
    } as ValidatorSigningInfoLegacyMissedHeights;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
          break;
        case 2:
          message.startHeight = longToNumber(reader.int64() as Long);
          break;
        case 3:
          message.jailedUntil = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 4:
          message.tombstoned = reader.bool();
          break;
        case 5:
          message.missedBlocksCounter = longToNumber(reader.int64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorSigningInfoLegacyMissedHeights {
    const message = {
      ...baseValidatorSigningInfoLegacyMissedHeights,
    } as ValidatorSigningInfoLegacyMissedHeights;
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (object.startHeight !== undefined && object.startHeight !== null) {
      message.startHeight = Number(object.startHeight);
    } else {
      message.startHeight = 0;
    }
    if (object.jailedUntil !== undefined && object.jailedUntil !== null) {
      message.jailedUntil = fromJsonTimestamp(object.jailedUntil);
    } else {
      message.jailedUntil = undefined;
    }
    if (object.tombstoned !== undefined && object.tombstoned !== null) {
      message.tombstoned = Boolean(object.tombstoned);
    } else {
      message.tombstoned = false;
    }
    if (
      object.missedBlocksCounter !== undefined &&
      object.missedBlocksCounter !== null
    ) {
      message.missedBlocksCounter = Number(object.missedBlocksCounter);
    } else {
      message.missedBlocksCounter = 0;
    }
    return message;
  },

  toJSON(message: ValidatorSigningInfoLegacyMissedHeights): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    message.startHeight !== undefined &&
      (obj.startHeight = message.startHeight);
    message.jailedUntil !== undefined &&
      (obj.jailedUntil =
        message.jailedUntil !== undefined
          ? message.jailedUntil.toISOString()
          : null);
    message.tombstoned !== undefined && (obj.tombstoned = message.tombstoned);
    message.missedBlocksCounter !== undefined &&
      (obj.missedBlocksCounter = message.missedBlocksCounter);
    return obj;
  },

  fromPartial(
    object: DeepPartial<ValidatorSigningInfoLegacyMissedHeights>
  ): ValidatorSigningInfoLegacyMissedHeights {
    const message = {
      ...baseValidatorSigningInfoLegacyMissedHeights,
    } as ValidatorSigningInfoLegacyMissedHeights;
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.startHeight !== undefined && object.startHeight !== null) {
      message.startHeight = object.startHeight;
    } else {
      message.startHeight = 0;
    }
    if (object.jailedUntil !== undefined && object.jailedUntil !== null) {
      message.jailedUntil = object.jailedUntil;
    } else {
      message.jailedUntil = undefined;
    }
    if (object.tombstoned !== undefined && object.tombstoned !== null) {
      message.tombstoned = object.tombstoned;
    } else {
      message.tombstoned = false;
    }
    if (
      object.missedBlocksCounter !== undefined &&
      object.missedBlocksCounter !== null
    ) {
      message.missedBlocksCounter = object.missedBlocksCounter;
    } else {
      message.missedBlocksCounter = 0;
    }
    return message;
  },
};

const baseValidatorSigningInfo: object = {
  address: "",
  startHeight: 0,
  indexOffset: 0,
  tombstoned: false,
  missedBlocksCounter: 0,
};

export const ValidatorSigningInfo = {
  encode(
    message: ValidatorSigningInfo,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    if (message.startHeight !== 0) {
      writer.uint32(16).int64(message.startHeight);
    }
    if (message.indexOffset !== 0) {
      writer.uint32(24).int64(message.indexOffset);
    }
    if (message.jailedUntil !== undefined) {
      Timestamp.encode(
        toTimestamp(message.jailedUntil),
        writer.uint32(34).fork()
      ).ldelim();
    }
    if (message.tombstoned === true) {
      writer.uint32(40).bool(message.tombstoned);
    }
    if (message.missedBlocksCounter !== 0) {
      writer.uint32(48).int64(message.missedBlocksCounter);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ValidatorSigningInfo {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseValidatorSigningInfo } as ValidatorSigningInfo;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
          break;
        case 2:
          message.startHeight = longToNumber(reader.int64() as Long);
          break;
        case 3:
          message.indexOffset = longToNumber(reader.int64() as Long);
          break;
        case 4:
          message.jailedUntil = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 5:
          message.tombstoned = reader.bool();
          break;
        case 6:
          message.missedBlocksCounter = longToNumber(reader.int64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorSigningInfo {
    const message = { ...baseValidatorSigningInfo } as ValidatorSigningInfo;
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (object.startHeight !== undefined && object.startHeight !== null) {
      message.startHeight = Number(object.startHeight);
    } else {
      message.startHeight = 0;
    }
    if (object.indexOffset !== undefined && object.indexOffset !== null) {
      message.indexOffset = Number(object.indexOffset);
    } else {
      message.indexOffset = 0;
    }
    if (object.jailedUntil !== undefined && object.jailedUntil !== null) {
      message.jailedUntil = fromJsonTimestamp(object.jailedUntil);
    } else {
      message.jailedUntil = undefined;
    }
    if (object.tombstoned !== undefined && object.tombstoned !== null) {
      message.tombstoned = Boolean(object.tombstoned);
    } else {
      message.tombstoned = false;
    }
    if (
      object.missedBlocksCounter !== undefined &&
      object.missedBlocksCounter !== null
    ) {
      message.missedBlocksCounter = Number(object.missedBlocksCounter);
    } else {
      message.missedBlocksCounter = 0;
    }
    return message;
  },

  toJSON(message: ValidatorSigningInfo): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    message.startHeight !== undefined &&
      (obj.startHeight = message.startHeight);
    message.indexOffset !== undefined &&
      (obj.indexOffset = message.indexOffset);
    message.jailedUntil !== undefined &&
      (obj.jailedUntil =
        message.jailedUntil !== undefined
          ? message.jailedUntil.toISOString()
          : null);
    message.tombstoned !== undefined && (obj.tombstoned = message.tombstoned);
    message.missedBlocksCounter !== undefined &&
      (obj.missedBlocksCounter = message.missedBlocksCounter);
    return obj;
  },

  fromPartial(object: DeepPartial<ValidatorSigningInfo>): ValidatorSigningInfo {
    const message = { ...baseValidatorSigningInfo } as ValidatorSigningInfo;
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.startHeight !== undefined && object.startHeight !== null) {
      message.startHeight = object.startHeight;
    } else {
      message.startHeight = 0;
    }
    if (object.indexOffset !== undefined && object.indexOffset !== null) {
      message.indexOffset = object.indexOffset;
    } else {
      message.indexOffset = 0;
    }
    if (object.jailedUntil !== undefined && object.jailedUntil !== null) {
      message.jailedUntil = object.jailedUntil;
    } else {
      message.jailedUntil = undefined;
    }
    if (object.tombstoned !== undefined && object.tombstoned !== null) {
      message.tombstoned = object.tombstoned;
    } else {
      message.tombstoned = false;
    }
    if (
      object.missedBlocksCounter !== undefined &&
      object.missedBlocksCounter !== null
    ) {
      message.missedBlocksCounter = object.missedBlocksCounter;
    } else {
      message.missedBlocksCounter = 0;
    }
    return message;
  },
};

const baseValidatorMissedBlockArrayLegacyMissedHeights: object = {
  address: "",
  missedHeights: 0,
};

export const ValidatorMissedBlockArrayLegacyMissedHeights = {
  encode(
    message: ValidatorMissedBlockArrayLegacyMissedHeights,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    writer.uint32(18).fork();
    for (const v of message.missedHeights) {
      writer.int64(v);
    }
    writer.ldelim();
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ValidatorMissedBlockArrayLegacyMissedHeights {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseValidatorMissedBlockArrayLegacyMissedHeights,
    } as ValidatorMissedBlockArrayLegacyMissedHeights;
    message.missedHeights = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
          break;
        case 2:
          if ((tag & 7) === 2) {
            const end2 = reader.uint32() + reader.pos;
            while (reader.pos < end2) {
              message.missedHeights.push(longToNumber(reader.int64() as Long));
            }
          } else {
            message.missedHeights.push(longToNumber(reader.int64() as Long));
          }
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorMissedBlockArrayLegacyMissedHeights {
    const message = {
      ...baseValidatorMissedBlockArrayLegacyMissedHeights,
    } as ValidatorMissedBlockArrayLegacyMissedHeights;
    message.missedHeights = [];
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (object.missedHeights !== undefined && object.missedHeights !== null) {
      for (const e of object.missedHeights) {
        message.missedHeights.push(Number(e));
      }
    }
    return message;
  },

  toJSON(message: ValidatorMissedBlockArrayLegacyMissedHeights): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    if (message.missedHeights) {
      obj.missedHeights = message.missedHeights.map((e) => e);
    } else {
      obj.missedHeights = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<ValidatorMissedBlockArrayLegacyMissedHeights>
  ): ValidatorMissedBlockArrayLegacyMissedHeights {
    const message = {
      ...baseValidatorMissedBlockArrayLegacyMissedHeights,
    } as ValidatorMissedBlockArrayLegacyMissedHeights;
    message.missedHeights = [];
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.missedHeights !== undefined && object.missedHeights !== null) {
      for (const e of object.missedHeights) {
        message.missedHeights.push(e);
      }
    }
    return message;
  },
};

const baseValidatorMissedBlockArray: object = {
  address: "",
  windowSize: 0,
  missedBlocks: 0,
};

export const ValidatorMissedBlockArray = {
  encode(
    message: ValidatorMissedBlockArray,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    if (message.windowSize !== 0) {
      writer.uint32(16).int64(message.windowSize);
    }
    writer.uint32(26).fork();
    for (const v of message.missedBlocks) {
      writer.uint64(v);
    }
    writer.ldelim();
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ValidatorMissedBlockArray {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseValidatorMissedBlockArray,
    } as ValidatorMissedBlockArray;
    message.missedBlocks = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
          break;
        case 2:
          message.windowSize = longToNumber(reader.int64() as Long);
          break;
        case 3:
          if ((tag & 7) === 2) {
            const end2 = reader.uint32() + reader.pos;
            while (reader.pos < end2) {
              message.missedBlocks.push(longToNumber(reader.uint64() as Long));
            }
          } else {
            message.missedBlocks.push(longToNumber(reader.uint64() as Long));
          }
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorMissedBlockArray {
    const message = {
      ...baseValidatorMissedBlockArray,
    } as ValidatorMissedBlockArray;
    message.missedBlocks = [];
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (object.windowSize !== undefined && object.windowSize !== null) {
      message.windowSize = Number(object.windowSize);
    } else {
      message.windowSize = 0;
    }
    if (object.missedBlocks !== undefined && object.missedBlocks !== null) {
      for (const e of object.missedBlocks) {
        message.missedBlocks.push(Number(e));
      }
    }
    return message;
  },

  toJSON(message: ValidatorMissedBlockArray): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    message.windowSize !== undefined && (obj.windowSize = message.windowSize);
    if (message.missedBlocks) {
      obj.missedBlocks = message.missedBlocks.map((e) => e);
    } else {
      obj.missedBlocks = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<ValidatorMissedBlockArray>
  ): ValidatorMissedBlockArray {
    const message = {
      ...baseValidatorMissedBlockArray,
    } as ValidatorMissedBlockArray;
    message.missedBlocks = [];
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.windowSize !== undefined && object.windowSize !== null) {
      message.windowSize = object.windowSize;
    } else {
      message.windowSize = 0;
    }
    if (object.missedBlocks !== undefined && object.missedBlocks !== null) {
      for (const e of object.missedBlocks) {
        message.missedBlocks.push(e);
      }
    }
    return message;
  },
};

const baseParams: object = { signedBlocksWindow: 0 };

export const Params = {
  encode(message: Params, writer: Writer = Writer.create()): Writer {
    if (message.signedBlocksWindow !== 0) {
      writer.uint32(8).int64(message.signedBlocksWindow);
    }
    if (message.minSignedPerWindow.length !== 0) {
      writer.uint32(18).bytes(message.minSignedPerWindow);
    }
    if (message.downtimeJailDuration !== undefined) {
      Duration.encode(
        message.downtimeJailDuration,
        writer.uint32(26).fork()
      ).ldelim();
    }
    if (message.slashFractionDoubleSign.length !== 0) {
      writer.uint32(34).bytes(message.slashFractionDoubleSign);
    }
    if (message.slashFractionDowntime.length !== 0) {
      writer.uint32(42).bytes(message.slashFractionDowntime);
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
          message.signedBlocksWindow = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.minSignedPerWindow = reader.bytes();
          break;
        case 3:
          message.downtimeJailDuration = Duration.decode(
            reader,
            reader.uint32()
          );
          break;
        case 4:
          message.slashFractionDoubleSign = reader.bytes();
          break;
        case 5:
          message.slashFractionDowntime = reader.bytes();
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
    if (
      object.signedBlocksWindow !== undefined &&
      object.signedBlocksWindow !== null
    ) {
      message.signedBlocksWindow = Number(object.signedBlocksWindow);
    } else {
      message.signedBlocksWindow = 0;
    }
    if (
      object.minSignedPerWindow !== undefined &&
      object.minSignedPerWindow !== null
    ) {
      message.minSignedPerWindow = bytesFromBase64(object.minSignedPerWindow);
    }
    if (
      object.downtimeJailDuration !== undefined &&
      object.downtimeJailDuration !== null
    ) {
      message.downtimeJailDuration = Duration.fromJSON(
        object.downtimeJailDuration
      );
    } else {
      message.downtimeJailDuration = undefined;
    }
    if (
      object.slashFractionDoubleSign !== undefined &&
      object.slashFractionDoubleSign !== null
    ) {
      message.slashFractionDoubleSign = bytesFromBase64(
        object.slashFractionDoubleSign
      );
    }
    if (
      object.slashFractionDowntime !== undefined &&
      object.slashFractionDowntime !== null
    ) {
      message.slashFractionDowntime = bytesFromBase64(
        object.slashFractionDowntime
      );
    }
    return message;
  },

  toJSON(message: Params): unknown {
    const obj: any = {};
    message.signedBlocksWindow !== undefined &&
      (obj.signedBlocksWindow = message.signedBlocksWindow);
    message.minSignedPerWindow !== undefined &&
      (obj.minSignedPerWindow = base64FromBytes(
        message.minSignedPerWindow !== undefined
          ? message.minSignedPerWindow
          : new Uint8Array()
      ));
    message.downtimeJailDuration !== undefined &&
      (obj.downtimeJailDuration = message.downtimeJailDuration
        ? Duration.toJSON(message.downtimeJailDuration)
        : undefined);
    message.slashFractionDoubleSign !== undefined &&
      (obj.slashFractionDoubleSign = base64FromBytes(
        message.slashFractionDoubleSign !== undefined
          ? message.slashFractionDoubleSign
          : new Uint8Array()
      ));
    message.slashFractionDowntime !== undefined &&
      (obj.slashFractionDowntime = base64FromBytes(
        message.slashFractionDowntime !== undefined
          ? message.slashFractionDowntime
          : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<Params>): Params {
    const message = { ...baseParams } as Params;
    if (
      object.signedBlocksWindow !== undefined &&
      object.signedBlocksWindow !== null
    ) {
      message.signedBlocksWindow = object.signedBlocksWindow;
    } else {
      message.signedBlocksWindow = 0;
    }
    if (
      object.minSignedPerWindow !== undefined &&
      object.minSignedPerWindow !== null
    ) {
      message.minSignedPerWindow = object.minSignedPerWindow;
    } else {
      message.minSignedPerWindow = new Uint8Array();
    }
    if (
      object.downtimeJailDuration !== undefined &&
      object.downtimeJailDuration !== null
    ) {
      message.downtimeJailDuration = Duration.fromPartial(
        object.downtimeJailDuration
      );
    } else {
      message.downtimeJailDuration = undefined;
    }
    if (
      object.slashFractionDoubleSign !== undefined &&
      object.slashFractionDoubleSign !== null
    ) {
      message.slashFractionDoubleSign = object.slashFractionDoubleSign;
    } else {
      message.slashFractionDoubleSign = new Uint8Array();
    }
    if (
      object.slashFractionDowntime !== undefined &&
      object.slashFractionDowntime !== null
    ) {
      message.slashFractionDowntime = object.slashFractionDowntime;
    } else {
      message.slashFractionDowntime = new Uint8Array();
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

function toTimestamp(date: Date): Timestamp {
  const seconds = date.getTime() / 1_000;
  const nanos = (date.getTime() % 1_000) * 1_000_000;
  return { seconds, nanos };
}

function fromTimestamp(t: Timestamp): Date {
  let millis = t.seconds * 1_000;
  millis += t.nanos / 1_000_000;
  return new Date(millis);
}

function fromJsonTimestamp(o: any): Date {
  if (o instanceof Date) {
    return o;
  } else if (typeof o === "string") {
    return new Date(o);
  } else {
    return fromTimestamp(Timestamp.fromJSON(o));
  }
}

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
