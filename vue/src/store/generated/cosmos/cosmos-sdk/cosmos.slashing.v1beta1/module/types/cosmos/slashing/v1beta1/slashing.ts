/* eslint-disable */
import { Timestamp } from "../../../google/protobuf/timestamp";
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Duration } from "../../../google/protobuf/duration";

export const protobufPackage = "cosmos.slashing.v1beta1";

/**
 * ValidatorSigningInfo defines a validator's signing info for monitoring their
 * liveness activity.
 */
export interface ValidatorSigningInfo {
  address: string;
  /** Height at which validator was first a candidate OR was unjailed */
  start_height: number;
  /**
   * Index which is incremented each time the validator was a bonded
   * in a block and may have signed a precommit or not. This in conjunction with the
   * `SignedBlocksWindow` param determines the index in the `MissedBlocksBitArray`.
   */
  index_offset: number;
  /** Timestamp until which the validator is jailed due to liveness downtime. */
  jailed_until: Date | undefined;
  /**
   * Whether or not a validator has been tombstoned (killed out of validator set). It is set
   * once the validator commits an equivocation or for any other configured misbehiavor.
   */
  tombstoned: boolean;
  /**
   * A counter kept to avoid unnecessary array reads.
   * Note that `Sum(MissedBlocksBitArray)` always equals `MissedBlocksCounter`.
   */
  missed_blocks_counter: number;
}

/** Params represents the parameters used for by the slashing module. */
export interface Params {
  signed_blocks_window: number;
  min_signed_per_window: Uint8Array;
  downtime_jail_duration: Duration | undefined;
  slash_fraction_double_sign: Uint8Array;
  slash_fraction_downtime: Uint8Array;
}

const baseValidatorSigningInfo: object = {
  address: "",
  start_height: 0,
  index_offset: 0,
  tombstoned: false,
  missed_blocks_counter: 0,
};

export const ValidatorSigningInfo = {
  encode(
    message: ValidatorSigningInfo,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    if (message.start_height !== 0) {
      writer.uint32(16).int64(message.start_height);
    }
    if (message.index_offset !== 0) {
      writer.uint32(24).int64(message.index_offset);
    }
    if (message.jailed_until !== undefined) {
      Timestamp.encode(
        toTimestamp(message.jailed_until),
        writer.uint32(34).fork()
      ).ldelim();
    }
    if (message.tombstoned === true) {
      writer.uint32(40).bool(message.tombstoned);
    }
    if (message.missed_blocks_counter !== 0) {
      writer.uint32(48).int64(message.missed_blocks_counter);
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
          message.start_height = longToNumber(reader.int64() as Long);
          break;
        case 3:
          message.index_offset = longToNumber(reader.int64() as Long);
          break;
        case 4:
          message.jailed_until = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 5:
          message.tombstoned = reader.bool();
          break;
        case 6:
          message.missed_blocks_counter = longToNumber(reader.int64() as Long);
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
    if (object.start_height !== undefined && object.start_height !== null) {
      message.start_height = Number(object.start_height);
    } else {
      message.start_height = 0;
    }
    if (object.index_offset !== undefined && object.index_offset !== null) {
      message.index_offset = Number(object.index_offset);
    } else {
      message.index_offset = 0;
    }
    if (object.jailed_until !== undefined && object.jailed_until !== null) {
      message.jailed_until = fromJsonTimestamp(object.jailed_until);
    } else {
      message.jailed_until = undefined;
    }
    if (object.tombstoned !== undefined && object.tombstoned !== null) {
      message.tombstoned = Boolean(object.tombstoned);
    } else {
      message.tombstoned = false;
    }
    if (
      object.missed_blocks_counter !== undefined &&
      object.missed_blocks_counter !== null
    ) {
      message.missed_blocks_counter = Number(object.missed_blocks_counter);
    } else {
      message.missed_blocks_counter = 0;
    }
    return message;
  },

  toJSON(message: ValidatorSigningInfo): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    message.start_height !== undefined &&
      (obj.start_height = message.start_height);
    message.index_offset !== undefined &&
      (obj.index_offset = message.index_offset);
    message.jailed_until !== undefined &&
      (obj.jailed_until =
        message.jailed_until !== undefined
          ? message.jailed_until.toISOString()
          : null);
    message.tombstoned !== undefined && (obj.tombstoned = message.tombstoned);
    message.missed_blocks_counter !== undefined &&
      (obj.missed_blocks_counter = message.missed_blocks_counter);
    return obj;
  },

  fromPartial(object: DeepPartial<ValidatorSigningInfo>): ValidatorSigningInfo {
    const message = { ...baseValidatorSigningInfo } as ValidatorSigningInfo;
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.start_height !== undefined && object.start_height !== null) {
      message.start_height = object.start_height;
    } else {
      message.start_height = 0;
    }
    if (object.index_offset !== undefined && object.index_offset !== null) {
      message.index_offset = object.index_offset;
    } else {
      message.index_offset = 0;
    }
    if (object.jailed_until !== undefined && object.jailed_until !== null) {
      message.jailed_until = object.jailed_until;
    } else {
      message.jailed_until = undefined;
    }
    if (object.tombstoned !== undefined && object.tombstoned !== null) {
      message.tombstoned = object.tombstoned;
    } else {
      message.tombstoned = false;
    }
    if (
      object.missed_blocks_counter !== undefined &&
      object.missed_blocks_counter !== null
    ) {
      message.missed_blocks_counter = object.missed_blocks_counter;
    } else {
      message.missed_blocks_counter = 0;
    }
    return message;
  },
};

const baseParams: object = { signed_blocks_window: 0 };

export const Params = {
  encode(message: Params, writer: Writer = Writer.create()): Writer {
    if (message.signed_blocks_window !== 0) {
      writer.uint32(8).int64(message.signed_blocks_window);
    }
    if (message.min_signed_per_window.length !== 0) {
      writer.uint32(18).bytes(message.min_signed_per_window);
    }
    if (message.downtime_jail_duration !== undefined) {
      Duration.encode(
        message.downtime_jail_duration,
        writer.uint32(26).fork()
      ).ldelim();
    }
    if (message.slash_fraction_double_sign.length !== 0) {
      writer.uint32(34).bytes(message.slash_fraction_double_sign);
    }
    if (message.slash_fraction_downtime.length !== 0) {
      writer.uint32(42).bytes(message.slash_fraction_downtime);
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
          message.signed_blocks_window = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.min_signed_per_window = reader.bytes();
          break;
        case 3:
          message.downtime_jail_duration = Duration.decode(
            reader,
            reader.uint32()
          );
          break;
        case 4:
          message.slash_fraction_double_sign = reader.bytes();
          break;
        case 5:
          message.slash_fraction_downtime = reader.bytes();
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
      object.signed_blocks_window !== undefined &&
      object.signed_blocks_window !== null
    ) {
      message.signed_blocks_window = Number(object.signed_blocks_window);
    } else {
      message.signed_blocks_window = 0;
    }
    if (
      object.min_signed_per_window !== undefined &&
      object.min_signed_per_window !== null
    ) {
      message.min_signed_per_window = bytesFromBase64(
        object.min_signed_per_window
      );
    }
    if (
      object.downtime_jail_duration !== undefined &&
      object.downtime_jail_duration !== null
    ) {
      message.downtime_jail_duration = Duration.fromJSON(
        object.downtime_jail_duration
      );
    } else {
      message.downtime_jail_duration = undefined;
    }
    if (
      object.slash_fraction_double_sign !== undefined &&
      object.slash_fraction_double_sign !== null
    ) {
      message.slash_fraction_double_sign = bytesFromBase64(
        object.slash_fraction_double_sign
      );
    }
    if (
      object.slash_fraction_downtime !== undefined &&
      object.slash_fraction_downtime !== null
    ) {
      message.slash_fraction_downtime = bytesFromBase64(
        object.slash_fraction_downtime
      );
    }
    return message;
  },

  toJSON(message: Params): unknown {
    const obj: any = {};
    message.signed_blocks_window !== undefined &&
      (obj.signed_blocks_window = message.signed_blocks_window);
    message.min_signed_per_window !== undefined &&
      (obj.min_signed_per_window = base64FromBytes(
        message.min_signed_per_window !== undefined
          ? message.min_signed_per_window
          : new Uint8Array()
      ));
    message.downtime_jail_duration !== undefined &&
      (obj.downtime_jail_duration = message.downtime_jail_duration
        ? Duration.toJSON(message.downtime_jail_duration)
        : undefined);
    message.slash_fraction_double_sign !== undefined &&
      (obj.slash_fraction_double_sign = base64FromBytes(
        message.slash_fraction_double_sign !== undefined
          ? message.slash_fraction_double_sign
          : new Uint8Array()
      ));
    message.slash_fraction_downtime !== undefined &&
      (obj.slash_fraction_downtime = base64FromBytes(
        message.slash_fraction_downtime !== undefined
          ? message.slash_fraction_downtime
          : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<Params>): Params {
    const message = { ...baseParams } as Params;
    if (
      object.signed_blocks_window !== undefined &&
      object.signed_blocks_window !== null
    ) {
      message.signed_blocks_window = object.signed_blocks_window;
    } else {
      message.signed_blocks_window = 0;
    }
    if (
      object.min_signed_per_window !== undefined &&
      object.min_signed_per_window !== null
    ) {
      message.min_signed_per_window = object.min_signed_per_window;
    } else {
      message.min_signed_per_window = new Uint8Array();
    }
    if (
      object.downtime_jail_duration !== undefined &&
      object.downtime_jail_duration !== null
    ) {
      message.downtime_jail_duration = Duration.fromPartial(
        object.downtime_jail_duration
      );
    } else {
      message.downtime_jail_duration = undefined;
    }
    if (
      object.slash_fraction_double_sign !== undefined &&
      object.slash_fraction_double_sign !== null
    ) {
      message.slash_fraction_double_sign = object.slash_fraction_double_sign;
    } else {
      message.slash_fraction_double_sign = new Uint8Array();
    }
    if (
      object.slash_fraction_downtime !== undefined &&
      object.slash_fraction_downtime !== null
    ) {
      message.slash_fraction_downtime = object.slash_fraction_downtime;
    } else {
      message.slash_fraction_downtime = new Uint8Array();
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
