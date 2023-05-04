/* eslint-disable */
import { Timestamp } from "../../../google/protobuf/timestamp";
import { Coin } from "../../../cosmos/base/v1beta1/coin";
import { Duration } from "../../../google/protobuf/duration";
import { Any } from "../../../google/protobuf/any";
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "cosmos.feegrant.v1beta1";

/** Since: cosmos-sdk 0.43 */

/**
 * BasicAllowance implements Allowance with a one-time grant of tokens
 * that optionally expires. The grantee can use up to SpendLimit to cover fees.
 */
export interface BasicAllowance {
  /**
   * spend_limit specifies the maximum amount of tokens that can be spent
   * by this allowance and will be updated as tokens are spent. If it is
   * empty, there is no spend limit and any amount of coins can be spent.
   */
  spend_limit: Coin[];
  /** expiration specifies an optional time when this allowance expires */
  expiration: Date | undefined;
}

/**
 * PeriodicAllowance extends Allowance to allow for both a maximum cap,
 * as well as a limit per time period.
 */
export interface PeriodicAllowance {
  /** basic specifies a struct of `BasicAllowance` */
  basic: BasicAllowance | undefined;
  /**
   * period specifies the time duration in which period_spend_limit coins can
   * be spent before that allowance is reset
   */
  period: Duration | undefined;
  /**
   * period_spend_limit specifies the maximum number of coins that can be spent
   * in the period
   */
  period_spend_limit: Coin[];
  /** period_can_spend is the number of coins left to be spent before the period_reset time */
  period_can_spend: Coin[];
  /**
   * period_reset is the time at which this period resets and a new one begins,
   * it is calculated from the start time of the first transaction after the
   * last period ended
   */
  period_reset: Date | undefined;
}

/** AllowedMsgAllowance creates allowance only for specified message types. */
export interface AllowedMsgAllowance {
  /** allowance can be any of basic and filtered fee allowance. */
  allowance: Any | undefined;
  /** allowed_messages are the messages for which the grantee has the access. */
  allowed_messages: string[];
}

/** Grant is stored in the KVStore to record a grant with full context */
export interface Grant {
  /** granter is the address of the user granting an allowance of their funds. */
  granter: string;
  /** grantee is the address of the user being granted an allowance of another user's funds. */
  grantee: string;
  /** allowance can be any of basic and filtered fee allowance. */
  allowance: Any | undefined;
}

const baseBasicAllowance: object = {};

export const BasicAllowance = {
  encode(message: BasicAllowance, writer: Writer = Writer.create()): Writer {
    for (const v of message.spend_limit) {
      Coin.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.expiration !== undefined) {
      Timestamp.encode(
        toTimestamp(message.expiration),
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): BasicAllowance {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseBasicAllowance } as BasicAllowance;
    message.spend_limit = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.spend_limit.push(Coin.decode(reader, reader.uint32()));
          break;
        case 2:
          message.expiration = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): BasicAllowance {
    const message = { ...baseBasicAllowance } as BasicAllowance;
    message.spend_limit = [];
    if (object.spend_limit !== undefined && object.spend_limit !== null) {
      for (const e of object.spend_limit) {
        message.spend_limit.push(Coin.fromJSON(e));
      }
    }
    if (object.expiration !== undefined && object.expiration !== null) {
      message.expiration = fromJsonTimestamp(object.expiration);
    } else {
      message.expiration = undefined;
    }
    return message;
  },

  toJSON(message: BasicAllowance): unknown {
    const obj: any = {};
    if (message.spend_limit) {
      obj.spend_limit = message.spend_limit.map((e) =>
        e ? Coin.toJSON(e) : undefined
      );
    } else {
      obj.spend_limit = [];
    }
    message.expiration !== undefined &&
      (obj.expiration =
        message.expiration !== undefined
          ? message.expiration.toISOString()
          : null);
    return obj;
  },

  fromPartial(object: DeepPartial<BasicAllowance>): BasicAllowance {
    const message = { ...baseBasicAllowance } as BasicAllowance;
    message.spend_limit = [];
    if (object.spend_limit !== undefined && object.spend_limit !== null) {
      for (const e of object.spend_limit) {
        message.spend_limit.push(Coin.fromPartial(e));
      }
    }
    if (object.expiration !== undefined && object.expiration !== null) {
      message.expiration = object.expiration;
    } else {
      message.expiration = undefined;
    }
    return message;
  },
};

const basePeriodicAllowance: object = {};

export const PeriodicAllowance = {
  encode(message: PeriodicAllowance, writer: Writer = Writer.create()): Writer {
    if (message.basic !== undefined) {
      BasicAllowance.encode(message.basic, writer.uint32(10).fork()).ldelim();
    }
    if (message.period !== undefined) {
      Duration.encode(message.period, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.period_spend_limit) {
      Coin.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    for (const v of message.period_can_spend) {
      Coin.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    if (message.period_reset !== undefined) {
      Timestamp.encode(
        toTimestamp(message.period_reset),
        writer.uint32(42).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): PeriodicAllowance {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePeriodicAllowance } as PeriodicAllowance;
    message.period_spend_limit = [];
    message.period_can_spend = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.basic = BasicAllowance.decode(reader, reader.uint32());
          break;
        case 2:
          message.period = Duration.decode(reader, reader.uint32());
          break;
        case 3:
          message.period_spend_limit.push(Coin.decode(reader, reader.uint32()));
          break;
        case 4:
          message.period_can_spend.push(Coin.decode(reader, reader.uint32()));
          break;
        case 5:
          message.period_reset = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): PeriodicAllowance {
    const message = { ...basePeriodicAllowance } as PeriodicAllowance;
    message.period_spend_limit = [];
    message.period_can_spend = [];
    if (object.basic !== undefined && object.basic !== null) {
      message.basic = BasicAllowance.fromJSON(object.basic);
    } else {
      message.basic = undefined;
    }
    if (object.period !== undefined && object.period !== null) {
      message.period = Duration.fromJSON(object.period);
    } else {
      message.period = undefined;
    }
    if (
      object.period_spend_limit !== undefined &&
      object.period_spend_limit !== null
    ) {
      for (const e of object.period_spend_limit) {
        message.period_spend_limit.push(Coin.fromJSON(e));
      }
    }
    if (
      object.period_can_spend !== undefined &&
      object.period_can_spend !== null
    ) {
      for (const e of object.period_can_spend) {
        message.period_can_spend.push(Coin.fromJSON(e));
      }
    }
    if (object.period_reset !== undefined && object.period_reset !== null) {
      message.period_reset = fromJsonTimestamp(object.period_reset);
    } else {
      message.period_reset = undefined;
    }
    return message;
  },

  toJSON(message: PeriodicAllowance): unknown {
    const obj: any = {};
    message.basic !== undefined &&
      (obj.basic = message.basic
        ? BasicAllowance.toJSON(message.basic)
        : undefined);
    message.period !== undefined &&
      (obj.period = message.period
        ? Duration.toJSON(message.period)
        : undefined);
    if (message.period_spend_limit) {
      obj.period_spend_limit = message.period_spend_limit.map((e) =>
        e ? Coin.toJSON(e) : undefined
      );
    } else {
      obj.period_spend_limit = [];
    }
    if (message.period_can_spend) {
      obj.period_can_spend = message.period_can_spend.map((e) =>
        e ? Coin.toJSON(e) : undefined
      );
    } else {
      obj.period_can_spend = [];
    }
    message.period_reset !== undefined &&
      (obj.period_reset =
        message.period_reset !== undefined
          ? message.period_reset.toISOString()
          : null);
    return obj;
  },

  fromPartial(object: DeepPartial<PeriodicAllowance>): PeriodicAllowance {
    const message = { ...basePeriodicAllowance } as PeriodicAllowance;
    message.period_spend_limit = [];
    message.period_can_spend = [];
    if (object.basic !== undefined && object.basic !== null) {
      message.basic = BasicAllowance.fromPartial(object.basic);
    } else {
      message.basic = undefined;
    }
    if (object.period !== undefined && object.period !== null) {
      message.period = Duration.fromPartial(object.period);
    } else {
      message.period = undefined;
    }
    if (
      object.period_spend_limit !== undefined &&
      object.period_spend_limit !== null
    ) {
      for (const e of object.period_spend_limit) {
        message.period_spend_limit.push(Coin.fromPartial(e));
      }
    }
    if (
      object.period_can_spend !== undefined &&
      object.period_can_spend !== null
    ) {
      for (const e of object.period_can_spend) {
        message.period_can_spend.push(Coin.fromPartial(e));
      }
    }
    if (object.period_reset !== undefined && object.period_reset !== null) {
      message.period_reset = object.period_reset;
    } else {
      message.period_reset = undefined;
    }
    return message;
  },
};

const baseAllowedMsgAllowance: object = { allowed_messages: "" };

export const AllowedMsgAllowance = {
  encode(
    message: AllowedMsgAllowance,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.allowance !== undefined) {
      Any.encode(message.allowance, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.allowed_messages) {
      writer.uint32(18).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): AllowedMsgAllowance {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseAllowedMsgAllowance } as AllowedMsgAllowance;
    message.allowed_messages = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.allowance = Any.decode(reader, reader.uint32());
          break;
        case 2:
          message.allowed_messages.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): AllowedMsgAllowance {
    const message = { ...baseAllowedMsgAllowance } as AllowedMsgAllowance;
    message.allowed_messages = [];
    if (object.allowance !== undefined && object.allowance !== null) {
      message.allowance = Any.fromJSON(object.allowance);
    } else {
      message.allowance = undefined;
    }
    if (
      object.allowed_messages !== undefined &&
      object.allowed_messages !== null
    ) {
      for (const e of object.allowed_messages) {
        message.allowed_messages.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: AllowedMsgAllowance): unknown {
    const obj: any = {};
    message.allowance !== undefined &&
      (obj.allowance = message.allowance
        ? Any.toJSON(message.allowance)
        : undefined);
    if (message.allowed_messages) {
      obj.allowed_messages = message.allowed_messages.map((e) => e);
    } else {
      obj.allowed_messages = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<AllowedMsgAllowance>): AllowedMsgAllowance {
    const message = { ...baseAllowedMsgAllowance } as AllowedMsgAllowance;
    message.allowed_messages = [];
    if (object.allowance !== undefined && object.allowance !== null) {
      message.allowance = Any.fromPartial(object.allowance);
    } else {
      message.allowance = undefined;
    }
    if (
      object.allowed_messages !== undefined &&
      object.allowed_messages !== null
    ) {
      for (const e of object.allowed_messages) {
        message.allowed_messages.push(e);
      }
    }
    return message;
  },
};

const baseGrant: object = { granter: "", grantee: "" };

export const Grant = {
  encode(message: Grant, writer: Writer = Writer.create()): Writer {
    if (message.granter !== "") {
      writer.uint32(10).string(message.granter);
    }
    if (message.grantee !== "") {
      writer.uint32(18).string(message.grantee);
    }
    if (message.allowance !== undefined) {
      Any.encode(message.allowance, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Grant {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGrant } as Grant;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.granter = reader.string();
          break;
        case 2:
          message.grantee = reader.string();
          break;
        case 3:
          message.allowance = Any.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Grant {
    const message = { ...baseGrant } as Grant;
    if (object.granter !== undefined && object.granter !== null) {
      message.granter = String(object.granter);
    } else {
      message.granter = "";
    }
    if (object.grantee !== undefined && object.grantee !== null) {
      message.grantee = String(object.grantee);
    } else {
      message.grantee = "";
    }
    if (object.allowance !== undefined && object.allowance !== null) {
      message.allowance = Any.fromJSON(object.allowance);
    } else {
      message.allowance = undefined;
    }
    return message;
  },

  toJSON(message: Grant): unknown {
    const obj: any = {};
    message.granter !== undefined && (obj.granter = message.granter);
    message.grantee !== undefined && (obj.grantee = message.grantee);
    message.allowance !== undefined &&
      (obj.allowance = message.allowance
        ? Any.toJSON(message.allowance)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<Grant>): Grant {
    const message = { ...baseGrant } as Grant;
    if (object.granter !== undefined && object.granter !== null) {
      message.granter = object.granter;
    } else {
      message.granter = "";
    }
    if (object.grantee !== undefined && object.grantee !== null) {
      message.grantee = object.grantee;
    } else {
      message.grantee = "";
    }
    if (object.allowance !== undefined && object.allowance !== null) {
      message.allowance = Any.fromPartial(object.allowance);
    } else {
      message.allowance = undefined;
    }
    return message;
  },
};

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
