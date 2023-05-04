/* eslint-disable */
import { Timestamp } from "../google/protobuf/timestamp";
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Duration } from "../google/protobuf/duration";

export const protobufPackage = "seiprotocol.seichain.epoch";

export interface Epoch {
  genesis_time: Date | undefined;
  epoch_duration: Duration | undefined;
  current_epoch: number;
  current_epoch_start_time: Date | undefined;
  current_epoch_height: number;
}

const baseEpoch: object = { current_epoch: 0, current_epoch_height: 0 };

export const Epoch = {
  encode(message: Epoch, writer: Writer = Writer.create()): Writer {
    if (message.genesis_time !== undefined) {
      Timestamp.encode(
        toTimestamp(message.genesis_time),
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.epoch_duration !== undefined) {
      Duration.encode(
        message.epoch_duration,
        writer.uint32(18).fork()
      ).ldelim();
    }
    if (message.current_epoch !== 0) {
      writer.uint32(24).uint64(message.current_epoch);
    }
    if (message.current_epoch_start_time !== undefined) {
      Timestamp.encode(
        toTimestamp(message.current_epoch_start_time),
        writer.uint32(34).fork()
      ).ldelim();
    }
    if (message.current_epoch_height !== 0) {
      writer.uint32(40).int64(message.current_epoch_height);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Epoch {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseEpoch } as Epoch;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.genesis_time = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 2:
          message.epoch_duration = Duration.decode(reader, reader.uint32());
          break;
        case 3:
          message.current_epoch = longToNumber(reader.uint64() as Long);
          break;
        case 4:
          message.current_epoch_start_time = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 5:
          message.current_epoch_height = longToNumber(reader.int64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Epoch {
    const message = { ...baseEpoch } as Epoch;
    if (object.genesis_time !== undefined && object.genesis_time !== null) {
      message.genesis_time = fromJsonTimestamp(object.genesis_time);
    } else {
      message.genesis_time = undefined;
    }
    if (object.epoch_duration !== undefined && object.epoch_duration !== null) {
      message.epoch_duration = Duration.fromJSON(object.epoch_duration);
    } else {
      message.epoch_duration = undefined;
    }
    if (object.current_epoch !== undefined && object.current_epoch !== null) {
      message.current_epoch = Number(object.current_epoch);
    } else {
      message.current_epoch = 0;
    }
    if (
      object.current_epoch_start_time !== undefined &&
      object.current_epoch_start_time !== null
    ) {
      message.current_epoch_start_time = fromJsonTimestamp(
        object.current_epoch_start_time
      );
    } else {
      message.current_epoch_start_time = undefined;
    }
    if (
      object.current_epoch_height !== undefined &&
      object.current_epoch_height !== null
    ) {
      message.current_epoch_height = Number(object.current_epoch_height);
    } else {
      message.current_epoch_height = 0;
    }
    return message;
  },

  toJSON(message: Epoch): unknown {
    const obj: any = {};
    message.genesis_time !== undefined &&
      (obj.genesis_time =
        message.genesis_time !== undefined
          ? message.genesis_time.toISOString()
          : null);
    message.epoch_duration !== undefined &&
      (obj.epoch_duration = message.epoch_duration
        ? Duration.toJSON(message.epoch_duration)
        : undefined);
    message.current_epoch !== undefined &&
      (obj.current_epoch = message.current_epoch);
    message.current_epoch_start_time !== undefined &&
      (obj.current_epoch_start_time =
        message.current_epoch_start_time !== undefined
          ? message.current_epoch_start_time.toISOString()
          : null);
    message.current_epoch_height !== undefined &&
      (obj.current_epoch_height = message.current_epoch_height);
    return obj;
  },

  fromPartial(object: DeepPartial<Epoch>): Epoch {
    const message = { ...baseEpoch } as Epoch;
    if (object.genesis_time !== undefined && object.genesis_time !== null) {
      message.genesis_time = object.genesis_time;
    } else {
      message.genesis_time = undefined;
    }
    if (object.epoch_duration !== undefined && object.epoch_duration !== null) {
      message.epoch_duration = Duration.fromPartial(object.epoch_duration);
    } else {
      message.epoch_duration = undefined;
    }
    if (object.current_epoch !== undefined && object.current_epoch !== null) {
      message.current_epoch = object.current_epoch;
    } else {
      message.current_epoch = 0;
    }
    if (
      object.current_epoch_start_time !== undefined &&
      object.current_epoch_start_time !== null
    ) {
      message.current_epoch_start_time = object.current_epoch_start_time;
    } else {
      message.current_epoch_start_time = undefined;
    }
    if (
      object.current_epoch_height !== undefined &&
      object.current_epoch_height !== null
    ) {
      message.current_epoch_height = object.current_epoch_height;
    } else {
      message.current_epoch_height = 0;
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
