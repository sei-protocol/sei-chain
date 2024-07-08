/* eslint-disable */
import { Timestamp } from "../../google/protobuf/timestamp";
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Duration } from "../../google/protobuf/duration";

export const protobufPackage = "sei.epoch.v1";

export interface Epoch {
  genesisTime: Date | undefined;
  epochDuration: Duration | undefined;
  currentEpoch: number;
  currentEpochStartTime: Date | undefined;
  currentEpochHeight: number;
}

const baseEpoch: object = { currentEpoch: 0, currentEpochHeight: 0 };

export const Epoch = {
  encode(message: Epoch, writer: Writer = Writer.create()): Writer {
    if (message.genesisTime !== undefined) {
      Timestamp.encode(
        toTimestamp(message.genesisTime),
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.epochDuration !== undefined) {
      Duration.encode(message.epochDuration, writer.uint32(18).fork()).ldelim();
    }
    if (message.currentEpoch !== 0) {
      writer.uint32(24).uint64(message.currentEpoch);
    }
    if (message.currentEpochStartTime !== undefined) {
      Timestamp.encode(
        toTimestamp(message.currentEpochStartTime),
        writer.uint32(34).fork()
      ).ldelim();
    }
    if (message.currentEpochHeight !== 0) {
      writer.uint32(40).int64(message.currentEpochHeight);
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
          message.genesisTime = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 2:
          message.epochDuration = Duration.decode(reader, reader.uint32());
          break;
        case 3:
          message.currentEpoch = longToNumber(reader.uint64() as Long);
          break;
        case 4:
          message.currentEpochStartTime = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 5:
          message.currentEpochHeight = longToNumber(reader.int64() as Long);
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
    if (object.genesisTime !== undefined && object.genesisTime !== null) {
      message.genesisTime = fromJsonTimestamp(object.genesisTime);
    } else {
      message.genesisTime = undefined;
    }
    if (object.epochDuration !== undefined && object.epochDuration !== null) {
      message.epochDuration = Duration.fromJSON(object.epochDuration);
    } else {
      message.epochDuration = undefined;
    }
    if (object.currentEpoch !== undefined && object.currentEpoch !== null) {
      message.currentEpoch = Number(object.currentEpoch);
    } else {
      message.currentEpoch = 0;
    }
    if (
      object.currentEpochStartTime !== undefined &&
      object.currentEpochStartTime !== null
    ) {
      message.currentEpochStartTime = fromJsonTimestamp(
        object.currentEpochStartTime
      );
    } else {
      message.currentEpochStartTime = undefined;
    }
    if (
      object.currentEpochHeight !== undefined &&
      object.currentEpochHeight !== null
    ) {
      message.currentEpochHeight = Number(object.currentEpochHeight);
    } else {
      message.currentEpochHeight = 0;
    }
    return message;
  },

  toJSON(message: Epoch): unknown {
    const obj: any = {};
    message.genesisTime !== undefined &&
      (obj.genesisTime =
        message.genesisTime !== undefined
          ? message.genesisTime.toISOString()
          : null);
    message.epochDuration !== undefined &&
      (obj.epochDuration = message.epochDuration
        ? Duration.toJSON(message.epochDuration)
        : undefined);
    message.currentEpoch !== undefined &&
      (obj.currentEpoch = message.currentEpoch);
    message.currentEpochStartTime !== undefined &&
      (obj.currentEpochStartTime =
        message.currentEpochStartTime !== undefined
          ? message.currentEpochStartTime.toISOString()
          : null);
    message.currentEpochHeight !== undefined &&
      (obj.currentEpochHeight = message.currentEpochHeight);
    return obj;
  },

  fromPartial(object: DeepPartial<Epoch>): Epoch {
    const message = { ...baseEpoch } as Epoch;
    if (object.genesisTime !== undefined && object.genesisTime !== null) {
      message.genesisTime = object.genesisTime;
    } else {
      message.genesisTime = undefined;
    }
    if (object.epochDuration !== undefined && object.epochDuration !== null) {
      message.epochDuration = Duration.fromPartial(object.epochDuration);
    } else {
      message.epochDuration = undefined;
    }
    if (object.currentEpoch !== undefined && object.currentEpoch !== null) {
      message.currentEpoch = object.currentEpoch;
    } else {
      message.currentEpoch = 0;
    }
    if (
      object.currentEpochStartTime !== undefined &&
      object.currentEpochStartTime !== null
    ) {
      message.currentEpochStartTime = object.currentEpochStartTime;
    } else {
      message.currentEpochStartTime = undefined;
    }
    if (
      object.currentEpochHeight !== undefined &&
      object.currentEpochHeight !== null
    ) {
      message.currentEpochHeight = object.currentEpochHeight;
    } else {
      message.currentEpochHeight = 0;
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
