/* eslint-disable */
import {
  Params,
  ExchangeRateTuple,
  AggregateExchangeRateVote,
  PriceSnapshot,
  VotePenaltyCounter,
} from "../../oracle/v1/oracle";
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "sei.oracle.v1";

export interface GenesisState {
  params: Params | undefined;
  feederDelegations: FeederDelegation[];
  exchangeRates: ExchangeRateTuple[];
  penaltyCounters: PenaltyCounter[];
  aggregateExchangeRateVotes: AggregateExchangeRateVote[];
  priceSnapshots: PriceSnapshot[];
}

export interface FeederDelegation {
  feederAddress: string;
  validatorAddress: string;
}

export interface PenaltyCounter {
  validatorAddress: string;
  votePenaltyCounter: VotePenaltyCounter | undefined;
}

const baseGenesisState: object = {};

export const GenesisState = {
  encode(message: GenesisState, writer: Writer = Writer.create()): Writer {
    if (message.params !== undefined) {
      Params.encode(message.params, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.feederDelegations) {
      FeederDelegation.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.exchangeRates) {
      ExchangeRateTuple.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    for (const v of message.penaltyCounters) {
      PenaltyCounter.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    for (const v of message.aggregateExchangeRateVotes) {
      AggregateExchangeRateVote.encode(v!, writer.uint32(50).fork()).ldelim();
    }
    for (const v of message.priceSnapshots) {
      PriceSnapshot.encode(v!, writer.uint32(58).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisState } as GenesisState;
    message.feederDelegations = [];
    message.exchangeRates = [];
    message.penaltyCounters = [];
    message.aggregateExchangeRateVotes = [];
    message.priceSnapshots = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.params = Params.decode(reader, reader.uint32());
          break;
        case 2:
          message.feederDelegations.push(
            FeederDelegation.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.exchangeRates.push(
            ExchangeRateTuple.decode(reader, reader.uint32())
          );
          break;
        case 4:
          message.penaltyCounters.push(
            PenaltyCounter.decode(reader, reader.uint32())
          );
          break;
        case 6:
          message.aggregateExchangeRateVotes.push(
            AggregateExchangeRateVote.decode(reader, reader.uint32())
          );
          break;
        case 7:
          message.priceSnapshots.push(
            PriceSnapshot.decode(reader, reader.uint32())
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
    message.feederDelegations = [];
    message.exchangeRates = [];
    message.penaltyCounters = [];
    message.aggregateExchangeRateVotes = [];
    message.priceSnapshots = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromJSON(object.params);
    } else {
      message.params = undefined;
    }
    if (
      object.feederDelegations !== undefined &&
      object.feederDelegations !== null
    ) {
      for (const e of object.feederDelegations) {
        message.feederDelegations.push(FeederDelegation.fromJSON(e));
      }
    }
    if (object.exchangeRates !== undefined && object.exchangeRates !== null) {
      for (const e of object.exchangeRates) {
        message.exchangeRates.push(ExchangeRateTuple.fromJSON(e));
      }
    }
    if (
      object.penaltyCounters !== undefined &&
      object.penaltyCounters !== null
    ) {
      for (const e of object.penaltyCounters) {
        message.penaltyCounters.push(PenaltyCounter.fromJSON(e));
      }
    }
    if (
      object.aggregateExchangeRateVotes !== undefined &&
      object.aggregateExchangeRateVotes !== null
    ) {
      for (const e of object.aggregateExchangeRateVotes) {
        message.aggregateExchangeRateVotes.push(
          AggregateExchangeRateVote.fromJSON(e)
        );
      }
    }
    if (object.priceSnapshots !== undefined && object.priceSnapshots !== null) {
      for (const e of object.priceSnapshots) {
        message.priceSnapshots.push(PriceSnapshot.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: GenesisState): unknown {
    const obj: any = {};
    message.params !== undefined &&
      (obj.params = message.params ? Params.toJSON(message.params) : undefined);
    if (message.feederDelegations) {
      obj.feederDelegations = message.feederDelegations.map((e) =>
        e ? FeederDelegation.toJSON(e) : undefined
      );
    } else {
      obj.feederDelegations = [];
    }
    if (message.exchangeRates) {
      obj.exchangeRates = message.exchangeRates.map((e) =>
        e ? ExchangeRateTuple.toJSON(e) : undefined
      );
    } else {
      obj.exchangeRates = [];
    }
    if (message.penaltyCounters) {
      obj.penaltyCounters = message.penaltyCounters.map((e) =>
        e ? PenaltyCounter.toJSON(e) : undefined
      );
    } else {
      obj.penaltyCounters = [];
    }
    if (message.aggregateExchangeRateVotes) {
      obj.aggregateExchangeRateVotes = message.aggregateExchangeRateVotes.map(
        (e) => (e ? AggregateExchangeRateVote.toJSON(e) : undefined)
      );
    } else {
      obj.aggregateExchangeRateVotes = [];
    }
    if (message.priceSnapshots) {
      obj.priceSnapshots = message.priceSnapshots.map((e) =>
        e ? PriceSnapshot.toJSON(e) : undefined
      );
    } else {
      obj.priceSnapshots = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisState>): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.feederDelegations = [];
    message.exchangeRates = [];
    message.penaltyCounters = [];
    message.aggregateExchangeRateVotes = [];
    message.priceSnapshots = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromPartial(object.params);
    } else {
      message.params = undefined;
    }
    if (
      object.feederDelegations !== undefined &&
      object.feederDelegations !== null
    ) {
      for (const e of object.feederDelegations) {
        message.feederDelegations.push(FeederDelegation.fromPartial(e));
      }
    }
    if (object.exchangeRates !== undefined && object.exchangeRates !== null) {
      for (const e of object.exchangeRates) {
        message.exchangeRates.push(ExchangeRateTuple.fromPartial(e));
      }
    }
    if (
      object.penaltyCounters !== undefined &&
      object.penaltyCounters !== null
    ) {
      for (const e of object.penaltyCounters) {
        message.penaltyCounters.push(PenaltyCounter.fromPartial(e));
      }
    }
    if (
      object.aggregateExchangeRateVotes !== undefined &&
      object.aggregateExchangeRateVotes !== null
    ) {
      for (const e of object.aggregateExchangeRateVotes) {
        message.aggregateExchangeRateVotes.push(
          AggregateExchangeRateVote.fromPartial(e)
        );
      }
    }
    if (object.priceSnapshots !== undefined && object.priceSnapshots !== null) {
      for (const e of object.priceSnapshots) {
        message.priceSnapshots.push(PriceSnapshot.fromPartial(e));
      }
    }
    return message;
  },
};

const baseFeederDelegation: object = {
  feederAddress: "",
  validatorAddress: "",
};

export const FeederDelegation = {
  encode(message: FeederDelegation, writer: Writer = Writer.create()): Writer {
    if (message.feederAddress !== "") {
      writer.uint32(10).string(message.feederAddress);
    }
    if (message.validatorAddress !== "") {
      writer.uint32(18).string(message.validatorAddress);
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
          message.feederAddress = reader.string();
          break;
        case 2:
          message.validatorAddress = reader.string();
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
    if (object.feederAddress !== undefined && object.feederAddress !== null) {
      message.feederAddress = String(object.feederAddress);
    } else {
      message.feederAddress = "";
    }
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = String(object.validatorAddress);
    } else {
      message.validatorAddress = "";
    }
    return message;
  },

  toJSON(message: FeederDelegation): unknown {
    const obj: any = {};
    message.feederAddress !== undefined &&
      (obj.feederAddress = message.feederAddress);
    message.validatorAddress !== undefined &&
      (obj.validatorAddress = message.validatorAddress);
    return obj;
  },

  fromPartial(object: DeepPartial<FeederDelegation>): FeederDelegation {
    const message = { ...baseFeederDelegation } as FeederDelegation;
    if (object.feederAddress !== undefined && object.feederAddress !== null) {
      message.feederAddress = object.feederAddress;
    } else {
      message.feederAddress = "";
    }
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = object.validatorAddress;
    } else {
      message.validatorAddress = "";
    }
    return message;
  },
};

const basePenaltyCounter: object = { validatorAddress: "" };

export const PenaltyCounter = {
  encode(message: PenaltyCounter, writer: Writer = Writer.create()): Writer {
    if (message.validatorAddress !== "") {
      writer.uint32(10).string(message.validatorAddress);
    }
    if (message.votePenaltyCounter !== undefined) {
      VotePenaltyCounter.encode(
        message.votePenaltyCounter,
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
          message.validatorAddress = reader.string();
          break;
        case 2:
          message.votePenaltyCounter = VotePenaltyCounter.decode(
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
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = String(object.validatorAddress);
    } else {
      message.validatorAddress = "";
    }
    if (
      object.votePenaltyCounter !== undefined &&
      object.votePenaltyCounter !== null
    ) {
      message.votePenaltyCounter = VotePenaltyCounter.fromJSON(
        object.votePenaltyCounter
      );
    } else {
      message.votePenaltyCounter = undefined;
    }
    return message;
  },

  toJSON(message: PenaltyCounter): unknown {
    const obj: any = {};
    message.validatorAddress !== undefined &&
      (obj.validatorAddress = message.validatorAddress);
    message.votePenaltyCounter !== undefined &&
      (obj.votePenaltyCounter = message.votePenaltyCounter
        ? VotePenaltyCounter.toJSON(message.votePenaltyCounter)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<PenaltyCounter>): PenaltyCounter {
    const message = { ...basePenaltyCounter } as PenaltyCounter;
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = object.validatorAddress;
    } else {
      message.validatorAddress = "";
    }
    if (
      object.votePenaltyCounter !== undefined &&
      object.votePenaltyCounter !== null
    ) {
      message.votePenaltyCounter = VotePenaltyCounter.fromPartial(
        object.votePenaltyCounter
      );
    } else {
      message.votePenaltyCounter = undefined;
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
