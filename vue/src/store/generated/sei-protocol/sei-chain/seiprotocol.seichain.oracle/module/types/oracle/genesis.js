/* eslint-disable */
import { Params, ExchangeRateTuple, AggregateExchangeRatePrevote, AggregateExchangeRateVote, VotePenaltyCounter, } from "../oracle/oracle";
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "seiprotocol.seichain.oracle";
const baseGenesisState = {};
export const GenesisState = {
    encode(message, writer = Writer.create()) {
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(10).fork()).ldelim();
        }
        for (const v of message.feederDelegations) {
            FeederDelegation.encode(v, writer.uint32(18).fork()).ldelim();
        }
        for (const v of message.exchangeRates) {
            ExchangeRateTuple.encode(v, writer.uint32(26).fork()).ldelim();
        }
        for (const v of message.penaltyCounters) {
            PenaltyCounter.encode(v, writer.uint32(34).fork()).ldelim();
        }
        for (const v of message.aggregateExchangeRatePrevotes) {
            AggregateExchangeRatePrevote.encode(v, writer.uint32(42).fork()).ldelim();
        }
        for (const v of message.aggregateExchangeRateVotes) {
            AggregateExchangeRateVote.encode(v, writer.uint32(50).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseGenesisState };
        message.feederDelegations = [];
        message.exchangeRates = [];
        message.penaltyCounters = [];
        message.aggregateExchangeRatePrevotes = [];
        message.aggregateExchangeRateVotes = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.params = Params.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.feederDelegations.push(FeederDelegation.decode(reader, reader.uint32()));
                    break;
                case 3:
                    message.exchangeRates.push(ExchangeRateTuple.decode(reader, reader.uint32()));
                    break;
                case 4:
                    message.penaltyCounters.push(PenaltyCounter.decode(reader, reader.uint32()));
                    break;
                case 5:
                    message.aggregateExchangeRatePrevotes.push(AggregateExchangeRatePrevote.decode(reader, reader.uint32()));
                    break;
                case 6:
                    message.aggregateExchangeRateVotes.push(AggregateExchangeRateVote.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseGenesisState };
        message.feederDelegations = [];
        message.exchangeRates = [];
        message.penaltyCounters = [];
        message.aggregateExchangeRatePrevotes = [];
        message.aggregateExchangeRateVotes = [];
        if (object.params !== undefined && object.params !== null) {
            message.params = Params.fromJSON(object.params);
        }
        else {
            message.params = undefined;
        }
        if (object.feederDelegations !== undefined &&
            object.feederDelegations !== null) {
            for (const e of object.feederDelegations) {
                message.feederDelegations.push(FeederDelegation.fromJSON(e));
            }
        }
        if (object.exchangeRates !== undefined && object.exchangeRates !== null) {
            for (const e of object.exchangeRates) {
                message.exchangeRates.push(ExchangeRateTuple.fromJSON(e));
            }
        }
        if (object.penaltyCounters !== undefined &&
            object.penaltyCounters !== null) {
            for (const e of object.penaltyCounters) {
                message.penaltyCounters.push(PenaltyCounter.fromJSON(e));
            }
        }
        if (object.aggregateExchangeRatePrevotes !== undefined &&
            object.aggregateExchangeRatePrevotes !== null) {
            for (const e of object.aggregateExchangeRatePrevotes) {
                message.aggregateExchangeRatePrevotes.push(AggregateExchangeRatePrevote.fromJSON(e));
            }
        }
        if (object.aggregateExchangeRateVotes !== undefined &&
            object.aggregateExchangeRateVotes !== null) {
            for (const e of object.aggregateExchangeRateVotes) {
                message.aggregateExchangeRateVotes.push(AggregateExchangeRateVote.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.params !== undefined &&
            (obj.params = message.params ? Params.toJSON(message.params) : undefined);
        if (message.feederDelegations) {
            obj.feederDelegations = message.feederDelegations.map((e) => e ? FeederDelegation.toJSON(e) : undefined);
        }
        else {
            obj.feederDelegations = [];
        }
        if (message.exchangeRates) {
            obj.exchangeRates = message.exchangeRates.map((e) => e ? ExchangeRateTuple.toJSON(e) : undefined);
        }
        else {
            obj.exchangeRates = [];
        }
        if (message.penaltyCounters) {
            obj.penaltyCounters = message.penaltyCounters.map((e) => e ? PenaltyCounter.toJSON(e) : undefined);
        }
        else {
            obj.penaltyCounters = [];
        }
        if (message.aggregateExchangeRatePrevotes) {
            obj.aggregateExchangeRatePrevotes = message.aggregateExchangeRatePrevotes.map((e) => (e ? AggregateExchangeRatePrevote.toJSON(e) : undefined));
        }
        else {
            obj.aggregateExchangeRatePrevotes = [];
        }
        if (message.aggregateExchangeRateVotes) {
            obj.aggregateExchangeRateVotes = message.aggregateExchangeRateVotes.map((e) => (e ? AggregateExchangeRateVote.toJSON(e) : undefined));
        }
        else {
            obj.aggregateExchangeRateVotes = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseGenesisState };
        message.feederDelegations = [];
        message.exchangeRates = [];
        message.penaltyCounters = [];
        message.aggregateExchangeRatePrevotes = [];
        message.aggregateExchangeRateVotes = [];
        if (object.params !== undefined && object.params !== null) {
            message.params = Params.fromPartial(object.params);
        }
        else {
            message.params = undefined;
        }
        if (object.feederDelegations !== undefined &&
            object.feederDelegations !== null) {
            for (const e of object.feederDelegations) {
                message.feederDelegations.push(FeederDelegation.fromPartial(e));
            }
        }
        if (object.exchangeRates !== undefined && object.exchangeRates !== null) {
            for (const e of object.exchangeRates) {
                message.exchangeRates.push(ExchangeRateTuple.fromPartial(e));
            }
        }
        if (object.penaltyCounters !== undefined &&
            object.penaltyCounters !== null) {
            for (const e of object.penaltyCounters) {
                message.penaltyCounters.push(PenaltyCounter.fromPartial(e));
            }
        }
        if (object.aggregateExchangeRatePrevotes !== undefined &&
            object.aggregateExchangeRatePrevotes !== null) {
            for (const e of object.aggregateExchangeRatePrevotes) {
                message.aggregateExchangeRatePrevotes.push(AggregateExchangeRatePrevote.fromPartial(e));
            }
        }
        if (object.aggregateExchangeRateVotes !== undefined &&
            object.aggregateExchangeRateVotes !== null) {
            for (const e of object.aggregateExchangeRateVotes) {
                message.aggregateExchangeRateVotes.push(AggregateExchangeRateVote.fromPartial(e));
            }
        }
        return message;
    },
};
const baseFeederDelegation = {
    feederAddress: "",
    validatorAddress: "",
};
export const FeederDelegation = {
    encode(message, writer = Writer.create()) {
        if (message.feederAddress !== "") {
            writer.uint32(10).string(message.feederAddress);
        }
        if (message.validatorAddress !== "") {
            writer.uint32(18).string(message.validatorAddress);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseFeederDelegation };
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
    fromJSON(object) {
        const message = { ...baseFeederDelegation };
        if (object.feederAddress !== undefined && object.feederAddress !== null) {
            message.feederAddress = String(object.feederAddress);
        }
        else {
            message.feederAddress = "";
        }
        if (object.validatorAddress !== undefined &&
            object.validatorAddress !== null) {
            message.validatorAddress = String(object.validatorAddress);
        }
        else {
            message.validatorAddress = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.feederAddress !== undefined &&
            (obj.feederAddress = message.feederAddress);
        message.validatorAddress !== undefined &&
            (obj.validatorAddress = message.validatorAddress);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseFeederDelegation };
        if (object.feederAddress !== undefined && object.feederAddress !== null) {
            message.feederAddress = object.feederAddress;
        }
        else {
            message.feederAddress = "";
        }
        if (object.validatorAddress !== undefined &&
            object.validatorAddress !== null) {
            message.validatorAddress = object.validatorAddress;
        }
        else {
            message.validatorAddress = "";
        }
        return message;
    },
};
const basePenaltyCounter = { validatorAddress: "" };
export const PenaltyCounter = {
    encode(message, writer = Writer.create()) {
        if (message.validatorAddress !== "") {
            writer.uint32(10).string(message.validatorAddress);
        }
        if (message.votePenaltyCounter !== undefined) {
            VotePenaltyCounter.encode(message.votePenaltyCounter, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...basePenaltyCounter };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.validatorAddress = reader.string();
                    break;
                case 2:
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
        const message = { ...basePenaltyCounter };
        if (object.validatorAddress !== undefined &&
            object.validatorAddress !== null) {
            message.validatorAddress = String(object.validatorAddress);
        }
        else {
            message.validatorAddress = "";
        }
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
        message.validatorAddress !== undefined &&
            (obj.validatorAddress = message.validatorAddress);
        message.votePenaltyCounter !== undefined &&
            (obj.votePenaltyCounter = message.votePenaltyCounter
                ? VotePenaltyCounter.toJSON(message.votePenaltyCounter)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...basePenaltyCounter };
        if (object.validatorAddress !== undefined &&
            object.validatorAddress !== null) {
            message.validatorAddress = object.validatorAddress;
        }
        else {
            message.validatorAddress = "";
        }
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
