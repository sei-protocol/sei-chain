/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import {
  OracleExchangeRate,
  PriceSnapshot,
  OracleTwap,
  VotePenaltyCounter,
  Params,
} from "../../oracle/v1/oracle";

export const protobufPackage = "sei.oracle.v1";

/** QueryExchangeRateRequest is the request type for the Query/ExchangeRate RPC method. */
export interface QueryExchangeRateRequest {
  /** denom defines the denomination to query for. */
  denom: string;
}

/**
 * QueryExchangeRateResponse is response type for the
 * Query/ExchangeRate RPC method.
 */
export interface QueryExchangeRateResponse {
  /** exchange_rate defines the exchange rate of Sei denominated in various Sei */
  oracleExchangeRate: OracleExchangeRate | undefined;
}

/** QueryExchangeRatesRequest is the request type for the Query/ExchangeRates RPC method. */
export interface QueryExchangeRatesRequest {}

export interface DenomOracleExchangeRatePair {
  denom: string;
  oracleExchangeRate: OracleExchangeRate | undefined;
}

/**
 * QueryExchangeRatesResponse is response type for the
 * Query/ExchangeRates RPC method.
 */
export interface QueryExchangeRatesResponse {
  /** exchange_rates defines a list of the exchange rate for all whitelisted denoms. */
  denomOracleExchangeRatePairs: DenomOracleExchangeRatePair[];
}

/** QueryActivesRequest is the request type for the Query/Actives RPC method. */
export interface QueryActivesRequest {}

/**
 * QueryActivesResponse is response type for the
 * Query/Actives RPC method.
 */
export interface QueryActivesResponse {
  /** actives defines a list of the denomination which oracle prices aggreed upon. */
  actives: string[];
}

/** QueryVoteTargetsRequest is the request type for the Query/VoteTargets RPC method. */
export interface QueryVoteTargetsRequest {}

/**
 * QueryVoteTargetsResponse is response type for the
 * Query/VoteTargets RPC method.
 */
export interface QueryVoteTargetsResponse {
  /**
   * vote_targets defines a list of the denomination in which everyone
   * should vote in the current vote period.
   */
  voteTargets: string[];
}

/** request type for price snapshot history RPC method */
export interface QueryPriceSnapshotHistoryRequest {}

export interface QueryPriceSnapshotHistoryResponse {
  priceSnapshots: PriceSnapshot[];
}

/** request type for twap RPC method */
export interface QueryTwapsRequest {
  lookbackSeconds: number;
}

export interface QueryTwapsResponse {
  oracleTwaps: OracleTwap[];
}

/** QueryFeederDelegationRequest is the request type for the Query/FeederDelegation RPC method. */
export interface QueryFeederDelegationRequest {
  /** validator defines the validator address to query for. */
  validatorAddr: string;
}

/**
 * QueryFeederDelegationResponse is response type for the
 * Query/FeederDelegation RPC method.
 */
export interface QueryFeederDelegationResponse {
  /** feeder_addr defines the feeder delegation of a validator */
  feederAddr: string;
}

/** QueryVotePenaltyCounterRequest is the request type for the Query/MissCounter RPC method. */
export interface QueryVotePenaltyCounterRequest {
  /** validator defines the validator address to query for. */
  validatorAddr: string;
}

/**
 * QueryVotePenaltyCounterResponse is response type for the
 * Query/VotePenaltyCounter RPC method.
 */
export interface QueryVotePenaltyCounterResponse {
  votePenaltyCounter: VotePenaltyCounter | undefined;
}

/**
 * QuerySlashWindow is the request type for the
 * Query/SlashWindow RPC method.
 */
export interface QuerySlashWindowRequest {}

/**
 * QuerySlashWindowResponse is response type for the
 * Query/SlashWindow RPC method.
 */
export interface QuerySlashWindowResponse {
  /**
   * window_progress defines the number of voting periods
   * since the last slashing event would have taken place.
   */
  windowProgress: number;
}

/** QueryParamsRequest is the request type for the Query/Params RPC method. */
export interface QueryParamsRequest {}

/** QueryParamsResponse is the response type for the Query/Params RPC method. */
export interface QueryParamsResponse {
  /** params defines the parameters of the module. */
  params: Params | undefined;
}

const baseQueryExchangeRateRequest: object = { denom: "" };

export const QueryExchangeRateRequest = {
  encode(
    message: QueryExchangeRateRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.denom !== "") {
      writer.uint32(10).string(message.denom);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryExchangeRateRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryExchangeRateRequest,
    } as QueryExchangeRateRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.denom = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryExchangeRateRequest {
    const message = {
      ...baseQueryExchangeRateRequest,
    } as QueryExchangeRateRequest;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = String(object.denom);
    } else {
      message.denom = "";
    }
    return message;
  },

  toJSON(message: QueryExchangeRateRequest): unknown {
    const obj: any = {};
    message.denom !== undefined && (obj.denom = message.denom);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryExchangeRateRequest>
  ): QueryExchangeRateRequest {
    const message = {
      ...baseQueryExchangeRateRequest,
    } as QueryExchangeRateRequest;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = object.denom;
    } else {
      message.denom = "";
    }
    return message;
  },
};

const baseQueryExchangeRateResponse: object = {};

export const QueryExchangeRateResponse = {
  encode(
    message: QueryExchangeRateResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.oracleExchangeRate !== undefined) {
      OracleExchangeRate.encode(
        message.oracleExchangeRate,
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryExchangeRateResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryExchangeRateResponse,
    } as QueryExchangeRateResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
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

  fromJSON(object: any): QueryExchangeRateResponse {
    const message = {
      ...baseQueryExchangeRateResponse,
    } as QueryExchangeRateResponse;
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

  toJSON(message: QueryExchangeRateResponse): unknown {
    const obj: any = {};
    message.oracleExchangeRate !== undefined &&
      (obj.oracleExchangeRate = message.oracleExchangeRate
        ? OracleExchangeRate.toJSON(message.oracleExchangeRate)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryExchangeRateResponse>
  ): QueryExchangeRateResponse {
    const message = {
      ...baseQueryExchangeRateResponse,
    } as QueryExchangeRateResponse;
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

const baseQueryExchangeRatesRequest: object = {};

export const QueryExchangeRatesRequest = {
  encode(
    _: QueryExchangeRatesRequest,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryExchangeRatesRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryExchangeRatesRequest,
    } as QueryExchangeRatesRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(_: any): QueryExchangeRatesRequest {
    const message = {
      ...baseQueryExchangeRatesRequest,
    } as QueryExchangeRatesRequest;
    return message;
  },

  toJSON(_: QueryExchangeRatesRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<QueryExchangeRatesRequest>
  ): QueryExchangeRatesRequest {
    const message = {
      ...baseQueryExchangeRatesRequest,
    } as QueryExchangeRatesRequest;
    return message;
  },
};

const baseDenomOracleExchangeRatePair: object = { denom: "" };

export const DenomOracleExchangeRatePair = {
  encode(
    message: DenomOracleExchangeRatePair,
    writer: Writer = Writer.create()
  ): Writer {
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

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): DenomOracleExchangeRatePair {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseDenomOracleExchangeRatePair,
    } as DenomOracleExchangeRatePair;
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

  fromJSON(object: any): DenomOracleExchangeRatePair {
    const message = {
      ...baseDenomOracleExchangeRatePair,
    } as DenomOracleExchangeRatePair;
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

  toJSON(message: DenomOracleExchangeRatePair): unknown {
    const obj: any = {};
    message.denom !== undefined && (obj.denom = message.denom);
    message.oracleExchangeRate !== undefined &&
      (obj.oracleExchangeRate = message.oracleExchangeRate
        ? OracleExchangeRate.toJSON(message.oracleExchangeRate)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<DenomOracleExchangeRatePair>
  ): DenomOracleExchangeRatePair {
    const message = {
      ...baseDenomOracleExchangeRatePair,
    } as DenomOracleExchangeRatePair;
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

const baseQueryExchangeRatesResponse: object = {};

export const QueryExchangeRatesResponse = {
  encode(
    message: QueryExchangeRatesResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.denomOracleExchangeRatePairs) {
      DenomOracleExchangeRatePair.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryExchangeRatesResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryExchangeRatesResponse,
    } as QueryExchangeRatesResponse;
    message.denomOracleExchangeRatePairs = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.denomOracleExchangeRatePairs.push(
            DenomOracleExchangeRatePair.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryExchangeRatesResponse {
    const message = {
      ...baseQueryExchangeRatesResponse,
    } as QueryExchangeRatesResponse;
    message.denomOracleExchangeRatePairs = [];
    if (
      object.denomOracleExchangeRatePairs !== undefined &&
      object.denomOracleExchangeRatePairs !== null
    ) {
      for (const e of object.denomOracleExchangeRatePairs) {
        message.denomOracleExchangeRatePairs.push(
          DenomOracleExchangeRatePair.fromJSON(e)
        );
      }
    }
    return message;
  },

  toJSON(message: QueryExchangeRatesResponse): unknown {
    const obj: any = {};
    if (message.denomOracleExchangeRatePairs) {
      obj.denomOracleExchangeRatePairs = message.denomOracleExchangeRatePairs.map(
        (e) => (e ? DenomOracleExchangeRatePair.toJSON(e) : undefined)
      );
    } else {
      obj.denomOracleExchangeRatePairs = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryExchangeRatesResponse>
  ): QueryExchangeRatesResponse {
    const message = {
      ...baseQueryExchangeRatesResponse,
    } as QueryExchangeRatesResponse;
    message.denomOracleExchangeRatePairs = [];
    if (
      object.denomOracleExchangeRatePairs !== undefined &&
      object.denomOracleExchangeRatePairs !== null
    ) {
      for (const e of object.denomOracleExchangeRatePairs) {
        message.denomOracleExchangeRatePairs.push(
          DenomOracleExchangeRatePair.fromPartial(e)
        );
      }
    }
    return message;
  },
};

const baseQueryActivesRequest: object = {};

export const QueryActivesRequest = {
  encode(_: QueryActivesRequest, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryActivesRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryActivesRequest } as QueryActivesRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(_: any): QueryActivesRequest {
    const message = { ...baseQueryActivesRequest } as QueryActivesRequest;
    return message;
  },

  toJSON(_: QueryActivesRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(_: DeepPartial<QueryActivesRequest>): QueryActivesRequest {
    const message = { ...baseQueryActivesRequest } as QueryActivesRequest;
    return message;
  },
};

const baseQueryActivesResponse: object = { actives: "" };

export const QueryActivesResponse = {
  encode(
    message: QueryActivesResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.actives) {
      writer.uint32(10).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryActivesResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryActivesResponse } as QueryActivesResponse;
    message.actives = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.actives.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryActivesResponse {
    const message = { ...baseQueryActivesResponse } as QueryActivesResponse;
    message.actives = [];
    if (object.actives !== undefined && object.actives !== null) {
      for (const e of object.actives) {
        message.actives.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: QueryActivesResponse): unknown {
    const obj: any = {};
    if (message.actives) {
      obj.actives = message.actives.map((e) => e);
    } else {
      obj.actives = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<QueryActivesResponse>): QueryActivesResponse {
    const message = { ...baseQueryActivesResponse } as QueryActivesResponse;
    message.actives = [];
    if (object.actives !== undefined && object.actives !== null) {
      for (const e of object.actives) {
        message.actives.push(e);
      }
    }
    return message;
  },
};

const baseQueryVoteTargetsRequest: object = {};

export const QueryVoteTargetsRequest = {
  encode(_: QueryVoteTargetsRequest, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryVoteTargetsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryVoteTargetsRequest,
    } as QueryVoteTargetsRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(_: any): QueryVoteTargetsRequest {
    const message = {
      ...baseQueryVoteTargetsRequest,
    } as QueryVoteTargetsRequest;
    return message;
  },

  toJSON(_: QueryVoteTargetsRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<QueryVoteTargetsRequest>
  ): QueryVoteTargetsRequest {
    const message = {
      ...baseQueryVoteTargetsRequest,
    } as QueryVoteTargetsRequest;
    return message;
  },
};

const baseQueryVoteTargetsResponse: object = { voteTargets: "" };

export const QueryVoteTargetsResponse = {
  encode(
    message: QueryVoteTargetsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.voteTargets) {
      writer.uint32(10).string(v!);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryVoteTargetsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryVoteTargetsResponse,
    } as QueryVoteTargetsResponse;
    message.voteTargets = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.voteTargets.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryVoteTargetsResponse {
    const message = {
      ...baseQueryVoteTargetsResponse,
    } as QueryVoteTargetsResponse;
    message.voteTargets = [];
    if (object.voteTargets !== undefined && object.voteTargets !== null) {
      for (const e of object.voteTargets) {
        message.voteTargets.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: QueryVoteTargetsResponse): unknown {
    const obj: any = {};
    if (message.voteTargets) {
      obj.voteTargets = message.voteTargets.map((e) => e);
    } else {
      obj.voteTargets = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryVoteTargetsResponse>
  ): QueryVoteTargetsResponse {
    const message = {
      ...baseQueryVoteTargetsResponse,
    } as QueryVoteTargetsResponse;
    message.voteTargets = [];
    if (object.voteTargets !== undefined && object.voteTargets !== null) {
      for (const e of object.voteTargets) {
        message.voteTargets.push(e);
      }
    }
    return message;
  },
};

const baseQueryPriceSnapshotHistoryRequest: object = {};

export const QueryPriceSnapshotHistoryRequest = {
  encode(
    _: QueryPriceSnapshotHistoryRequest,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryPriceSnapshotHistoryRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryPriceSnapshotHistoryRequest,
    } as QueryPriceSnapshotHistoryRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(_: any): QueryPriceSnapshotHistoryRequest {
    const message = {
      ...baseQueryPriceSnapshotHistoryRequest,
    } as QueryPriceSnapshotHistoryRequest;
    return message;
  },

  toJSON(_: QueryPriceSnapshotHistoryRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<QueryPriceSnapshotHistoryRequest>
  ): QueryPriceSnapshotHistoryRequest {
    const message = {
      ...baseQueryPriceSnapshotHistoryRequest,
    } as QueryPriceSnapshotHistoryRequest;
    return message;
  },
};

const baseQueryPriceSnapshotHistoryResponse: object = {};

export const QueryPriceSnapshotHistoryResponse = {
  encode(
    message: QueryPriceSnapshotHistoryResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.priceSnapshots) {
      PriceSnapshot.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryPriceSnapshotHistoryResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryPriceSnapshotHistoryResponse,
    } as QueryPriceSnapshotHistoryResponse;
    message.priceSnapshots = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
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

  fromJSON(object: any): QueryPriceSnapshotHistoryResponse {
    const message = {
      ...baseQueryPriceSnapshotHistoryResponse,
    } as QueryPriceSnapshotHistoryResponse;
    message.priceSnapshots = [];
    if (object.priceSnapshots !== undefined && object.priceSnapshots !== null) {
      for (const e of object.priceSnapshots) {
        message.priceSnapshots.push(PriceSnapshot.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: QueryPriceSnapshotHistoryResponse): unknown {
    const obj: any = {};
    if (message.priceSnapshots) {
      obj.priceSnapshots = message.priceSnapshots.map((e) =>
        e ? PriceSnapshot.toJSON(e) : undefined
      );
    } else {
      obj.priceSnapshots = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryPriceSnapshotHistoryResponse>
  ): QueryPriceSnapshotHistoryResponse {
    const message = {
      ...baseQueryPriceSnapshotHistoryResponse,
    } as QueryPriceSnapshotHistoryResponse;
    message.priceSnapshots = [];
    if (object.priceSnapshots !== undefined && object.priceSnapshots !== null) {
      for (const e of object.priceSnapshots) {
        message.priceSnapshots.push(PriceSnapshot.fromPartial(e));
      }
    }
    return message;
  },
};

const baseQueryTwapsRequest: object = { lookbackSeconds: 0 };

export const QueryTwapsRequest = {
  encode(message: QueryTwapsRequest, writer: Writer = Writer.create()): Writer {
    if (message.lookbackSeconds !== 0) {
      writer.uint32(8).uint64(message.lookbackSeconds);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryTwapsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryTwapsRequest } as QueryTwapsRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.lookbackSeconds = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryTwapsRequest {
    const message = { ...baseQueryTwapsRequest } as QueryTwapsRequest;
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

  toJSON(message: QueryTwapsRequest): unknown {
    const obj: any = {};
    message.lookbackSeconds !== undefined &&
      (obj.lookbackSeconds = message.lookbackSeconds);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryTwapsRequest>): QueryTwapsRequest {
    const message = { ...baseQueryTwapsRequest } as QueryTwapsRequest;
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

const baseQueryTwapsResponse: object = {};

export const QueryTwapsResponse = {
  encode(
    message: QueryTwapsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.oracleTwaps) {
      OracleTwap.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryTwapsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryTwapsResponse } as QueryTwapsResponse;
    message.oracleTwaps = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.oracleTwaps.push(OracleTwap.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryTwapsResponse {
    const message = { ...baseQueryTwapsResponse } as QueryTwapsResponse;
    message.oracleTwaps = [];
    if (object.oracleTwaps !== undefined && object.oracleTwaps !== null) {
      for (const e of object.oracleTwaps) {
        message.oracleTwaps.push(OracleTwap.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: QueryTwapsResponse): unknown {
    const obj: any = {};
    if (message.oracleTwaps) {
      obj.oracleTwaps = message.oracleTwaps.map((e) =>
        e ? OracleTwap.toJSON(e) : undefined
      );
    } else {
      obj.oracleTwaps = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<QueryTwapsResponse>): QueryTwapsResponse {
    const message = { ...baseQueryTwapsResponse } as QueryTwapsResponse;
    message.oracleTwaps = [];
    if (object.oracleTwaps !== undefined && object.oracleTwaps !== null) {
      for (const e of object.oracleTwaps) {
        message.oracleTwaps.push(OracleTwap.fromPartial(e));
      }
    }
    return message;
  },
};

const baseQueryFeederDelegationRequest: object = { validatorAddr: "" };

export const QueryFeederDelegationRequest = {
  encode(
    message: QueryFeederDelegationRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.validatorAddr !== "") {
      writer.uint32(10).string(message.validatorAddr);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryFeederDelegationRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryFeederDelegationRequest,
    } as QueryFeederDelegationRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validatorAddr = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryFeederDelegationRequest {
    const message = {
      ...baseQueryFeederDelegationRequest,
    } as QueryFeederDelegationRequest;
    if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
      message.validatorAddr = String(object.validatorAddr);
    } else {
      message.validatorAddr = "";
    }
    return message;
  },

  toJSON(message: QueryFeederDelegationRequest): unknown {
    const obj: any = {};
    message.validatorAddr !== undefined &&
      (obj.validatorAddr = message.validatorAddr);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryFeederDelegationRequest>
  ): QueryFeederDelegationRequest {
    const message = {
      ...baseQueryFeederDelegationRequest,
    } as QueryFeederDelegationRequest;
    if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
      message.validatorAddr = object.validatorAddr;
    } else {
      message.validatorAddr = "";
    }
    return message;
  },
};

const baseQueryFeederDelegationResponse: object = { feederAddr: "" };

export const QueryFeederDelegationResponse = {
  encode(
    message: QueryFeederDelegationResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.feederAddr !== "") {
      writer.uint32(10).string(message.feederAddr);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryFeederDelegationResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryFeederDelegationResponse,
    } as QueryFeederDelegationResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.feederAddr = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryFeederDelegationResponse {
    const message = {
      ...baseQueryFeederDelegationResponse,
    } as QueryFeederDelegationResponse;
    if (object.feederAddr !== undefined && object.feederAddr !== null) {
      message.feederAddr = String(object.feederAddr);
    } else {
      message.feederAddr = "";
    }
    return message;
  },

  toJSON(message: QueryFeederDelegationResponse): unknown {
    const obj: any = {};
    message.feederAddr !== undefined && (obj.feederAddr = message.feederAddr);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryFeederDelegationResponse>
  ): QueryFeederDelegationResponse {
    const message = {
      ...baseQueryFeederDelegationResponse,
    } as QueryFeederDelegationResponse;
    if (object.feederAddr !== undefined && object.feederAddr !== null) {
      message.feederAddr = object.feederAddr;
    } else {
      message.feederAddr = "";
    }
    return message;
  },
};

const baseQueryVotePenaltyCounterRequest: object = { validatorAddr: "" };

export const QueryVotePenaltyCounterRequest = {
  encode(
    message: QueryVotePenaltyCounterRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.validatorAddr !== "") {
      writer.uint32(10).string(message.validatorAddr);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryVotePenaltyCounterRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryVotePenaltyCounterRequest,
    } as QueryVotePenaltyCounterRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validatorAddr = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryVotePenaltyCounterRequest {
    const message = {
      ...baseQueryVotePenaltyCounterRequest,
    } as QueryVotePenaltyCounterRequest;
    if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
      message.validatorAddr = String(object.validatorAddr);
    } else {
      message.validatorAddr = "";
    }
    return message;
  },

  toJSON(message: QueryVotePenaltyCounterRequest): unknown {
    const obj: any = {};
    message.validatorAddr !== undefined &&
      (obj.validatorAddr = message.validatorAddr);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryVotePenaltyCounterRequest>
  ): QueryVotePenaltyCounterRequest {
    const message = {
      ...baseQueryVotePenaltyCounterRequest,
    } as QueryVotePenaltyCounterRequest;
    if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
      message.validatorAddr = object.validatorAddr;
    } else {
      message.validatorAddr = "";
    }
    return message;
  },
};

const baseQueryVotePenaltyCounterResponse: object = {};

export const QueryVotePenaltyCounterResponse = {
  encode(
    message: QueryVotePenaltyCounterResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.votePenaltyCounter !== undefined) {
      VotePenaltyCounter.encode(
        message.votePenaltyCounter,
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryVotePenaltyCounterResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryVotePenaltyCounterResponse,
    } as QueryVotePenaltyCounterResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
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

  fromJSON(object: any): QueryVotePenaltyCounterResponse {
    const message = {
      ...baseQueryVotePenaltyCounterResponse,
    } as QueryVotePenaltyCounterResponse;
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

  toJSON(message: QueryVotePenaltyCounterResponse): unknown {
    const obj: any = {};
    message.votePenaltyCounter !== undefined &&
      (obj.votePenaltyCounter = message.votePenaltyCounter
        ? VotePenaltyCounter.toJSON(message.votePenaltyCounter)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryVotePenaltyCounterResponse>
  ): QueryVotePenaltyCounterResponse {
    const message = {
      ...baseQueryVotePenaltyCounterResponse,
    } as QueryVotePenaltyCounterResponse;
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

const baseQuerySlashWindowRequest: object = {};

export const QuerySlashWindowRequest = {
  encode(_: QuerySlashWindowRequest, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QuerySlashWindowRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQuerySlashWindowRequest,
    } as QuerySlashWindowRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(_: any): QuerySlashWindowRequest {
    const message = {
      ...baseQuerySlashWindowRequest,
    } as QuerySlashWindowRequest;
    return message;
  },

  toJSON(_: QuerySlashWindowRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<QuerySlashWindowRequest>
  ): QuerySlashWindowRequest {
    const message = {
      ...baseQuerySlashWindowRequest,
    } as QuerySlashWindowRequest;
    return message;
  },
};

const baseQuerySlashWindowResponse: object = { windowProgress: 0 };

export const QuerySlashWindowResponse = {
  encode(
    message: QuerySlashWindowResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.windowProgress !== 0) {
      writer.uint32(8).uint64(message.windowProgress);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QuerySlashWindowResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQuerySlashWindowResponse,
    } as QuerySlashWindowResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.windowProgress = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QuerySlashWindowResponse {
    const message = {
      ...baseQuerySlashWindowResponse,
    } as QuerySlashWindowResponse;
    if (object.windowProgress !== undefined && object.windowProgress !== null) {
      message.windowProgress = Number(object.windowProgress);
    } else {
      message.windowProgress = 0;
    }
    return message;
  },

  toJSON(message: QuerySlashWindowResponse): unknown {
    const obj: any = {};
    message.windowProgress !== undefined &&
      (obj.windowProgress = message.windowProgress);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QuerySlashWindowResponse>
  ): QuerySlashWindowResponse {
    const message = {
      ...baseQuerySlashWindowResponse,
    } as QuerySlashWindowResponse;
    if (object.windowProgress !== undefined && object.windowProgress !== null) {
      message.windowProgress = object.windowProgress;
    } else {
      message.windowProgress = 0;
    }
    return message;
  },
};

const baseQueryParamsRequest: object = {};

export const QueryParamsRequest = {
  encode(_: QueryParamsRequest, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryParamsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryParamsRequest } as QueryParamsRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(_: any): QueryParamsRequest {
    const message = { ...baseQueryParamsRequest } as QueryParamsRequest;
    return message;
  },

  toJSON(_: QueryParamsRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(_: DeepPartial<QueryParamsRequest>): QueryParamsRequest {
    const message = { ...baseQueryParamsRequest } as QueryParamsRequest;
    return message;
  },
};

const baseQueryParamsResponse: object = {};

export const QueryParamsResponse = {
  encode(
    message: QueryParamsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.params !== undefined) {
      Params.encode(message.params, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryParamsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryParamsResponse } as QueryParamsResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.params = Params.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryParamsResponse {
    const message = { ...baseQueryParamsResponse } as QueryParamsResponse;
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromJSON(object.params);
    } else {
      message.params = undefined;
    }
    return message;
  },

  toJSON(message: QueryParamsResponse): unknown {
    const obj: any = {};
    message.params !== undefined &&
      (obj.params = message.params ? Params.toJSON(message.params) : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryParamsResponse>): QueryParamsResponse {
    const message = { ...baseQueryParamsResponse } as QueryParamsResponse;
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromPartial(object.params);
    } else {
      message.params = undefined;
    }
    return message;
  },
};

/** Query defines the gRPC querier service. */
export interface Query {
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/denoms/{denom}/exchange_rate` instead.
   *
   * @deprecated
   */
  deprecated_ExchangeRate(
    request: QueryExchangeRateRequest
  ): Promise<QueryExchangeRateResponse>;
  /** ExchangeRate returns exchange rate of a denom */
  ExchangeRate(
    request: QueryExchangeRateRequest
  ): Promise<QueryExchangeRateResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/denoms/exchange_rates` instead.
   *
   * @deprecated
   */
  deprecated_ExchangeRates(
    request: QueryExchangeRatesRequest
  ): Promise<QueryExchangeRatesResponse>;
  /** ExchangeRates returns exchange rates of all denoms */
  ExchangeRates(
    request: QueryExchangeRatesRequest
  ): Promise<QueryExchangeRatesResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/denoms/actives` instead.
   *
   * @deprecated
   */
  deprecated_Actives(
    request: QueryActivesRequest
  ): Promise<QueryActivesResponse>;
  /** Actives returns all active denoms */
  Actives(request: QueryActivesRequest): Promise<QueryActivesResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/denoms/vote_targets` instead.
   *
   * @deprecated
   */
  deprecated_VoteTargets(
    request: QueryVoteTargetsRequest
  ): Promise<QueryVoteTargetsResponse>;
  /** VoteTargets returns all vote target denoms */
  VoteTargets(
    request: QueryVoteTargetsRequest
  ): Promise<QueryVoteTargetsResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/denoms/price_snapshot_history` instead.
   *
   * @deprecated
   */
  deprecated_PriceSnapshotHistory(
    request: QueryPriceSnapshotHistoryRequest
  ): Promise<QueryPriceSnapshotHistoryResponse>;
  /** PriceSnapshotHistory returns the history of price snapshots for all assets */
  PriceSnapshotHistory(
    request: QueryPriceSnapshotHistoryRequest
  ): Promise<QueryPriceSnapshotHistoryResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/denoms/twaps/{lookback_seconds}` instead.
   *
   * @deprecated
   */
  deprecated_Twaps(request: QueryTwapsRequest): Promise<QueryTwapsResponse>;
  Twaps(request: QueryTwapsRequest): Promise<QueryTwapsResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/validators/{validator_addr}/feeder` instead.
   *
   * @deprecated
   */
  deprecated_FeederDelegation(
    request: QueryFeederDelegationRequest
  ): Promise<QueryFeederDelegationResponse>;
  /** FeederDelegation returns feeder delegation of a validator */
  FeederDelegation(
    request: QueryFeederDelegationRequest
  ): Promise<QueryFeederDelegationResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/validators/{validator_addr}/vote_penalty_counter` instead.
   *
   * @deprecated
   */
  deprecated_VotePenaltyCounter(
    request: QueryVotePenaltyCounterRequest
  ): Promise<QueryVotePenaltyCounterResponse>;
  /** MissCounter returns oracle miss counter of a validator */
  VotePenaltyCounter(
    request: QueryVotePenaltyCounterRequest
  ): Promise<QueryVotePenaltyCounterResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/slash_window` instead.
   *
   * @deprecated
   */
  deprecated_SlashWindow(
    request: QuerySlashWindowRequest
  ): Promise<QuerySlashWindowResponse>;
  /** SlashWindow returns slash window information */
  SlashWindow(
    request: QuerySlashWindowRequest
  ): Promise<QuerySlashWindowResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/oracle/v1/params` instead.
   *
   * @deprecated
   */
  deprecated_Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
  /** Params queries all parameters. */
  Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  deprecated_ExchangeRate(
    request: QueryExchangeRateRequest
  ): Promise<QueryExchangeRateResponse> {
    const data = QueryExchangeRateRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "deprecated_ExchangeRate",
      data
    );
    return promise.then((data) =>
      QueryExchangeRateResponse.decode(new Reader(data))
    );
  }

  ExchangeRate(
    request: QueryExchangeRateRequest
  ): Promise<QueryExchangeRateResponse> {
    const data = QueryExchangeRateRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "ExchangeRate",
      data
    );
    return promise.then((data) =>
      QueryExchangeRateResponse.decode(new Reader(data))
    );
  }

  deprecated_ExchangeRates(
    request: QueryExchangeRatesRequest
  ): Promise<QueryExchangeRatesResponse> {
    const data = QueryExchangeRatesRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "deprecated_ExchangeRates",
      data
    );
    return promise.then((data) =>
      QueryExchangeRatesResponse.decode(new Reader(data))
    );
  }

  ExchangeRates(
    request: QueryExchangeRatesRequest
  ): Promise<QueryExchangeRatesResponse> {
    const data = QueryExchangeRatesRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "ExchangeRates",
      data
    );
    return promise.then((data) =>
      QueryExchangeRatesResponse.decode(new Reader(data))
    );
  }

  deprecated_Actives(
    request: QueryActivesRequest
  ): Promise<QueryActivesResponse> {
    const data = QueryActivesRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "deprecated_Actives",
      data
    );
    return promise.then((data) =>
      QueryActivesResponse.decode(new Reader(data))
    );
  }

  Actives(request: QueryActivesRequest): Promise<QueryActivesResponse> {
    const data = QueryActivesRequest.encode(request).finish();
    const promise = this.rpc.request("sei.oracle.v1.Query", "Actives", data);
    return promise.then((data) =>
      QueryActivesResponse.decode(new Reader(data))
    );
  }

  deprecated_VoteTargets(
    request: QueryVoteTargetsRequest
  ): Promise<QueryVoteTargetsResponse> {
    const data = QueryVoteTargetsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "deprecated_VoteTargets",
      data
    );
    return promise.then((data) =>
      QueryVoteTargetsResponse.decode(new Reader(data))
    );
  }

  VoteTargets(
    request: QueryVoteTargetsRequest
  ): Promise<QueryVoteTargetsResponse> {
    const data = QueryVoteTargetsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "VoteTargets",
      data
    );
    return promise.then((data) =>
      QueryVoteTargetsResponse.decode(new Reader(data))
    );
  }

  deprecated_PriceSnapshotHistory(
    request: QueryPriceSnapshotHistoryRequest
  ): Promise<QueryPriceSnapshotHistoryResponse> {
    const data = QueryPriceSnapshotHistoryRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "deprecated_PriceSnapshotHistory",
      data
    );
    return promise.then((data) =>
      QueryPriceSnapshotHistoryResponse.decode(new Reader(data))
    );
  }

  PriceSnapshotHistory(
    request: QueryPriceSnapshotHistoryRequest
  ): Promise<QueryPriceSnapshotHistoryResponse> {
    const data = QueryPriceSnapshotHistoryRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "PriceSnapshotHistory",
      data
    );
    return promise.then((data) =>
      QueryPriceSnapshotHistoryResponse.decode(new Reader(data))
    );
  }

  deprecated_Twaps(request: QueryTwapsRequest): Promise<QueryTwapsResponse> {
    const data = QueryTwapsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "deprecated_Twaps",
      data
    );
    return promise.then((data) => QueryTwapsResponse.decode(new Reader(data)));
  }

  Twaps(request: QueryTwapsRequest): Promise<QueryTwapsResponse> {
    const data = QueryTwapsRequest.encode(request).finish();
    const promise = this.rpc.request("sei.oracle.v1.Query", "Twaps", data);
    return promise.then((data) => QueryTwapsResponse.decode(new Reader(data)));
  }

  deprecated_FeederDelegation(
    request: QueryFeederDelegationRequest
  ): Promise<QueryFeederDelegationResponse> {
    const data = QueryFeederDelegationRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "deprecated_FeederDelegation",
      data
    );
    return promise.then((data) =>
      QueryFeederDelegationResponse.decode(new Reader(data))
    );
  }

  FeederDelegation(
    request: QueryFeederDelegationRequest
  ): Promise<QueryFeederDelegationResponse> {
    const data = QueryFeederDelegationRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "FeederDelegation",
      data
    );
    return promise.then((data) =>
      QueryFeederDelegationResponse.decode(new Reader(data))
    );
  }

  deprecated_VotePenaltyCounter(
    request: QueryVotePenaltyCounterRequest
  ): Promise<QueryVotePenaltyCounterResponse> {
    const data = QueryVotePenaltyCounterRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "deprecated_VotePenaltyCounter",
      data
    );
    return promise.then((data) =>
      QueryVotePenaltyCounterResponse.decode(new Reader(data))
    );
  }

  VotePenaltyCounter(
    request: QueryVotePenaltyCounterRequest
  ): Promise<QueryVotePenaltyCounterResponse> {
    const data = QueryVotePenaltyCounterRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "VotePenaltyCounter",
      data
    );
    return promise.then((data) =>
      QueryVotePenaltyCounterResponse.decode(new Reader(data))
    );
  }

  deprecated_SlashWindow(
    request: QuerySlashWindowRequest
  ): Promise<QuerySlashWindowResponse> {
    const data = QuerySlashWindowRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "deprecated_SlashWindow",
      data
    );
    return promise.then((data) =>
      QuerySlashWindowResponse.decode(new Reader(data))
    );
  }

  SlashWindow(
    request: QuerySlashWindowRequest
  ): Promise<QuerySlashWindowResponse> {
    const data = QuerySlashWindowRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "SlashWindow",
      data
    );
    return promise.then((data) =>
      QuerySlashWindowResponse.decode(new Reader(data))
    );
  }

  deprecated_Params(request: QueryParamsRequest): Promise<QueryParamsResponse> {
    const data = QueryParamsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.oracle.v1.Query",
      "deprecated_Params",
      data
    );
    return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
  }

  Params(request: QueryParamsRequest): Promise<QueryParamsResponse> {
    const data = QueryParamsRequest.encode(request).finish();
    const promise = this.rpc.request("sei.oracle.v1.Query", "Params", data);
    return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
  }
}

interface Rpc {
  request(
    service: string,
    method: string,
    data: Uint8Array
  ): Promise<Uint8Array>;
}

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
