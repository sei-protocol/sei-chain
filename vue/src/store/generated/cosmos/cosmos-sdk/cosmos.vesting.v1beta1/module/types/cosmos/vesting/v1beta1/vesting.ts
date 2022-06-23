/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { BaseAccount } from "../../../cosmos/auth/v1beta1/auth";
import { Coin } from "../../../cosmos/base/v1beta1/coin";

export const protobufPackage = "cosmos.vesting.v1beta1";

/**
 * BaseVestingAccount implements the VestingAccount interface. It contains all
 * the necessary fields needed for any vesting account implementation.
 */
export interface BaseVestingAccount {
  baseAccount: BaseAccount | undefined;
  originalVesting: Coin[];
  delegatedFree: Coin[];
  delegatedVesting: Coin[];
  endTime: number;
}

/**
 * ContinuousVestingAccount implements the VestingAccount interface. It
 * continuously vests by unlocking coins linearly with respect to time.
 */
export interface ContinuousVestingAccount {
  baseVestingAccount: BaseVestingAccount | undefined;
  startTime: number;
}

/**
 * DelayedVestingAccount implements the VestingAccount interface. It vests all
 * coins after a specific time, but non prior. In other words, it keeps them
 * locked until a specified time.
 */
export interface DelayedVestingAccount {
  baseVestingAccount: BaseVestingAccount | undefined;
}

/** Period defines a length of time and amount of coins that will vest. */
export interface Period {
  length: number;
  amount: Coin[];
}

/**
 * PeriodicVestingAccount implements the VestingAccount interface. It
 * periodically vests by unlocking coins during each specified period.
 */
export interface PeriodicVestingAccount {
  baseVestingAccount: BaseVestingAccount | undefined;
  startTime: number;
  vestingPeriods: Period[];
}

/**
 * PermanentLockedAccount implements the VestingAccount interface. It does
 * not ever release coins, locking them indefinitely. Coins in this account can
 * still be used for delegating and for governance votes even while locked.
 *
 * Since: cosmos-sdk 0.43
 */
export interface PermanentLockedAccount {
  baseVestingAccount: BaseVestingAccount | undefined;
}

const baseBaseVestingAccount: object = { endTime: 0 };

export const BaseVestingAccount = {
  encode(
    message: BaseVestingAccount,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.baseAccount !== undefined) {
      BaseAccount.encode(
        message.baseAccount,
        writer.uint32(10).fork()
      ).ldelim();
    }
    for (const v of message.originalVesting) {
      Coin.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.delegatedFree) {
      Coin.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    for (const v of message.delegatedVesting) {
      Coin.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    if (message.endTime !== 0) {
      writer.uint32(40).int64(message.endTime);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): BaseVestingAccount {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseBaseVestingAccount } as BaseVestingAccount;
    message.originalVesting = [];
    message.delegatedFree = [];
    message.delegatedVesting = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.baseAccount = BaseAccount.decode(reader, reader.uint32());
          break;
        case 2:
          message.originalVesting.push(Coin.decode(reader, reader.uint32()));
          break;
        case 3:
          message.delegatedFree.push(Coin.decode(reader, reader.uint32()));
          break;
        case 4:
          message.delegatedVesting.push(Coin.decode(reader, reader.uint32()));
          break;
        case 5:
          message.endTime = longToNumber(reader.int64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): BaseVestingAccount {
    const message = { ...baseBaseVestingAccount } as BaseVestingAccount;
    message.originalVesting = [];
    message.delegatedFree = [];
    message.delegatedVesting = [];
    if (object.baseAccount !== undefined && object.baseAccount !== null) {
      message.baseAccount = BaseAccount.fromJSON(object.baseAccount);
    } else {
      message.baseAccount = undefined;
    }
    if (
      object.originalVesting !== undefined &&
      object.originalVesting !== null
    ) {
      for (const e of object.originalVesting) {
        message.originalVesting.push(Coin.fromJSON(e));
      }
    }
    if (object.delegatedFree !== undefined && object.delegatedFree !== null) {
      for (const e of object.delegatedFree) {
        message.delegatedFree.push(Coin.fromJSON(e));
      }
    }
    if (
      object.delegatedVesting !== undefined &&
      object.delegatedVesting !== null
    ) {
      for (const e of object.delegatedVesting) {
        message.delegatedVesting.push(Coin.fromJSON(e));
      }
    }
    if (object.endTime !== undefined && object.endTime !== null) {
      message.endTime = Number(object.endTime);
    } else {
      message.endTime = 0;
    }
    return message;
  },

  toJSON(message: BaseVestingAccount): unknown {
    const obj: any = {};
    message.baseAccount !== undefined &&
      (obj.baseAccount = message.baseAccount
        ? BaseAccount.toJSON(message.baseAccount)
        : undefined);
    if (message.originalVesting) {
      obj.originalVesting = message.originalVesting.map((e) =>
        e ? Coin.toJSON(e) : undefined
      );
    } else {
      obj.originalVesting = [];
    }
    if (message.delegatedFree) {
      obj.delegatedFree = message.delegatedFree.map((e) =>
        e ? Coin.toJSON(e) : undefined
      );
    } else {
      obj.delegatedFree = [];
    }
    if (message.delegatedVesting) {
      obj.delegatedVesting = message.delegatedVesting.map((e) =>
        e ? Coin.toJSON(e) : undefined
      );
    } else {
      obj.delegatedVesting = [];
    }
    message.endTime !== undefined && (obj.endTime = message.endTime);
    return obj;
  },

  fromPartial(object: DeepPartial<BaseVestingAccount>): BaseVestingAccount {
    const message = { ...baseBaseVestingAccount } as BaseVestingAccount;
    message.originalVesting = [];
    message.delegatedFree = [];
    message.delegatedVesting = [];
    if (object.baseAccount !== undefined && object.baseAccount !== null) {
      message.baseAccount = BaseAccount.fromPartial(object.baseAccount);
    } else {
      message.baseAccount = undefined;
    }
    if (
      object.originalVesting !== undefined &&
      object.originalVesting !== null
    ) {
      for (const e of object.originalVesting) {
        message.originalVesting.push(Coin.fromPartial(e));
      }
    }
    if (object.delegatedFree !== undefined && object.delegatedFree !== null) {
      for (const e of object.delegatedFree) {
        message.delegatedFree.push(Coin.fromPartial(e));
      }
    }
    if (
      object.delegatedVesting !== undefined &&
      object.delegatedVesting !== null
    ) {
      for (const e of object.delegatedVesting) {
        message.delegatedVesting.push(Coin.fromPartial(e));
      }
    }
    if (object.endTime !== undefined && object.endTime !== null) {
      message.endTime = object.endTime;
    } else {
      message.endTime = 0;
    }
    return message;
  },
};

const baseContinuousVestingAccount: object = { startTime: 0 };

export const ContinuousVestingAccount = {
  encode(
    message: ContinuousVestingAccount,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.baseVestingAccount !== undefined) {
      BaseVestingAccount.encode(
        message.baseVestingAccount,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.startTime !== 0) {
      writer.uint32(16).int64(message.startTime);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ContinuousVestingAccount {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseContinuousVestingAccount,
    } as ContinuousVestingAccount;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.baseVestingAccount = BaseVestingAccount.decode(
            reader,
            reader.uint32()
          );
          break;
        case 2:
          message.startTime = longToNumber(reader.int64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ContinuousVestingAccount {
    const message = {
      ...baseContinuousVestingAccount,
    } as ContinuousVestingAccount;
    if (
      object.baseVestingAccount !== undefined &&
      object.baseVestingAccount !== null
    ) {
      message.baseVestingAccount = BaseVestingAccount.fromJSON(
        object.baseVestingAccount
      );
    } else {
      message.baseVestingAccount = undefined;
    }
    if (object.startTime !== undefined && object.startTime !== null) {
      message.startTime = Number(object.startTime);
    } else {
      message.startTime = 0;
    }
    return message;
  },

  toJSON(message: ContinuousVestingAccount): unknown {
    const obj: any = {};
    message.baseVestingAccount !== undefined &&
      (obj.baseVestingAccount = message.baseVestingAccount
        ? BaseVestingAccount.toJSON(message.baseVestingAccount)
        : undefined);
    message.startTime !== undefined && (obj.startTime = message.startTime);
    return obj;
  },

  fromPartial(
    object: DeepPartial<ContinuousVestingAccount>
  ): ContinuousVestingAccount {
    const message = {
      ...baseContinuousVestingAccount,
    } as ContinuousVestingAccount;
    if (
      object.baseVestingAccount !== undefined &&
      object.baseVestingAccount !== null
    ) {
      message.baseVestingAccount = BaseVestingAccount.fromPartial(
        object.baseVestingAccount
      );
    } else {
      message.baseVestingAccount = undefined;
    }
    if (object.startTime !== undefined && object.startTime !== null) {
      message.startTime = object.startTime;
    } else {
      message.startTime = 0;
    }
    return message;
  },
};

const baseDelayedVestingAccount: object = {};

export const DelayedVestingAccount = {
  encode(
    message: DelayedVestingAccount,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.baseVestingAccount !== undefined) {
      BaseVestingAccount.encode(
        message.baseVestingAccount,
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): DelayedVestingAccount {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDelayedVestingAccount } as DelayedVestingAccount;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.baseVestingAccount = BaseVestingAccount.decode(
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

  fromJSON(object: any): DelayedVestingAccount {
    const message = { ...baseDelayedVestingAccount } as DelayedVestingAccount;
    if (
      object.baseVestingAccount !== undefined &&
      object.baseVestingAccount !== null
    ) {
      message.baseVestingAccount = BaseVestingAccount.fromJSON(
        object.baseVestingAccount
      );
    } else {
      message.baseVestingAccount = undefined;
    }
    return message;
  },

  toJSON(message: DelayedVestingAccount): unknown {
    const obj: any = {};
    message.baseVestingAccount !== undefined &&
      (obj.baseVestingAccount = message.baseVestingAccount
        ? BaseVestingAccount.toJSON(message.baseVestingAccount)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<DelayedVestingAccount>
  ): DelayedVestingAccount {
    const message = { ...baseDelayedVestingAccount } as DelayedVestingAccount;
    if (
      object.baseVestingAccount !== undefined &&
      object.baseVestingAccount !== null
    ) {
      message.baseVestingAccount = BaseVestingAccount.fromPartial(
        object.baseVestingAccount
      );
    } else {
      message.baseVestingAccount = undefined;
    }
    return message;
  },
};

const basePeriod: object = { length: 0 };

export const Period = {
  encode(message: Period, writer: Writer = Writer.create()): Writer {
    if (message.length !== 0) {
      writer.uint32(8).int64(message.length);
    }
    for (const v of message.amount) {
      Coin.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Period {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePeriod } as Period;
    message.amount = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.length = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.amount.push(Coin.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Period {
    const message = { ...basePeriod } as Period;
    message.amount = [];
    if (object.length !== undefined && object.length !== null) {
      message.length = Number(object.length);
    } else {
      message.length = 0;
    }
    if (object.amount !== undefined && object.amount !== null) {
      for (const e of object.amount) {
        message.amount.push(Coin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: Period): unknown {
    const obj: any = {};
    message.length !== undefined && (obj.length = message.length);
    if (message.amount) {
      obj.amount = message.amount.map((e) => (e ? Coin.toJSON(e) : undefined));
    } else {
      obj.amount = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<Period>): Period {
    const message = { ...basePeriod } as Period;
    message.amount = [];
    if (object.length !== undefined && object.length !== null) {
      message.length = object.length;
    } else {
      message.length = 0;
    }
    if (object.amount !== undefined && object.amount !== null) {
      for (const e of object.amount) {
        message.amount.push(Coin.fromPartial(e));
      }
    }
    return message;
  },
};

const basePeriodicVestingAccount: object = { startTime: 0 };

export const PeriodicVestingAccount = {
  encode(
    message: PeriodicVestingAccount,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.baseVestingAccount !== undefined) {
      BaseVestingAccount.encode(
        message.baseVestingAccount,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.startTime !== 0) {
      writer.uint32(16).int64(message.startTime);
    }
    for (const v of message.vestingPeriods) {
      Period.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): PeriodicVestingAccount {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePeriodicVestingAccount } as PeriodicVestingAccount;
    message.vestingPeriods = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.baseVestingAccount = BaseVestingAccount.decode(
            reader,
            reader.uint32()
          );
          break;
        case 2:
          message.startTime = longToNumber(reader.int64() as Long);
          break;
        case 3:
          message.vestingPeriods.push(Period.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): PeriodicVestingAccount {
    const message = { ...basePeriodicVestingAccount } as PeriodicVestingAccount;
    message.vestingPeriods = [];
    if (
      object.baseVestingAccount !== undefined &&
      object.baseVestingAccount !== null
    ) {
      message.baseVestingAccount = BaseVestingAccount.fromJSON(
        object.baseVestingAccount
      );
    } else {
      message.baseVestingAccount = undefined;
    }
    if (object.startTime !== undefined && object.startTime !== null) {
      message.startTime = Number(object.startTime);
    } else {
      message.startTime = 0;
    }
    if (object.vestingPeriods !== undefined && object.vestingPeriods !== null) {
      for (const e of object.vestingPeriods) {
        message.vestingPeriods.push(Period.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: PeriodicVestingAccount): unknown {
    const obj: any = {};
    message.baseVestingAccount !== undefined &&
      (obj.baseVestingAccount = message.baseVestingAccount
        ? BaseVestingAccount.toJSON(message.baseVestingAccount)
        : undefined);
    message.startTime !== undefined && (obj.startTime = message.startTime);
    if (message.vestingPeriods) {
      obj.vestingPeriods = message.vestingPeriods.map((e) =>
        e ? Period.toJSON(e) : undefined
      );
    } else {
      obj.vestingPeriods = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<PeriodicVestingAccount>
  ): PeriodicVestingAccount {
    const message = { ...basePeriodicVestingAccount } as PeriodicVestingAccount;
    message.vestingPeriods = [];
    if (
      object.baseVestingAccount !== undefined &&
      object.baseVestingAccount !== null
    ) {
      message.baseVestingAccount = BaseVestingAccount.fromPartial(
        object.baseVestingAccount
      );
    } else {
      message.baseVestingAccount = undefined;
    }
    if (object.startTime !== undefined && object.startTime !== null) {
      message.startTime = object.startTime;
    } else {
      message.startTime = 0;
    }
    if (object.vestingPeriods !== undefined && object.vestingPeriods !== null) {
      for (const e of object.vestingPeriods) {
        message.vestingPeriods.push(Period.fromPartial(e));
      }
    }
    return message;
  },
};

const basePermanentLockedAccount: object = {};

export const PermanentLockedAccount = {
  encode(
    message: PermanentLockedAccount,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.baseVestingAccount !== undefined) {
      BaseVestingAccount.encode(
        message.baseVestingAccount,
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): PermanentLockedAccount {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePermanentLockedAccount } as PermanentLockedAccount;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.baseVestingAccount = BaseVestingAccount.decode(
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

  fromJSON(object: any): PermanentLockedAccount {
    const message = { ...basePermanentLockedAccount } as PermanentLockedAccount;
    if (
      object.baseVestingAccount !== undefined &&
      object.baseVestingAccount !== null
    ) {
      message.baseVestingAccount = BaseVestingAccount.fromJSON(
        object.baseVestingAccount
      );
    } else {
      message.baseVestingAccount = undefined;
    }
    return message;
  },

  toJSON(message: PermanentLockedAccount): unknown {
    const obj: any = {};
    message.baseVestingAccount !== undefined &&
      (obj.baseVestingAccount = message.baseVestingAccount
        ? BaseVestingAccount.toJSON(message.baseVestingAccount)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<PermanentLockedAccount>
  ): PermanentLockedAccount {
    const message = { ...basePermanentLockedAccount } as PermanentLockedAccount;
    if (
      object.baseVestingAccount !== undefined &&
      object.baseVestingAccount !== null
    ) {
      message.baseVestingAccount = BaseVestingAccount.fromPartial(
        object.baseVestingAccount
      );
    } else {
      message.baseVestingAccount = undefined;
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
