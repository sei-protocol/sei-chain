/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "sei.oracle.v1";

export interface Params {
  /** The number of blocks per voting window, at the end of the vote period, the oracle votes are assessed and exchange rates are calculated. If the vote period is 1 this is equivalent to having oracle votes assessed and exchange rates calculated in each block. */
  votePeriod: number;
  voteThreshold: string;
  rewardBand: string;
  whitelist: Denom[];
  slashFraction: string;
  /** The interval in blocks at which the oracle module will assess validator penalty counters, and penalize validators with too poor performance. */
  slashWindow: number;
  /** The minimum percentage of voting windows for which a validator must have `success`es in order to not be penalized at the end of the slash window. */
  minValidPerWindow: string;
  lookbackDuration: number;
}

export interface Denom {
  name: string;
}

export interface AggregateExchangeRateVote {
  exchangeRateTuples: ExchangeRateTuple[];
  voter: string;
}

export interface ExchangeRateTuple {
  denom: string;
  exchangeRate: string;
}

export interface OracleExchangeRate {
  exchangeRate: string;
  lastUpdate: string;
  lastUpdateTimestamp: number;
}

export interface PriceSnapshotItem {
  denom: string;
  oracleExchangeRate: OracleExchangeRate | undefined;
}

export interface PriceSnapshot {
  snapshotTimestamp: number;
  priceSnapshotItems: PriceSnapshotItem[];
}

export interface OracleTwap {
  denom: string;
  twap: string;
  lookbackSeconds: number;
}

export interface VotePenaltyCounter {
  missCount: number;
  abstainCount: number;
  successCount: number;
}

const baseParams: object = {
  votePeriod: 0,
  voteThreshold: "",
  rewardBand: "",
  slashFraction: "",
  slashWindow: 0,
  minValidPerWindow: "",
  lookbackDuration: 0,
};

export const Params = {
  encode(message: Params, writer: Writer = Writer.create()): Writer {
    if (message.votePeriod !== 0) {
      writer.uint32(8).uint64(message.votePeriod);
    }
    if (message.voteThreshold !== "") {
      writer.uint32(18).string(message.voteThreshold);
    }
    if (message.rewardBand !== "") {
      writer.uint32(26).string(message.rewardBand);
    }
    for (const v of message.whitelist) {
      Denom.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    if (message.slashFraction !== "") {
      writer.uint32(42).string(message.slashFraction);
    }
    if (message.slashWindow !== 0) {
      writer.uint32(48).uint64(message.slashWindow);
    }
    if (message.minValidPerWindow !== "") {
      writer.uint32(58).string(message.minValidPerWindow);
    }
    if (message.lookbackDuration !== 0) {
      writer.uint32(72).uint64(message.lookbackDuration);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Params {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseParams } as Params;
    message.whitelist = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.votePeriod = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.voteThreshold = reader.string();
          break;
        case 3:
          message.rewardBand = reader.string();
          break;
        case 4:
          message.whitelist.push(Denom.decode(reader, reader.uint32()));
          break;
        case 5:
          message.slashFraction = reader.string();
          break;
        case 6:
          message.slashWindow = longToNumber(reader.uint64() as Long);
          break;
        case 7:
          message.minValidPerWindow = reader.string();
          break;
        case 9:
          message.lookbackDuration = longToNumber(reader.uint64() as Long);
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
    message.whitelist = [];
    if (object.votePeriod !== undefined && object.votePeriod !== null) {
      message.votePeriod = Number(object.votePeriod);
    } else {
      message.votePeriod = 0;
    }
    if (object.voteThreshold !== undefined && object.voteThreshold !== null) {
      message.voteThreshold = String(object.voteThreshold);
    } else {
      message.voteThreshold = "";
    }
    if (object.rewardBand !== undefined && object.rewardBand !== null) {
      message.rewardBand = String(object.rewardBand);
    } else {
      message.rewardBand = "";
    }
    if (object.whitelist !== undefined && object.whitelist !== null) {
      for (const e of object.whitelist) {
        message.whitelist.push(Denom.fromJSON(e));
      }
    }
    if (object.slashFraction !== undefined && object.slashFraction !== null) {
      message.slashFraction = String(object.slashFraction);
    } else {
      message.slashFraction = "";
    }
    if (object.slashWindow !== undefined && object.slashWindow !== null) {
      message.slashWindow = Number(object.slashWindow);
    } else {
      message.slashWindow = 0;
    }
    if (
      object.minValidPerWindow !== undefined &&
      object.minValidPerWindow !== null
    ) {
      message.minValidPerWindow = String(object.minValidPerWindow);
    } else {
      message.minValidPerWindow = "";
    }
    if (
      object.lookbackDuration !== undefined &&
      object.lookbackDuration !== null
    ) {
      message.lookbackDuration = Number(object.lookbackDuration);
    } else {
      message.lookbackDuration = 0;
    }
    return message;
  },

  toJSON(message: Params): unknown {
    const obj: any = {};
    message.votePeriod !== undefined && (obj.votePeriod = message.votePeriod);
    message.voteThreshold !== undefined &&
      (obj.voteThreshold = message.voteThreshold);
    message.rewardBand !== undefined && (obj.rewardBand = message.rewardBand);
    if (message.whitelist) {
      obj.whitelist = message.whitelist.map((e) =>
        e ? Denom.toJSON(e) : undefined
      );
    } else {
      obj.whitelist = [];
    }
    message.slashFraction !== undefined &&
      (obj.slashFraction = message.slashFraction);
    message.slashWindow !== undefined &&
      (obj.slashWindow = message.slashWindow);
    message.minValidPerWindow !== undefined &&
      (obj.minValidPerWindow = message.minValidPerWindow);
    message.lookbackDuration !== undefined &&
      (obj.lookbackDuration = message.lookbackDuration);
    return obj;
  },

  fromPartial(object: DeepPartial<Params>): Params {
    const message = { ...baseParams } as Params;
    message.whitelist = [];
    if (object.votePeriod !== undefined && object.votePeriod !== null) {
      message.votePeriod = object.votePeriod;
    } else {
      message.votePeriod = 0;
    }
    if (object.voteThreshold !== undefined && object.voteThreshold !== null) {
      message.voteThreshold = object.voteThreshold;
    } else {
      message.voteThreshold = "";
    }
    if (object.rewardBand !== undefined && object.rewardBand !== null) {
      message.rewardBand = object.rewardBand;
    } else {
      message.rewardBand = "";
    }
    if (object.whitelist !== undefined && object.whitelist !== null) {
      for (const e of object.whitelist) {
        message.whitelist.push(Denom.fromPartial(e));
      }
    }
    if (object.slashFraction !== undefined && object.slashFraction !== null) {
      message.slashFraction = object.slashFraction;
    } else {
      message.slashFraction = "";
    }
    if (object.slashWindow !== undefined && object.slashWindow !== null) {
      message.slashWindow = object.slashWindow;
    } else {
      message.slashWindow = 0;
    }
    if (
      object.minValidPerWindow !== undefined &&
      object.minValidPerWindow !== null
    ) {
      message.minValidPerWindow = object.minValidPerWindow;
    } else {
      message.minValidPerWindow = "";
    }
    if (
      object.lookbackDuration !== undefined &&
      object.lookbackDuration !== null
    ) {
      message.lookbackDuration = object.lookbackDuration;
    } else {
      message.lookbackDuration = 0;
    }
    return message;
  },
};

const baseDenom: object = { name: "" };

export const Denom = {
  encode(message: Denom, writer: Writer = Writer.create()): Writer {
    if (message.name !== "") {
      writer.uint32(10).string(message.name);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Denom {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDenom } as Denom;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.name = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Denom {
    const message = { ...baseDenom } as Denom;
    if (object.name !== undefined && object.name !== null) {
      message.name = String(object.name);
    } else {
      message.name = "";
    }
    return message;
  },

  toJSON(message: Denom): unknown {
    const obj: any = {};
    message.name !== undefined && (obj.name = message.name);
    return obj;
  },

  fromPartial(object: DeepPartial<Denom>): Denom {
    const message = { ...baseDenom } as Denom;
    if (object.name !== undefined && object.name !== null) {
      message.name = object.name;
    } else {
      message.name = "";
    }
    return message;
  },
};

const baseAggregateExchangeRateVote: object = { voter: "" };

export const AggregateExchangeRateVote = {
  encode(
    message: AggregateExchangeRateVote,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.exchangeRateTuples) {
      ExchangeRateTuple.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.voter !== "") {
      writer.uint32(18).string(message.voter);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): AggregateExchangeRateVote {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseAggregateExchangeRateVote,
    } as AggregateExchangeRateVote;
    message.exchangeRateTuples = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.exchangeRateTuples.push(
            ExchangeRateTuple.decode(reader, reader.uint32())
          );
          break;
        case 2:
          message.voter = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): AggregateExchangeRateVote {
    const message = {
      ...baseAggregateExchangeRateVote,
    } as AggregateExchangeRateVote;
    message.exchangeRateTuples = [];
    if (
      object.exchangeRateTuples !== undefined &&
      object.exchangeRateTuples !== null
    ) {
      for (const e of object.exchangeRateTuples) {
        message.exchangeRateTuples.push(ExchangeRateTuple.fromJSON(e));
      }
    }
    if (object.voter !== undefined && object.voter !== null) {
      message.voter = String(object.voter);
    } else {
      message.voter = "";
    }
    return message;
  },

  toJSON(message: AggregateExchangeRateVote): unknown {
    const obj: any = {};
    if (message.exchangeRateTuples) {
      obj.exchangeRateTuples = message.exchangeRateTuples.map((e) =>
        e ? ExchangeRateTuple.toJSON(e) : undefined
      );
    } else {
      obj.exchangeRateTuples = [];
    }
    message.voter !== undefined && (obj.voter = message.voter);
    return obj;
  },

  fromPartial(
    object: DeepPartial<AggregateExchangeRateVote>
  ): AggregateExchangeRateVote {
    const message = {
      ...baseAggregateExchangeRateVote,
    } as AggregateExchangeRateVote;
    message.exchangeRateTuples = [];
    if (
      object.exchangeRateTuples !== undefined &&
      object.exchangeRateTuples !== null
    ) {
      for (const e of object.exchangeRateTuples) {
        message.exchangeRateTuples.push(ExchangeRateTuple.fromPartial(e));
      }
    }
    if (object.voter !== undefined && object.voter !== null) {
      message.voter = object.voter;
    } else {
      message.voter = "";
    }
    return message;
  },
};

const baseExchangeRateTuple: object = { denom: "", exchangeRate: "" };

export const ExchangeRateTuple = {
  encode(message: ExchangeRateTuple, writer: Writer = Writer.create()): Writer {
    if (message.denom !== "") {
      writer.uint32(10).string(message.denom);
    }
    if (message.exchangeRate !== "") {
      writer.uint32(18).string(message.exchangeRate);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ExchangeRateTuple {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseExchangeRateTuple } as ExchangeRateTuple;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.denom = reader.string();
          break;
        case 2:
          message.exchangeRate = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ExchangeRateTuple {
    const message = { ...baseExchangeRateTuple } as ExchangeRateTuple;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = String(object.denom);
    } else {
      message.denom = "";
    }
    if (object.exchangeRate !== undefined && object.exchangeRate !== null) {
      message.exchangeRate = String(object.exchangeRate);
    } else {
      message.exchangeRate = "";
    }
    return message;
  },

  toJSON(message: ExchangeRateTuple): unknown {
    const obj: any = {};
    message.denom !== undefined && (obj.denom = message.denom);
    message.exchangeRate !== undefined &&
      (obj.exchangeRate = message.exchangeRate);
    return obj;
  },

  fromPartial(object: DeepPartial<ExchangeRateTuple>): ExchangeRateTuple {
    const message = { ...baseExchangeRateTuple } as ExchangeRateTuple;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = object.denom;
    } else {
      message.denom = "";
    }
    if (object.exchangeRate !== undefined && object.exchangeRate !== null) {
      message.exchangeRate = object.exchangeRate;
    } else {
      message.exchangeRate = "";
    }
    return message;
  },
};

const baseOracleExchangeRate: object = {
  exchangeRate: "",
  lastUpdate: "",
  lastUpdateTimestamp: 0,
};

export const OracleExchangeRate = {
  encode(
    message: OracleExchangeRate,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.exchangeRate !== "") {
      writer.uint32(10).string(message.exchangeRate);
    }
    if (message.lastUpdate !== "") {
      writer.uint32(18).string(message.lastUpdate);
    }
    if (message.lastUpdateTimestamp !== 0) {
      writer.uint32(24).int64(message.lastUpdateTimestamp);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): OracleExchangeRate {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseOracleExchangeRate } as OracleExchangeRate;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.exchangeRate = reader.string();
          break;
        case 2:
          message.lastUpdate = reader.string();
          break;
        case 3:
          message.lastUpdateTimestamp = longToNumber(reader.int64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): OracleExchangeRate {
    const message = { ...baseOracleExchangeRate } as OracleExchangeRate;
    if (object.exchangeRate !== undefined && object.exchangeRate !== null) {
      message.exchangeRate = String(object.exchangeRate);
    } else {
      message.exchangeRate = "";
    }
    if (object.lastUpdate !== undefined && object.lastUpdate !== null) {
      message.lastUpdate = String(object.lastUpdate);
    } else {
      message.lastUpdate = "";
    }
    if (
      object.lastUpdateTimestamp !== undefined &&
      object.lastUpdateTimestamp !== null
    ) {
      message.lastUpdateTimestamp = Number(object.lastUpdateTimestamp);
    } else {
      message.lastUpdateTimestamp = 0;
    }
    return message;
  },

  toJSON(message: OracleExchangeRate): unknown {
    const obj: any = {};
    message.exchangeRate !== undefined &&
      (obj.exchangeRate = message.exchangeRate);
    message.lastUpdate !== undefined && (obj.lastUpdate = message.lastUpdate);
    message.lastUpdateTimestamp !== undefined &&
      (obj.lastUpdateTimestamp = message.lastUpdateTimestamp);
    return obj;
  },

  fromPartial(object: DeepPartial<OracleExchangeRate>): OracleExchangeRate {
    const message = { ...baseOracleExchangeRate } as OracleExchangeRate;
    if (object.exchangeRate !== undefined && object.exchangeRate !== null) {
      message.exchangeRate = object.exchangeRate;
    } else {
      message.exchangeRate = "";
    }
    if (object.lastUpdate !== undefined && object.lastUpdate !== null) {
      message.lastUpdate = object.lastUpdate;
    } else {
      message.lastUpdate = "";
    }
    if (
      object.lastUpdateTimestamp !== undefined &&
      object.lastUpdateTimestamp !== null
    ) {
      message.lastUpdateTimestamp = object.lastUpdateTimestamp;
    } else {
      message.lastUpdateTimestamp = 0;
    }
    return message;
  },
};

const basePriceSnapshotItem: object = { denom: "" };

export const PriceSnapshotItem = {
  encode(message: PriceSnapshotItem, writer: Writer = Writer.create()): Writer {
    if (message.denom !== "") {
      writer.uint32(10).string(message.denom);
    }
    if (message.oracleExchangeRate !== undefined) {
      OracleExchangeRate.encode(
        message.oracleExchangeRate,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): PriceSnapshotItem {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePriceSnapshotItem } as PriceSnapshotItem;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.denom = reader.string();
          break;
        case 2:
          message.oracleExchangeRate = OracleExchangeRate.decode(
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

  fromJSON(object: any): PriceSnapshotItem {
    const message = { ...basePriceSnapshotItem } as PriceSnapshotItem;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = String(object.denom);
    } else {
      message.denom = "";
    }
    if (
      object.oracleExchangeRate !== undefined &&
      object.oracleExchangeRate !== null
    ) {
      message.oracleExchangeRate = OracleExchangeRate.fromJSON(
        object.oracleExchangeRate
      );
    } else {
      message.oracleExchangeRate = undefined;
    }
    return message;
  },

  toJSON(message: PriceSnapshotItem): unknown {
    const obj: any = {};
    message.denom !== undefined && (obj.denom = message.denom);
    message.oracleExchangeRate !== undefined &&
      (obj.oracleExchangeRate = message.oracleExchangeRate
        ? OracleExchangeRate.toJSON(message.oracleExchangeRate)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<PriceSnapshotItem>): PriceSnapshotItem {
    const message = { ...basePriceSnapshotItem } as PriceSnapshotItem;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = object.denom;
    } else {
      message.denom = "";
    }
    if (
      object.oracleExchangeRate !== undefined &&
      object.oracleExchangeRate !== null
    ) {
      message.oracleExchangeRate = OracleExchangeRate.fromPartial(
        object.oracleExchangeRate
      );
    } else {
      message.oracleExchangeRate = undefined;
    }
    return message;
  },
};

const basePriceSnapshot: object = { snapshotTimestamp: 0 };

export const PriceSnapshot = {
  encode(message: PriceSnapshot, writer: Writer = Writer.create()): Writer {
    if (message.snapshotTimestamp !== 0) {
      writer.uint32(8).int64(message.snapshotTimestamp);
    }
    for (const v of message.priceSnapshotItems) {
      PriceSnapshotItem.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): PriceSnapshot {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePriceSnapshot } as PriceSnapshot;
    message.priceSnapshotItems = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.snapshotTimestamp = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.priceSnapshotItems.push(
            PriceSnapshotItem.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): PriceSnapshot {
    const message = { ...basePriceSnapshot } as PriceSnapshot;
    message.priceSnapshotItems = [];
    if (
      object.snapshotTimestamp !== undefined &&
      object.snapshotTimestamp !== null
    ) {
      message.snapshotTimestamp = Number(object.snapshotTimestamp);
    } else {
      message.snapshotTimestamp = 0;
    }
    if (
      object.priceSnapshotItems !== undefined &&
      object.priceSnapshotItems !== null
    ) {
      for (const e of object.priceSnapshotItems) {
        message.priceSnapshotItems.push(PriceSnapshotItem.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: PriceSnapshot): unknown {
    const obj: any = {};
    message.snapshotTimestamp !== undefined &&
      (obj.snapshotTimestamp = message.snapshotTimestamp);
    if (message.priceSnapshotItems) {
      obj.priceSnapshotItems = message.priceSnapshotItems.map((e) =>
        e ? PriceSnapshotItem.toJSON(e) : undefined
      );
    } else {
      obj.priceSnapshotItems = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<PriceSnapshot>): PriceSnapshot {
    const message = { ...basePriceSnapshot } as PriceSnapshot;
    message.priceSnapshotItems = [];
    if (
      object.snapshotTimestamp !== undefined &&
      object.snapshotTimestamp !== null
    ) {
      message.snapshotTimestamp = object.snapshotTimestamp;
    } else {
      message.snapshotTimestamp = 0;
    }
    if (
      object.priceSnapshotItems !== undefined &&
      object.priceSnapshotItems !== null
    ) {
      for (const e of object.priceSnapshotItems) {
        message.priceSnapshotItems.push(PriceSnapshotItem.fromPartial(e));
      }
    }
    return message;
  },
};

const baseOracleTwap: object = { denom: "", twap: "", lookbackSeconds: 0 };

export const OracleTwap = {
  encode(message: OracleTwap, writer: Writer = Writer.create()): Writer {
    if (message.denom !== "") {
      writer.uint32(10).string(message.denom);
    }
    if (message.twap !== "") {
      writer.uint32(18).string(message.twap);
    }
    if (message.lookbackSeconds !== 0) {
      writer.uint32(24).int64(message.lookbackSeconds);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): OracleTwap {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseOracleTwap } as OracleTwap;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.denom = reader.string();
          break;
        case 2:
          message.twap = reader.string();
          break;
        case 3:
          message.lookbackSeconds = longToNumber(reader.int64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): OracleTwap {
    const message = { ...baseOracleTwap } as OracleTwap;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = String(object.denom);
    } else {
      message.denom = "";
    }
    if (object.twap !== undefined && object.twap !== null) {
      message.twap = String(object.twap);
    } else {
      message.twap = "";
    }
    if (
      object.lookbackSeconds !== undefined &&
      object.lookbackSeconds !== null
    ) {
      message.lookbackSeconds = Number(object.lookbackSeconds);
    } else {
      message.lookbackSeconds = 0;
    }
    return message;
  },

  toJSON(message: OracleTwap): unknown {
    const obj: any = {};
    message.denom !== undefined && (obj.denom = message.denom);
    message.twap !== undefined && (obj.twap = message.twap);
    message.lookbackSeconds !== undefined &&
      (obj.lookbackSeconds = message.lookbackSeconds);
    return obj;
  },

  fromPartial(object: DeepPartial<OracleTwap>): OracleTwap {
    const message = { ...baseOracleTwap } as OracleTwap;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = object.denom;
    } else {
      message.denom = "";
    }
    if (object.twap !== undefined && object.twap !== null) {
      message.twap = object.twap;
    } else {
      message.twap = "";
    }
    if (
      object.lookbackSeconds !== undefined &&
      object.lookbackSeconds !== null
    ) {
      message.lookbackSeconds = object.lookbackSeconds;
    } else {
      message.lookbackSeconds = 0;
    }
    return message;
  },
};

const baseVotePenaltyCounter: object = {
  missCount: 0,
  abstainCount: 0,
  successCount: 0,
};

export const VotePenaltyCounter = {
  encode(
    message: VotePenaltyCounter,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.missCount !== 0) {
      writer.uint32(8).uint64(message.missCount);
    }
    if (message.abstainCount !== 0) {
      writer.uint32(16).uint64(message.abstainCount);
    }
    if (message.successCount !== 0) {
      writer.uint32(24).uint64(message.successCount);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): VotePenaltyCounter {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseVotePenaltyCounter } as VotePenaltyCounter;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.missCount = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.abstainCount = longToNumber(reader.uint64() as Long);
          break;
        case 3:
          message.successCount = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): VotePenaltyCounter {
    const message = { ...baseVotePenaltyCounter } as VotePenaltyCounter;
    if (object.missCount !== undefined && object.missCount !== null) {
      message.missCount = Number(object.missCount);
    } else {
      message.missCount = 0;
    }
    if (object.abstainCount !== undefined && object.abstainCount !== null) {
      message.abstainCount = Number(object.abstainCount);
    } else {
      message.abstainCount = 0;
    }
    if (object.successCount !== undefined && object.successCount !== null) {
      message.successCount = Number(object.successCount);
    } else {
      message.successCount = 0;
    }
    return message;
  },

  toJSON(message: VotePenaltyCounter): unknown {
    const obj: any = {};
    message.missCount !== undefined && (obj.missCount = message.missCount);
    message.abstainCount !== undefined &&
      (obj.abstainCount = message.abstainCount);
    message.successCount !== undefined &&
      (obj.successCount = message.successCount);
    return obj;
  },

  fromPartial(object: DeepPartial<VotePenaltyCounter>): VotePenaltyCounter {
    const message = { ...baseVotePenaltyCounter } as VotePenaltyCounter;
    if (object.missCount !== undefined && object.missCount !== null) {
      message.missCount = object.missCount;
    } else {
      message.missCount = 0;
    }
    if (object.abstainCount !== undefined && object.abstainCount !== null) {
      message.abstainCount = object.abstainCount;
    } else {
      message.abstainCount = 0;
    }
    if (object.successCount !== undefined && object.successCount !== null) {
      message.successCount = object.successCount;
    } else {
      message.successCount = 0;
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
