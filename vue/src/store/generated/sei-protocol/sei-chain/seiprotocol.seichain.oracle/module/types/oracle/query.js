/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import { OracleExchangeRate, PriceSnapshot, OracleTwap, VotePenaltyCounter, AggregateExchangeRatePrevote, AggregateExchangeRateVote, Params, } from "../oracle/oracle";
export const protobufPackage = "seiprotocol.seichain.oracle";
const baseQueryExchangeRateRequest = { denom: "" };
export const QueryExchangeRateRequest = {
    encode(message, writer = Writer.create()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryExchangeRateRequest,
        };
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
    fromJSON(object) {
        const message = {
            ...baseQueryExchangeRateRequest,
        };
        if (object.denom !== undefined && object.denom !== null) {
            message.denom = String(object.denom);
        }
        else {
            message.denom = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.denom !== undefined && (obj.denom = message.denom);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryExchangeRateRequest,
        };
        if (object.denom !== undefined && object.denom !== null) {
            message.denom = object.denom;
        }
        else {
            message.denom = "";
        }
        return message;
    },
};
const baseQueryExchangeRateResponse = {};
export const QueryExchangeRateResponse = {
    encode(message, writer = Writer.create()) {
        if (message.oracleExchangeRate !== undefined) {
            OracleExchangeRate.encode(message.oracleExchangeRate, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryExchangeRateResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.oracleExchangeRate = OracleExchangeRate.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseQueryExchangeRateResponse,
        };
        if (object.oracleExchangeRate !== undefined &&
            object.oracleExchangeRate !== null) {
            message.oracleExchangeRate = OracleExchangeRate.fromJSON(object.oracleExchangeRate);
        }
        else {
            message.oracleExchangeRate = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.oracleExchangeRate !== undefined &&
            (obj.oracleExchangeRate = message.oracleExchangeRate
                ? OracleExchangeRate.toJSON(message.oracleExchangeRate)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryExchangeRateResponse,
        };
        if (object.oracleExchangeRate !== undefined &&
            object.oracleExchangeRate !== null) {
            message.oracleExchangeRate = OracleExchangeRate.fromPartial(object.oracleExchangeRate);
        }
        else {
            message.oracleExchangeRate = undefined;
        }
        return message;
    },
};
const baseQueryExchangeRatesRequest = {};
export const QueryExchangeRatesRequest = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryExchangeRatesRequest,
        };
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
    fromJSON(_) {
        const message = {
            ...baseQueryExchangeRatesRequest,
        };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = {
            ...baseQueryExchangeRatesRequest,
        };
        return message;
    },
};
const baseDenomOracleExchangeRatePair = { denom: "" };
export const DenomOracleExchangeRatePair = {
    encode(message, writer = Writer.create()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        if (message.oracleExchangeRate !== undefined) {
            OracleExchangeRate.encode(message.oracleExchangeRate, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseDenomOracleExchangeRatePair,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.denom = reader.string();
                    break;
                case 2:
                    message.oracleExchangeRate = OracleExchangeRate.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseDenomOracleExchangeRatePair,
        };
        if (object.denom !== undefined && object.denom !== null) {
            message.denom = String(object.denom);
        }
        else {
            message.denom = "";
        }
        if (object.oracleExchangeRate !== undefined &&
            object.oracleExchangeRate !== null) {
            message.oracleExchangeRate = OracleExchangeRate.fromJSON(object.oracleExchangeRate);
        }
        else {
            message.oracleExchangeRate = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.denom !== undefined && (obj.denom = message.denom);
        message.oracleExchangeRate !== undefined &&
            (obj.oracleExchangeRate = message.oracleExchangeRate
                ? OracleExchangeRate.toJSON(message.oracleExchangeRate)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseDenomOracleExchangeRatePair,
        };
        if (object.denom !== undefined && object.denom !== null) {
            message.denom = object.denom;
        }
        else {
            message.denom = "";
        }
        if (object.oracleExchangeRate !== undefined &&
            object.oracleExchangeRate !== null) {
            message.oracleExchangeRate = OracleExchangeRate.fromPartial(object.oracleExchangeRate);
        }
        else {
            message.oracleExchangeRate = undefined;
        }
        return message;
    },
};
const baseQueryExchangeRatesResponse = {};
export const QueryExchangeRatesResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.denomOracleExchangeRatePairs) {
            DenomOracleExchangeRatePair.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryExchangeRatesResponse,
        };
        message.denomOracleExchangeRatePairs = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.denomOracleExchangeRatePairs.push(DenomOracleExchangeRatePair.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseQueryExchangeRatesResponse,
        };
        message.denomOracleExchangeRatePairs = [];
        if (object.denomOracleExchangeRatePairs !== undefined &&
            object.denomOracleExchangeRatePairs !== null) {
            for (const e of object.denomOracleExchangeRatePairs) {
                message.denomOracleExchangeRatePairs.push(DenomOracleExchangeRatePair.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.denomOracleExchangeRatePairs) {
            obj.denomOracleExchangeRatePairs = message.denomOracleExchangeRatePairs.map((e) => (e ? DenomOracleExchangeRatePair.toJSON(e) : undefined));
        }
        else {
            obj.denomOracleExchangeRatePairs = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryExchangeRatesResponse,
        };
        message.denomOracleExchangeRatePairs = [];
        if (object.denomOracleExchangeRatePairs !== undefined &&
            object.denomOracleExchangeRatePairs !== null) {
            for (const e of object.denomOracleExchangeRatePairs) {
                message.denomOracleExchangeRatePairs.push(DenomOracleExchangeRatePair.fromPartial(e));
            }
        }
        return message;
    },
};
const baseQueryActivesRequest = {};
export const QueryActivesRequest = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryActivesRequest };
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
    fromJSON(_) {
        const message = { ...baseQueryActivesRequest };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = { ...baseQueryActivesRequest };
        return message;
    },
};
const baseQueryActivesResponse = { actives: "" };
export const QueryActivesResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.actives) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryActivesResponse };
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
    fromJSON(object) {
        const message = { ...baseQueryActivesResponse };
        message.actives = [];
        if (object.actives !== undefined && object.actives !== null) {
            for (const e of object.actives) {
                message.actives.push(String(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.actives) {
            obj.actives = message.actives.map((e) => e);
        }
        else {
            obj.actives = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryActivesResponse };
        message.actives = [];
        if (object.actives !== undefined && object.actives !== null) {
            for (const e of object.actives) {
                message.actives.push(e);
            }
        }
        return message;
    },
};
const baseQueryVoteTargetsRequest = {};
export const QueryVoteTargetsRequest = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryVoteTargetsRequest,
        };
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
    fromJSON(_) {
        const message = {
            ...baseQueryVoteTargetsRequest,
        };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = {
            ...baseQueryVoteTargetsRequest,
        };
        return message;
    },
};
const baseQueryVoteTargetsResponse = { voteTargets: "" };
export const QueryVoteTargetsResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.voteTargets) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryVoteTargetsResponse,
        };
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
    fromJSON(object) {
        const message = {
            ...baseQueryVoteTargetsResponse,
        };
        message.voteTargets = [];
        if (object.voteTargets !== undefined && object.voteTargets !== null) {
            for (const e of object.voteTargets) {
                message.voteTargets.push(String(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.voteTargets) {
            obj.voteTargets = message.voteTargets.map((e) => e);
        }
        else {
            obj.voteTargets = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryVoteTargetsResponse,
        };
        message.voteTargets = [];
        if (object.voteTargets !== undefined && object.voteTargets !== null) {
            for (const e of object.voteTargets) {
                message.voteTargets.push(e);
            }
        }
        return message;
    },
};
const baseQueryPriceSnapshotHistoryRequest = {};
export const QueryPriceSnapshotHistoryRequest = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryPriceSnapshotHistoryRequest,
        };
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
    fromJSON(_) {
        const message = {
            ...baseQueryPriceSnapshotHistoryRequest,
        };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = {
            ...baseQueryPriceSnapshotHistoryRequest,
        };
        return message;
    },
};
const baseQueryPriceSnapshotHistoryResponse = {};
export const QueryPriceSnapshotHistoryResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.priceSnapshots) {
            PriceSnapshot.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryPriceSnapshotHistoryResponse,
        };
        message.priceSnapshots = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.priceSnapshots.push(PriceSnapshot.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseQueryPriceSnapshotHistoryResponse,
        };
        message.priceSnapshots = [];
        if (object.priceSnapshots !== undefined && object.priceSnapshots !== null) {
            for (const e of object.priceSnapshots) {
                message.priceSnapshots.push(PriceSnapshot.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.priceSnapshots) {
            obj.priceSnapshots = message.priceSnapshots.map((e) => e ? PriceSnapshot.toJSON(e) : undefined);
        }
        else {
            obj.priceSnapshots = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryPriceSnapshotHistoryResponse,
        };
        message.priceSnapshots = [];
        if (object.priceSnapshots !== undefined && object.priceSnapshots !== null) {
            for (const e of object.priceSnapshots) {
                message.priceSnapshots.push(PriceSnapshot.fromPartial(e));
            }
        }
        return message;
    },
};
const baseQueryTwapsRequest = { lookbackSeconds: 0 };
export const QueryTwapsRequest = {
    encode(message, writer = Writer.create()) {
        if (message.lookbackSeconds !== 0) {
            writer.uint32(8).int64(message.lookbackSeconds);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryTwapsRequest };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.lookbackSeconds = longToNumber(reader.int64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseQueryTwapsRequest };
        if (object.lookbackSeconds !== undefined &&
            object.lookbackSeconds !== null) {
            message.lookbackSeconds = Number(object.lookbackSeconds);
        }
        else {
            message.lookbackSeconds = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.lookbackSeconds !== undefined &&
            (obj.lookbackSeconds = message.lookbackSeconds);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryTwapsRequest };
        if (object.lookbackSeconds !== undefined &&
            object.lookbackSeconds !== null) {
            message.lookbackSeconds = object.lookbackSeconds;
        }
        else {
            message.lookbackSeconds = 0;
        }
        return message;
    },
};
const baseQueryTwapsResponse = {};
export const QueryTwapsResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.oracleTwaps) {
            OracleTwap.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryTwapsResponse };
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
    fromJSON(object) {
        const message = { ...baseQueryTwapsResponse };
        message.oracleTwaps = [];
        if (object.oracleTwaps !== undefined && object.oracleTwaps !== null) {
            for (const e of object.oracleTwaps) {
                message.oracleTwaps.push(OracleTwap.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.oracleTwaps) {
            obj.oracleTwaps = message.oracleTwaps.map((e) => e ? OracleTwap.toJSON(e) : undefined);
        }
        else {
            obj.oracleTwaps = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryTwapsResponse };
        message.oracleTwaps = [];
        if (object.oracleTwaps !== undefined && object.oracleTwaps !== null) {
            for (const e of object.oracleTwaps) {
                message.oracleTwaps.push(OracleTwap.fromPartial(e));
            }
        }
        return message;
    },
};
const baseQueryFeederDelegationRequest = { validatorAddr: "" };
export const QueryFeederDelegationRequest = {
    encode(message, writer = Writer.create()) {
        if (message.validatorAddr !== "") {
            writer.uint32(10).string(message.validatorAddr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryFeederDelegationRequest,
        };
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
    fromJSON(object) {
        const message = {
            ...baseQueryFeederDelegationRequest,
        };
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = String(object.validatorAddr);
        }
        else {
            message.validatorAddr = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.validatorAddr !== undefined &&
            (obj.validatorAddr = message.validatorAddr);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryFeederDelegationRequest,
        };
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = object.validatorAddr;
        }
        else {
            message.validatorAddr = "";
        }
        return message;
    },
};
const baseQueryFeederDelegationResponse = { feederAddr: "" };
export const QueryFeederDelegationResponse = {
    encode(message, writer = Writer.create()) {
        if (message.feederAddr !== "") {
            writer.uint32(10).string(message.feederAddr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryFeederDelegationResponse,
        };
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
    fromJSON(object) {
        const message = {
            ...baseQueryFeederDelegationResponse,
        };
        if (object.feederAddr !== undefined && object.feederAddr !== null) {
            message.feederAddr = String(object.feederAddr);
        }
        else {
            message.feederAddr = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.feederAddr !== undefined && (obj.feederAddr = message.feederAddr);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryFeederDelegationResponse,
        };
        if (object.feederAddr !== undefined && object.feederAddr !== null) {
            message.feederAddr = object.feederAddr;
        }
        else {
            message.feederAddr = "";
        }
        return message;
    },
};
const baseQueryVotePenaltyCounterRequest = { validatorAddr: "" };
export const QueryVotePenaltyCounterRequest = {
    encode(message, writer = Writer.create()) {
        if (message.validatorAddr !== "") {
            writer.uint32(10).string(message.validatorAddr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryVotePenaltyCounterRequest,
        };
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
    fromJSON(object) {
        const message = {
            ...baseQueryVotePenaltyCounterRequest,
        };
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = String(object.validatorAddr);
        }
        else {
            message.validatorAddr = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.validatorAddr !== undefined &&
            (obj.validatorAddr = message.validatorAddr);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryVotePenaltyCounterRequest,
        };
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = object.validatorAddr;
        }
        else {
            message.validatorAddr = "";
        }
        return message;
    },
};
const baseQueryVotePenaltyCounterResponse = {};
export const QueryVotePenaltyCounterResponse = {
    encode(message, writer = Writer.create()) {
        if (message.votePenaltyCounter !== undefined) {
            VotePenaltyCounter.encode(message.votePenaltyCounter, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryVotePenaltyCounterResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.votePenaltyCounter = VotePenaltyCounter.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseQueryVotePenaltyCounterResponse,
        };
        if (object.votePenaltyCounter !== undefined &&
            object.votePenaltyCounter !== null) {
            message.votePenaltyCounter = VotePenaltyCounter.fromJSON(object.votePenaltyCounter);
        }
        else {
            message.votePenaltyCounter = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.votePenaltyCounter !== undefined &&
            (obj.votePenaltyCounter = message.votePenaltyCounter
                ? VotePenaltyCounter.toJSON(message.votePenaltyCounter)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryVotePenaltyCounterResponse,
        };
        if (object.votePenaltyCounter !== undefined &&
            object.votePenaltyCounter !== null) {
            message.votePenaltyCounter = VotePenaltyCounter.fromPartial(object.votePenaltyCounter);
        }
        else {
            message.votePenaltyCounter = undefined;
        }
        return message;
    },
};
const baseQueryAggregatePrevoteRequest = { validatorAddr: "" };
export const QueryAggregatePrevoteRequest = {
    encode(message, writer = Writer.create()) {
        if (message.validatorAddr !== "") {
            writer.uint32(10).string(message.validatorAddr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryAggregatePrevoteRequest,
        };
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
    fromJSON(object) {
        const message = {
            ...baseQueryAggregatePrevoteRequest,
        };
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = String(object.validatorAddr);
        }
        else {
            message.validatorAddr = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.validatorAddr !== undefined &&
            (obj.validatorAddr = message.validatorAddr);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryAggregatePrevoteRequest,
        };
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = object.validatorAddr;
        }
        else {
            message.validatorAddr = "";
        }
        return message;
    },
};
const baseQueryAggregatePrevoteResponse = {};
export const QueryAggregatePrevoteResponse = {
    encode(message, writer = Writer.create()) {
        if (message.aggregatePrevote !== undefined) {
            AggregateExchangeRatePrevote.encode(message.aggregatePrevote, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryAggregatePrevoteResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.aggregatePrevote = AggregateExchangeRatePrevote.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseQueryAggregatePrevoteResponse,
        };
        if (object.aggregatePrevote !== undefined &&
            object.aggregatePrevote !== null) {
            message.aggregatePrevote = AggregateExchangeRatePrevote.fromJSON(object.aggregatePrevote);
        }
        else {
            message.aggregatePrevote = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.aggregatePrevote !== undefined &&
            (obj.aggregatePrevote = message.aggregatePrevote
                ? AggregateExchangeRatePrevote.toJSON(message.aggregatePrevote)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryAggregatePrevoteResponse,
        };
        if (object.aggregatePrevote !== undefined &&
            object.aggregatePrevote !== null) {
            message.aggregatePrevote = AggregateExchangeRatePrevote.fromPartial(object.aggregatePrevote);
        }
        else {
            message.aggregatePrevote = undefined;
        }
        return message;
    },
};
const baseQueryAggregatePrevotesRequest = {};
export const QueryAggregatePrevotesRequest = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryAggregatePrevotesRequest,
        };
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
    fromJSON(_) {
        const message = {
            ...baseQueryAggregatePrevotesRequest,
        };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = {
            ...baseQueryAggregatePrevotesRequest,
        };
        return message;
    },
};
const baseQueryAggregatePrevotesResponse = {};
export const QueryAggregatePrevotesResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.aggregatePrevotes) {
            AggregateExchangeRatePrevote.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryAggregatePrevotesResponse,
        };
        message.aggregatePrevotes = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.aggregatePrevotes.push(AggregateExchangeRatePrevote.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseQueryAggregatePrevotesResponse,
        };
        message.aggregatePrevotes = [];
        if (object.aggregatePrevotes !== undefined &&
            object.aggregatePrevotes !== null) {
            for (const e of object.aggregatePrevotes) {
                message.aggregatePrevotes.push(AggregateExchangeRatePrevote.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.aggregatePrevotes) {
            obj.aggregatePrevotes = message.aggregatePrevotes.map((e) => e ? AggregateExchangeRatePrevote.toJSON(e) : undefined);
        }
        else {
            obj.aggregatePrevotes = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryAggregatePrevotesResponse,
        };
        message.aggregatePrevotes = [];
        if (object.aggregatePrevotes !== undefined &&
            object.aggregatePrevotes !== null) {
            for (const e of object.aggregatePrevotes) {
                message.aggregatePrevotes.push(AggregateExchangeRatePrevote.fromPartial(e));
            }
        }
        return message;
    },
};
const baseQueryAggregateVoteRequest = { validatorAddr: "" };
export const QueryAggregateVoteRequest = {
    encode(message, writer = Writer.create()) {
        if (message.validatorAddr !== "") {
            writer.uint32(10).string(message.validatorAddr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryAggregateVoteRequest,
        };
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
    fromJSON(object) {
        const message = {
            ...baseQueryAggregateVoteRequest,
        };
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = String(object.validatorAddr);
        }
        else {
            message.validatorAddr = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.validatorAddr !== undefined &&
            (obj.validatorAddr = message.validatorAddr);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryAggregateVoteRequest,
        };
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = object.validatorAddr;
        }
        else {
            message.validatorAddr = "";
        }
        return message;
    },
};
const baseQueryAggregateVoteResponse = {};
export const QueryAggregateVoteResponse = {
    encode(message, writer = Writer.create()) {
        if (message.aggregateVote !== undefined) {
            AggregateExchangeRateVote.encode(message.aggregateVote, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryAggregateVoteResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.aggregateVote = AggregateExchangeRateVote.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseQueryAggregateVoteResponse,
        };
        if (object.aggregateVote !== undefined && object.aggregateVote !== null) {
            message.aggregateVote = AggregateExchangeRateVote.fromJSON(object.aggregateVote);
        }
        else {
            message.aggregateVote = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.aggregateVote !== undefined &&
            (obj.aggregateVote = message.aggregateVote
                ? AggregateExchangeRateVote.toJSON(message.aggregateVote)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryAggregateVoteResponse,
        };
        if (object.aggregateVote !== undefined && object.aggregateVote !== null) {
            message.aggregateVote = AggregateExchangeRateVote.fromPartial(object.aggregateVote);
        }
        else {
            message.aggregateVote = undefined;
        }
        return message;
    },
};
const baseQueryAggregateVotesRequest = {};
export const QueryAggregateVotesRequest = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryAggregateVotesRequest,
        };
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
    fromJSON(_) {
        const message = {
            ...baseQueryAggregateVotesRequest,
        };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = {
            ...baseQueryAggregateVotesRequest,
        };
        return message;
    },
};
const baseQueryAggregateVotesResponse = {};
export const QueryAggregateVotesResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.aggregateVotes) {
            AggregateExchangeRateVote.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryAggregateVotesResponse,
        };
        message.aggregateVotes = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.aggregateVotes.push(AggregateExchangeRateVote.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseQueryAggregateVotesResponse,
        };
        message.aggregateVotes = [];
        if (object.aggregateVotes !== undefined && object.aggregateVotes !== null) {
            for (const e of object.aggregateVotes) {
                message.aggregateVotes.push(AggregateExchangeRateVote.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.aggregateVotes) {
            obj.aggregateVotes = message.aggregateVotes.map((e) => e ? AggregateExchangeRateVote.toJSON(e) : undefined);
        }
        else {
            obj.aggregateVotes = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryAggregateVotesResponse,
        };
        message.aggregateVotes = [];
        if (object.aggregateVotes !== undefined && object.aggregateVotes !== null) {
            for (const e of object.aggregateVotes) {
                message.aggregateVotes.push(AggregateExchangeRateVote.fromPartial(e));
            }
        }
        return message;
    },
};
const baseQueryParamsRequest = {};
export const QueryParamsRequest = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryParamsRequest };
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
    fromJSON(_) {
        const message = { ...baseQueryParamsRequest };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = { ...baseQueryParamsRequest };
        return message;
    },
};
const baseQueryParamsResponse = {};
export const QueryParamsResponse = {
    encode(message, writer = Writer.create()) {
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryParamsResponse };
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
    fromJSON(object) {
        const message = { ...baseQueryParamsResponse };
        if (object.params !== undefined && object.params !== null) {
            message.params = Params.fromJSON(object.params);
        }
        else {
            message.params = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.params !== undefined &&
            (obj.params = message.params ? Params.toJSON(message.params) : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryParamsResponse };
        if (object.params !== undefined && object.params !== null) {
            message.params = Params.fromPartial(object.params);
        }
        else {
            message.params = undefined;
        }
        return message;
    },
};
export class QueryClientImpl {
    constructor(rpc) {
        this.rpc = rpc;
    }
    ExchangeRate(request) {
        const data = QueryExchangeRateRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Query", "ExchangeRate", data);
        return promise.then((data) => QueryExchangeRateResponse.decode(new Reader(data)));
    }
    ExchangeRates(request) {
        const data = QueryExchangeRatesRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Query", "ExchangeRates", data);
        return promise.then((data) => QueryExchangeRatesResponse.decode(new Reader(data)));
    }
    Actives(request) {
        const data = QueryActivesRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Query", "Actives", data);
        return promise.then((data) => QueryActivesResponse.decode(new Reader(data)));
    }
    VoteTargets(request) {
        const data = QueryVoteTargetsRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Query", "VoteTargets", data);
        return promise.then((data) => QueryVoteTargetsResponse.decode(new Reader(data)));
    }
    PriceSnapshotHistory(request) {
        const data = QueryPriceSnapshotHistoryRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Query", "PriceSnapshotHistory", data);
        return promise.then((data) => QueryPriceSnapshotHistoryResponse.decode(new Reader(data)));
    }
    Twaps(request) {
        const data = QueryTwapsRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Query", "Twaps", data);
        return promise.then((data) => QueryTwapsResponse.decode(new Reader(data)));
    }
    FeederDelegation(request) {
        const data = QueryFeederDelegationRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Query", "FeederDelegation", data);
        return promise.then((data) => QueryFeederDelegationResponse.decode(new Reader(data)));
    }
    VotePenaltyCounter(request) {
        const data = QueryVotePenaltyCounterRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Query", "VotePenaltyCounter", data);
        return promise.then((data) => QueryVotePenaltyCounterResponse.decode(new Reader(data)));
    }
    AggregatePrevote(request) {
        const data = QueryAggregatePrevoteRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Query", "AggregatePrevote", data);
        return promise.then((data) => QueryAggregatePrevoteResponse.decode(new Reader(data)));
    }
    AggregatePrevotes(request) {
        const data = QueryAggregatePrevotesRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Query", "AggregatePrevotes", data);
        return promise.then((data) => QueryAggregatePrevotesResponse.decode(new Reader(data)));
    }
    AggregateVote(request) {
        const data = QueryAggregateVoteRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Query", "AggregateVote", data);
        return promise.then((data) => QueryAggregateVoteResponse.decode(new Reader(data)));
    }
    AggregateVotes(request) {
        const data = QueryAggregateVotesRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Query", "AggregateVotes", data);
        return promise.then((data) => QueryAggregateVotesResponse.decode(new Reader(data)));
    }
    Params(request) {
        const data = QueryParamsRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Query", "Params", data);
        return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
    }
}
var globalThis = (() => {
    if (typeof globalThis !== "undefined")
        return globalThis;
    if (typeof self !== "undefined")
        return self;
    if (typeof window !== "undefined")
        return window;
    if (typeof global !== "undefined")
        return global;
    throw "Unable to locate global object";
})();
function longToNumber(long) {
    if (long.gt(Number.MAX_SAFE_INTEGER)) {
        throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
    }
    return long.toNumber();
}
if (util.Long !== Long) {
    util.Long = Long;
    configure();
}
