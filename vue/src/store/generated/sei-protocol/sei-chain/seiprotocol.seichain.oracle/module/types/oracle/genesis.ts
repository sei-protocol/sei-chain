/* eslint-disable */
import {
  Params,
  ExchangeRateTuple,
  AggregateExchangeRatePrevote,
  AggregateExchangeRateVote,
  VotePenaltyCounter,
} from "../oracle/oracle";
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.oracle";

export interface GenesisState {
  params: Params | undefined;
  feeder_delegations: FeederDelegation[];
  exchange_rates: ExchangeRateTuple[];
  penalty_counters: PenaltyCounter[];
  aggregate_exchange_rate_prevotes: AggregateExchangeRatePrevote[];
  aggregate_exchange_rate_votes: AggregateExchangeRateVote[];
}

export interface FeederDelegation {
  feeder_address: string;
  validator_address: string;
}

export interface PenaltyCounter {
  validator_address: string;
  vote_penalty_counter: VotePenaltyCounter | undefined;
}

const baseGenesisState: object = {};

export const GenesisState = {
  encode(message: GenesisState, writer: Writer = Writer.create()): Writer {
    if (message.params !== undefined) {
      Params.encode(message.params, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.feeder_delegations) {
      FeederDelegation.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.exchange_rates) {
      ExchangeRateTuple.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    for (const v of message.penalty_counters) {
      PenaltyCounter.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    for (const v of message.aggregate_exchange_rate_prevotes) {
      AggregateExchangeRatePrevote.encode(
        v!,
        writer.uint32(42).fork()
      ).ldelim();
    }
    for (const v of message.aggregate_exchange_rate_votes) {
      AggregateExchangeRateVote.encode(v!, writer.uint32(50).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisState } as GenesisState;
    message.feeder_delegations = [];
    message.exchange_rates = [];
    message.penalty_counters = [];
    message.aggregate_exchange_rate_prevotes = [];
    message.aggregate_exchange_rate_votes = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.params = Params.decode(reader, reader.uint32());
          break;
        case 2:
          message.feeder_delegations.push(
            FeederDelegation.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.exchange_rates.push(
            ExchangeRateTuple.decode(reader, reader.uint32())
          );
          break;
        case 4:
          message.penalty_counters.push(
            PenaltyCounter.decode(reader, reader.uint32())
          );
          break;
        case 5:
          message.aggregate_exchange_rate_prevotes.push(
            AggregateExchangeRatePrevote.decode(reader, reader.uint32())
          );
          break;
        case 6:
          message.aggregate_exchange_rate_votes.push(
            AggregateExchangeRateVote.decode(reader, reader.uint32())
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
    message.feeder_delegations = [];
    message.exchange_rates = [];
    message.penalty_counters = [];
    message.aggregate_exchange_rate_prevotes = [];
    message.aggregate_exchange_rate_votes = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromJSON(object.params);
    } else {
      message.params = undefined;
    }
    if (
      object.feeder_delegations !== undefined &&
      object.feeder_delegations !== null
    ) {
      for (const e of object.feeder_delegations) {
        message.feeder_delegations.push(FeederDelegation.fromJSON(e));
      }
    }
    if (object.exchange_rates !== undefined && object.exchange_rates !== null) {
      for (const e of object.exchange_rates) {
        message.exchange_rates.push(ExchangeRateTuple.fromJSON(e));
      }
    }
    if (
      object.penalty_counters !== undefined &&
      object.penalty_counters !== null
    ) {
      for (const e of object.penalty_counters) {
        message.penalty_counters.push(PenaltyCounter.fromJSON(e));
      }
    }
    if (
      object.aggregate_exchange_rate_prevotes !== undefined &&
      object.aggregate_exchange_rate_prevotes !== null
    ) {
      for (const e of object.aggregate_exchange_rate_prevotes) {
        message.aggregate_exchange_rate_prevotes.push(
          AggregateExchangeRatePrevote.fromJSON(e)
        );
      }
    }
    if (
      object.aggregate_exchange_rate_votes !== undefined &&
      object.aggregate_exchange_rate_votes !== null
    ) {
      for (const e of object.aggregate_exchange_rate_votes) {
        message.aggregate_exchange_rate_votes.push(
          AggregateExchangeRateVote.fromJSON(e)
        );
      }
    }
    return message;
  },

  toJSON(message: GenesisState): unknown {
    const obj: any = {};
    message.params !== undefined &&
      (obj.params = message.params ? Params.toJSON(message.params) : undefined);
    if (message.feeder_delegations) {
      obj.feeder_delegations = message.feeder_delegations.map((e) =>
        e ? FeederDelegation.toJSON(e) : undefined
      );
    } else {
      obj.feeder_delegations = [];
    }
    if (message.exchange_rates) {
      obj.exchange_rates = message.exchange_rates.map((e) =>
        e ? ExchangeRateTuple.toJSON(e) : undefined
      );
    } else {
      obj.exchange_rates = [];
    }
    if (message.penalty_counters) {
      obj.penalty_counters = message.penalty_counters.map((e) =>
        e ? PenaltyCounter.toJSON(e) : undefined
      );
    } else {
      obj.penalty_counters = [];
    }
    if (message.aggregate_exchange_rate_prevotes) {
      obj.aggregate_exchange_rate_prevotes = message.aggregate_exchange_rate_prevotes.map(
        (e) => (e ? AggregateExchangeRatePrevote.toJSON(e) : undefined)
      );
    } else {
      obj.aggregate_exchange_rate_prevotes = [];
    }
    if (message.aggregate_exchange_rate_votes) {
      obj.aggregate_exchange_rate_votes = message.aggregate_exchange_rate_votes.map(
        (e) => (e ? AggregateExchangeRateVote.toJSON(e) : undefined)
      );
    } else {
      obj.aggregate_exchange_rate_votes = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisState>): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.feeder_delegations = [];
    message.exchange_rates = [];
    message.penalty_counters = [];
    message.aggregate_exchange_rate_prevotes = [];
    message.aggregate_exchange_rate_votes = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromPartial(object.params);
    } else {
      message.params = undefined;
    }
    if (
      object.feeder_delegations !== undefined &&
      object.feeder_delegations !== null
    ) {
      for (const e of object.feeder_delegations) {
        message.feeder_delegations.push(FeederDelegation.fromPartial(e));
      }
    }
    if (object.exchange_rates !== undefined && object.exchange_rates !== null) {
      for (const e of object.exchange_rates) {
        message.exchange_rates.push(ExchangeRateTuple.fromPartial(e));
      }
    }
    if (
      object.penalty_counters !== undefined &&
      object.penalty_counters !== null
    ) {
      for (const e of object.penalty_counters) {
        message.penalty_counters.push(PenaltyCounter.fromPartial(e));
      }
    }
    if (
      object.aggregate_exchange_rate_prevotes !== undefined &&
      object.aggregate_exchange_rate_prevotes !== null
    ) {
      for (const e of object.aggregate_exchange_rate_prevotes) {
        message.aggregate_exchange_rate_prevotes.push(
          AggregateExchangeRatePrevote.fromPartial(e)
        );
      }
    }
    if (
      object.aggregate_exchange_rate_votes !== undefined &&
      object.aggregate_exchange_rate_votes !== null
    ) {
      for (const e of object.aggregate_exchange_rate_votes) {
        message.aggregate_exchange_rate_votes.push(
          AggregateExchangeRateVote.fromPartial(e)
        );
      }
    }
    return message;
  },
};

const baseFeederDelegation: object = {
  feeder_address: "",
  validator_address: "",
};

export const FeederDelegation = {
  encode(message: FeederDelegation, writer: Writer = Writer.create()): Writer {
    if (message.feeder_address !== "") {
      writer.uint32(10).string(message.feeder_address);
    }
    if (message.validator_address !== "") {
      writer.uint32(18).string(message.validator_address);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): FeederDelegation {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseFeederDelegation } as FeederDelegation;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.feeder_address = reader.string();
          break;
        case 2:
          message.validator_address = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): FeederDelegation {
    const message = { ...baseFeederDelegation } as FeederDelegation;
    if (object.feeder_address !== undefined && object.feeder_address !== null) {
      message.feeder_address = String(object.feeder_address);
    } else {
      message.feeder_address = "";
    }
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = String(object.validator_address);
    } else {
      message.validator_address = "";
    }
    return message;
  },

  toJSON(message: FeederDelegation): unknown {
    const obj: any = {};
    message.feeder_address !== undefined &&
      (obj.feeder_address = message.feeder_address);
    message.validator_address !== undefined &&
      (obj.validator_address = message.validator_address);
    return obj;
  },

  fromPartial(object: DeepPartial<FeederDelegation>): FeederDelegation {
    const message = { ...baseFeederDelegation } as FeederDelegation;
    if (object.feeder_address !== undefined && object.feeder_address !== null) {
      message.feeder_address = object.feeder_address;
    } else {
      message.feeder_address = "";
    }
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = object.validator_address;
    } else {
      message.validator_address = "";
    }
    return message;
  },
};

const basePenaltyCounter: object = { validator_address: "" };

export const PenaltyCounter = {
  encode(message: PenaltyCounter, writer: Writer = Writer.create()): Writer {
    if (message.validator_address !== "") {
      writer.uint32(10).string(message.validator_address);
    }
    if (message.vote_penalty_counter !== undefined) {
      VotePenaltyCounter.encode(
        message.vote_penalty_counter,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): PenaltyCounter {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePenaltyCounter } as PenaltyCounter;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validator_address = reader.string();
          break;
        case 2:
          message.vote_penalty_counter = VotePenaltyCounter.decode(
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

  fromJSON(object: any): PenaltyCounter {
    const message = { ...basePenaltyCounter } as PenaltyCounter;
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = String(object.validator_address);
    } else {
      message.validator_address = "";
    }
    if (
      object.vote_penalty_counter !== undefined &&
      object.vote_penalty_counter !== null
    ) {
      message.vote_penalty_counter = VotePenaltyCounter.fromJSON(
        object.vote_penalty_counter
      );
    } else {
      message.vote_penalty_counter = undefined;
    }
    return message;
  },

  toJSON(message: PenaltyCounter): unknown {
    const obj: any = {};
    message.validator_address !== undefined &&
      (obj.validator_address = message.validator_address);
    message.vote_penalty_counter !== undefined &&
      (obj.vote_penalty_counter = message.vote_penalty_counter
        ? VotePenaltyCounter.toJSON(message.vote_penalty_counter)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<PenaltyCounter>): PenaltyCounter {
    const message = { ...basePenaltyCounter } as PenaltyCounter;
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = object.validator_address;
    } else {
      message.validator_address = "";
    }
    if (
      object.vote_penalty_counter !== undefined &&
      object.vote_penalty_counter !== null
    ) {
      message.vote_penalty_counter = VotePenaltyCounter.fromPartial(
        object.vote_penalty_counter
      );
    } else {
      message.vote_penalty_counter = undefined;
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
