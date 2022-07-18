/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.oracle";

export interface Params {
  vote_period: number;
  vote_threshold: string;
  reward_band: string;
  whitelist: Denom[];
  slash_fraction: string;
  slash_window: number;
  min_valid_per_window: string;
  lookback_duration: number;
}

export interface Denom {
  name: string;
}

export interface AggregateExchangeRatePrevote {
  hash: string;
  voter: string;
  submit_block: number;
}

export interface AggregateExchangeRateVote {
  exchange_rate_tuples: ExchangeRateTuple[];
  voter: string;
}

export interface ExchangeRateTuple {
  denom: string;
  exchange_rate: string;
}

export interface OracleExchangeRate {
  exchange_rate: string;
  last_update: string;
}

export interface PriceSnapshotItem {
  denom: string;
  oracle_exchange_rate: OracleExchangeRate | undefined;
}

export interface PriceSnapshot {
  snapshotTimestamp: number;
  price_snapshot_items: PriceSnapshotItem[];
}

export interface OracleTwap {
  denom: string;
  twap: string;
  lookback_seconds: number;
}

export interface VotePenaltyCounter {
  miss_count: number;
  abstain_count: number;
}

const baseParams: object = {
  vote_period: 0,
  vote_threshold: "",
  reward_band: "",
  slash_fraction: "",
  slash_window: 0,
  min_valid_per_window: "",
  lookback_duration: 0,
};

export const Params = {
  encode(message: Params, writer: Writer = Writer.create()): Writer {
    if (message.vote_period !== 0) {
      writer.uint32(8).uint64(message.vote_period);
    }
    if (message.vote_threshold !== "") {
      writer.uint32(18).string(message.vote_threshold);
    }
    if (message.reward_band !== "") {
      writer.uint32(26).string(message.reward_band);
    }
    for (const v of message.whitelist) {
      Denom.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    if (message.slash_fraction !== "") {
      writer.uint32(42).string(message.slash_fraction);
    }
    if (message.slash_window !== 0) {
      writer.uint32(48).uint64(message.slash_window);
    }
    if (message.min_valid_per_window !== "") {
      writer.uint32(58).string(message.min_valid_per_window);
    }
    if (message.lookback_duration !== 0) {
      writer.uint32(72).int64(message.lookback_duration);
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
          message.vote_period = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.vote_threshold = reader.string();
          break;
        case 3:
          message.reward_band = reader.string();
          break;
        case 4:
          message.whitelist.push(Denom.decode(reader, reader.uint32()));
          break;
        case 5:
          message.slash_fraction = reader.string();
          break;
        case 6:
          message.slash_window = longToNumber(reader.uint64() as Long);
          break;
        case 7:
          message.min_valid_per_window = reader.string();
          break;
        case 9:
          message.lookback_duration = longToNumber(reader.int64() as Long);
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
    if (object.vote_period !== undefined && object.vote_period !== null) {
      message.vote_period = Number(object.vote_period);
    } else {
      message.vote_period = 0;
    }
    if (object.vote_threshold !== undefined && object.vote_threshold !== null) {
      message.vote_threshold = String(object.vote_threshold);
    } else {
      message.vote_threshold = "";
    }
    if (object.reward_band !== undefined && object.reward_band !== null) {
      message.reward_band = String(object.reward_band);
    } else {
      message.reward_band = "";
    }
    if (object.whitelist !== undefined && object.whitelist !== null) {
      for (const e of object.whitelist) {
        message.whitelist.push(Denom.fromJSON(e));
      }
    }
    if (object.slash_fraction !== undefined && object.slash_fraction !== null) {
      message.slash_fraction = String(object.slash_fraction);
    } else {
      message.slash_fraction = "";
    }
    if (object.slash_window !== undefined && object.slash_window !== null) {
      message.slash_window = Number(object.slash_window);
    } else {
      message.slash_window = 0;
    }
    if (
      object.min_valid_per_window !== undefined &&
      object.min_valid_per_window !== null
    ) {
      message.min_valid_per_window = String(object.min_valid_per_window);
    } else {
      message.min_valid_per_window = "";
    }
    if (
      object.lookback_duration !== undefined &&
      object.lookback_duration !== null
    ) {
      message.lookback_duration = Number(object.lookback_duration);
    } else {
      message.lookback_duration = 0;
    }
    return message;
  },

  toJSON(message: Params): unknown {
    const obj: any = {};
    message.vote_period !== undefined &&
      (obj.vote_period = message.vote_period);
    message.vote_threshold !== undefined &&
      (obj.vote_threshold = message.vote_threshold);
    message.reward_band !== undefined &&
      (obj.reward_band = message.reward_band);
    if (message.whitelist) {
      obj.whitelist = message.whitelist.map((e) =>
        e ? Denom.toJSON(e) : undefined
      );
    } else {
      obj.whitelist = [];
    }
    message.slash_fraction !== undefined &&
      (obj.slash_fraction = message.slash_fraction);
    message.slash_window !== undefined &&
      (obj.slash_window = message.slash_window);
    message.min_valid_per_window !== undefined &&
      (obj.min_valid_per_window = message.min_valid_per_window);
    message.lookback_duration !== undefined &&
      (obj.lookback_duration = message.lookback_duration);
    return obj;
  },

  fromPartial(object: DeepPartial<Params>): Params {
    const message = { ...baseParams } as Params;
    message.whitelist = [];
    if (object.vote_period !== undefined && object.vote_period !== null) {
      message.vote_period = object.vote_period;
    } else {
      message.vote_period = 0;
    }
    if (object.vote_threshold !== undefined && object.vote_threshold !== null) {
      message.vote_threshold = object.vote_threshold;
    } else {
      message.vote_threshold = "";
    }
    if (object.reward_band !== undefined && object.reward_band !== null) {
      message.reward_band = object.reward_band;
    } else {
      message.reward_band = "";
    }
    if (object.whitelist !== undefined && object.whitelist !== null) {
      for (const e of object.whitelist) {
        message.whitelist.push(Denom.fromPartial(e));
      }
    }
    if (object.slash_fraction !== undefined && object.slash_fraction !== null) {
      message.slash_fraction = object.slash_fraction;
    } else {
      message.slash_fraction = "";
    }
    if (object.slash_window !== undefined && object.slash_window !== null) {
      message.slash_window = object.slash_window;
    } else {
      message.slash_window = 0;
    }
    if (
      object.min_valid_per_window !== undefined &&
      object.min_valid_per_window !== null
    ) {
      message.min_valid_per_window = object.min_valid_per_window;
    } else {
      message.min_valid_per_window = "";
    }
    if (
      object.lookback_duration !== undefined &&
      object.lookback_duration !== null
    ) {
      message.lookback_duration = object.lookback_duration;
    } else {
      message.lookback_duration = 0;
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

const baseAggregateExchangeRatePrevote: object = {
  hash: "",
  voter: "",
  submit_block: 0,
};

export const AggregateExchangeRatePrevote = {
  encode(
    message: AggregateExchangeRatePrevote,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.hash !== "") {
      writer.uint32(10).string(message.hash);
    }
    if (message.voter !== "") {
      writer.uint32(18).string(message.voter);
    }
    if (message.submit_block !== 0) {
      writer.uint32(24).uint64(message.submit_block);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): AggregateExchangeRatePrevote {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseAggregateExchangeRatePrevote,
    } as AggregateExchangeRatePrevote;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.hash = reader.string();
          break;
        case 2:
          message.voter = reader.string();
          break;
        case 3:
          message.submit_block = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): AggregateExchangeRatePrevote {
    const message = {
      ...baseAggregateExchangeRatePrevote,
    } as AggregateExchangeRatePrevote;
    if (object.hash !== undefined && object.hash !== null) {
      message.hash = String(object.hash);
    } else {
      message.hash = "";
    }
    if (object.voter !== undefined && object.voter !== null) {
      message.voter = String(object.voter);
    } else {
      message.voter = "";
    }
    if (object.submit_block !== undefined && object.submit_block !== null) {
      message.submit_block = Number(object.submit_block);
    } else {
      message.submit_block = 0;
    }
    return message;
  },

  toJSON(message: AggregateExchangeRatePrevote): unknown {
    const obj: any = {};
    message.hash !== undefined && (obj.hash = message.hash);
    message.voter !== undefined && (obj.voter = message.voter);
    message.submit_block !== undefined &&
      (obj.submit_block = message.submit_block);
    return obj;
  },

  fromPartial(
    object: DeepPartial<AggregateExchangeRatePrevote>
  ): AggregateExchangeRatePrevote {
    const message = {
      ...baseAggregateExchangeRatePrevote,
    } as AggregateExchangeRatePrevote;
    if (object.hash !== undefined && object.hash !== null) {
      message.hash = object.hash;
    } else {
      message.hash = "";
    }
    if (object.voter !== undefined && object.voter !== null) {
      message.voter = object.voter;
    } else {
      message.voter = "";
    }
    if (object.submit_block !== undefined && object.submit_block !== null) {
      message.submit_block = object.submit_block;
    } else {
      message.submit_block = 0;
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
    for (const v of message.exchange_rate_tuples) {
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
    message.exchange_rate_tuples = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.exchange_rate_tuples.push(
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
    message.exchange_rate_tuples = [];
    if (
      object.exchange_rate_tuples !== undefined &&
      object.exchange_rate_tuples !== null
    ) {
      for (const e of object.exchange_rate_tuples) {
        message.exchange_rate_tuples.push(ExchangeRateTuple.fromJSON(e));
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
    if (message.exchange_rate_tuples) {
      obj.exchange_rate_tuples = message.exchange_rate_tuples.map((e) =>
        e ? ExchangeRateTuple.toJSON(e) : undefined
      );
    } else {
      obj.exchange_rate_tuples = [];
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
    message.exchange_rate_tuples = [];
    if (
      object.exchange_rate_tuples !== undefined &&
      object.exchange_rate_tuples !== null
    ) {
      for (const e of object.exchange_rate_tuples) {
        message.exchange_rate_tuples.push(ExchangeRateTuple.fromPartial(e));
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

const baseExchangeRateTuple: object = { denom: "", exchange_rate: "" };

export const ExchangeRateTuple = {
  encode(message: ExchangeRateTuple, writer: Writer = Writer.create()): Writer {
    if (message.denom !== "") {
      writer.uint32(10).string(message.denom);
    }
    if (message.exchange_rate !== "") {
      writer.uint32(18).string(message.exchange_rate);
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
          message.exchange_rate = reader.string();
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
    if (object.exchange_rate !== undefined && object.exchange_rate !== null) {
      message.exchange_rate = String(object.exchange_rate);
    } else {
      message.exchange_rate = "";
    }
    return message;
  },

  toJSON(message: ExchangeRateTuple): unknown {
    const obj: any = {};
    message.denom !== undefined && (obj.denom = message.denom);
    message.exchange_rate !== undefined &&
      (obj.exchange_rate = message.exchange_rate);
    return obj;
  },

  fromPartial(object: DeepPartial<ExchangeRateTuple>): ExchangeRateTuple {
    const message = { ...baseExchangeRateTuple } as ExchangeRateTuple;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = object.denom;
    } else {
      message.denom = "";
    }
    if (object.exchange_rate !== undefined && object.exchange_rate !== null) {
      message.exchange_rate = object.exchange_rate;
    } else {
      message.exchange_rate = "";
    }
    return message;
  },
};

const baseOracleExchangeRate: object = { exchange_rate: "", last_update: "" };

export const OracleExchangeRate = {
  encode(
    message: OracleExchangeRate,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.exchange_rate !== "") {
      writer.uint32(10).string(message.exchange_rate);
    }
    if (message.last_update !== "") {
      writer.uint32(18).string(message.last_update);
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
          message.exchange_rate = reader.string();
          break;
        case 2:
          message.last_update = reader.string();
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
    if (object.exchange_rate !== undefined && object.exchange_rate !== null) {
      message.exchange_rate = String(object.exchange_rate);
    } else {
      message.exchange_rate = "";
    }
    if (object.last_update !== undefined && object.last_update !== null) {
      message.last_update = String(object.last_update);
    } else {
      message.last_update = "";
    }
    return message;
  },

  toJSON(message: OracleExchangeRate): unknown {
    const obj: any = {};
    message.exchange_rate !== undefined &&
      (obj.exchange_rate = message.exchange_rate);
    message.last_update !== undefined &&
      (obj.last_update = message.last_update);
    return obj;
  },

  fromPartial(object: DeepPartial<OracleExchangeRate>): OracleExchangeRate {
    const message = { ...baseOracleExchangeRate } as OracleExchangeRate;
    if (object.exchange_rate !== undefined && object.exchange_rate !== null) {
      message.exchange_rate = object.exchange_rate;
    } else {
      message.exchange_rate = "";
    }
    if (object.last_update !== undefined && object.last_update !== null) {
      message.last_update = object.last_update;
    } else {
      message.last_update = "";
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
    if (message.oracle_exchange_rate !== undefined) {
      OracleExchangeRate.encode(
        message.oracle_exchange_rate,
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
          message.oracle_exchange_rate = OracleExchangeRate.decode(
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
      object.oracle_exchange_rate !== undefined &&
      object.oracle_exchange_rate !== null
    ) {
      message.oracle_exchange_rate = OracleExchangeRate.fromJSON(
        object.oracle_exchange_rate
      );
    } else {
      message.oracle_exchange_rate = undefined;
    }
    return message;
  },

  toJSON(message: PriceSnapshotItem): unknown {
    const obj: any = {};
    message.denom !== undefined && (obj.denom = message.denom);
    message.oracle_exchange_rate !== undefined &&
      (obj.oracle_exchange_rate = message.oracle_exchange_rate
        ? OracleExchangeRate.toJSON(message.oracle_exchange_rate)
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
      object.oracle_exchange_rate !== undefined &&
      object.oracle_exchange_rate !== null
    ) {
      message.oracle_exchange_rate = OracleExchangeRate.fromPartial(
        object.oracle_exchange_rate
      );
    } else {
      message.oracle_exchange_rate = undefined;
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
    for (const v of message.price_snapshot_items) {
      PriceSnapshotItem.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): PriceSnapshot {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePriceSnapshot } as PriceSnapshot;
    message.price_snapshot_items = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.snapshotTimestamp = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.price_snapshot_items.push(
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
    message.price_snapshot_items = [];
    if (
      object.snapshotTimestamp !== undefined &&
      object.snapshotTimestamp !== null
    ) {
      message.snapshotTimestamp = Number(object.snapshotTimestamp);
    } else {
      message.snapshotTimestamp = 0;
    }
    if (
      object.price_snapshot_items !== undefined &&
      object.price_snapshot_items !== null
    ) {
      for (const e of object.price_snapshot_items) {
        message.price_snapshot_items.push(PriceSnapshotItem.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: PriceSnapshot): unknown {
    const obj: any = {};
    message.snapshotTimestamp !== undefined &&
      (obj.snapshotTimestamp = message.snapshotTimestamp);
    if (message.price_snapshot_items) {
      obj.price_snapshot_items = message.price_snapshot_items.map((e) =>
        e ? PriceSnapshotItem.toJSON(e) : undefined
      );
    } else {
      obj.price_snapshot_items = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<PriceSnapshot>): PriceSnapshot {
    const message = { ...basePriceSnapshot } as PriceSnapshot;
    message.price_snapshot_items = [];
    if (
      object.snapshotTimestamp !== undefined &&
      object.snapshotTimestamp !== null
    ) {
      message.snapshotTimestamp = object.snapshotTimestamp;
    } else {
      message.snapshotTimestamp = 0;
    }
    if (
      object.price_snapshot_items !== undefined &&
      object.price_snapshot_items !== null
    ) {
      for (const e of object.price_snapshot_items) {
        message.price_snapshot_items.push(PriceSnapshotItem.fromPartial(e));
      }
    }
    return message;
  },
};

const baseOracleTwap: object = { denom: "", twap: "", lookback_seconds: 0 };

export const OracleTwap = {
  encode(message: OracleTwap, writer: Writer = Writer.create()): Writer {
    if (message.denom !== "") {
      writer.uint32(10).string(message.denom);
    }
    if (message.twap !== "") {
      writer.uint32(18).string(message.twap);
    }
    if (message.lookback_seconds !== 0) {
      writer.uint32(24).int64(message.lookback_seconds);
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
          message.lookback_seconds = longToNumber(reader.int64() as Long);
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
      object.lookback_seconds !== undefined &&
      object.lookback_seconds !== null
    ) {
      message.lookback_seconds = Number(object.lookback_seconds);
    } else {
      message.lookback_seconds = 0;
    }
    return message;
  },

  toJSON(message: OracleTwap): unknown {
    const obj: any = {};
    message.denom !== undefined && (obj.denom = message.denom);
    message.twap !== undefined && (obj.twap = message.twap);
    message.lookback_seconds !== undefined &&
      (obj.lookback_seconds = message.lookback_seconds);
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
      object.lookback_seconds !== undefined &&
      object.lookback_seconds !== null
    ) {
      message.lookback_seconds = object.lookback_seconds;
    } else {
      message.lookback_seconds = 0;
    }
    return message;
  },
};

const baseVotePenaltyCounter: object = { miss_count: 0, abstain_count: 0 };

export const VotePenaltyCounter = {
  encode(
    message: VotePenaltyCounter,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.miss_count !== 0) {
      writer.uint32(8).uint64(message.miss_count);
    }
    if (message.abstain_count !== 0) {
      writer.uint32(16).uint64(message.abstain_count);
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
          message.miss_count = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.abstain_count = longToNumber(reader.uint64() as Long);
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
    if (object.miss_count !== undefined && object.miss_count !== null) {
      message.miss_count = Number(object.miss_count);
    } else {
      message.miss_count = 0;
    }
    if (object.abstain_count !== undefined && object.abstain_count !== null) {
      message.abstain_count = Number(object.abstain_count);
    } else {
      message.abstain_count = 0;
    }
    return message;
  },

  toJSON(message: VotePenaltyCounter): unknown {
    const obj: any = {};
    message.miss_count !== undefined && (obj.miss_count = message.miss_count);
    message.abstain_count !== undefined &&
      (obj.abstain_count = message.abstain_count);
    return obj;
  },

  fromPartial(object: DeepPartial<VotePenaltyCounter>): VotePenaltyCounter {
    const message = { ...baseVotePenaltyCounter } as VotePenaltyCounter;
    if (object.miss_count !== undefined && object.miss_count !== null) {
      message.miss_count = object.miss_count;
    } else {
      message.miss_count = 0;
    }
    if (object.abstain_count !== undefined && object.abstain_count !== null) {
      message.abstain_count = object.abstain_count;
    } else {
      message.abstain_count = 0;
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
