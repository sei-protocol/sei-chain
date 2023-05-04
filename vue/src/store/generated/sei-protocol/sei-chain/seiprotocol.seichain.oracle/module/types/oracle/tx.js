/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
export const protobufPackage = "seiprotocol.seichain.oracle";
const baseMsgAggregateExchangeRatePrevote = {
    hash: "",
    feeder: "",
    validator: "",
};
export const MsgAggregateExchangeRatePrevote = {
    encode(message, writer = Writer.create()) {
        if (message.hash !== "") {
            writer.uint32(10).string(message.hash);
        }
        if (message.feeder !== "") {
            writer.uint32(18).string(message.feeder);
        }
        if (message.validator !== "") {
            writer.uint32(26).string(message.validator);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseMsgAggregateExchangeRatePrevote,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.hash = reader.string();
                    break;
                case 2:
                    message.feeder = reader.string();
                    break;
                case 3:
                    message.validator = reader.string();
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
            ...baseMsgAggregateExchangeRatePrevote,
        };
        if (object.hash !== undefined && object.hash !== null) {
            message.hash = String(object.hash);
        }
        else {
            message.hash = "";
        }
        if (object.feeder !== undefined && object.feeder !== null) {
            message.feeder = String(object.feeder);
        }
        else {
            message.feeder = "";
        }
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = String(object.validator);
        }
        else {
            message.validator = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.hash !== undefined && (obj.hash = message.hash);
        message.feeder !== undefined && (obj.feeder = message.feeder);
        message.validator !== undefined && (obj.validator = message.validator);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseMsgAggregateExchangeRatePrevote,
        };
        if (object.hash !== undefined && object.hash !== null) {
            message.hash = object.hash;
        }
        else {
            message.hash = "";
        }
        if (object.feeder !== undefined && object.feeder !== null) {
            message.feeder = object.feeder;
        }
        else {
            message.feeder = "";
        }
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = object.validator;
        }
        else {
            message.validator = "";
        }
        return message;
    },
};
const baseMsgAggregateExchangeRatePrevoteResponse = {};
export const MsgAggregateExchangeRatePrevoteResponse = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseMsgAggregateExchangeRatePrevoteResponse,
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
            ...baseMsgAggregateExchangeRatePrevoteResponse,
        };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = {
            ...baseMsgAggregateExchangeRatePrevoteResponse,
        };
        return message;
    },
};
const baseMsgAggregateExchangeRateVote = {
    salt: "",
    exchangeRates: "",
    feeder: "",
    validator: "",
};
export const MsgAggregateExchangeRateVote = {
    encode(message, writer = Writer.create()) {
        if (message.salt !== "") {
            writer.uint32(10).string(message.salt);
        }
        if (message.exchangeRates !== "") {
            writer.uint32(18).string(message.exchangeRates);
        }
        if (message.feeder !== "") {
            writer.uint32(26).string(message.feeder);
        }
        if (message.validator !== "") {
            writer.uint32(34).string(message.validator);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseMsgAggregateExchangeRateVote,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.salt = reader.string();
                    break;
                case 2:
                    message.exchangeRates = reader.string();
                    break;
                case 3:
                    message.feeder = reader.string();
                    break;
                case 4:
                    message.validator = reader.string();
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
            ...baseMsgAggregateExchangeRateVote,
        };
        if (object.salt !== undefined && object.salt !== null) {
            message.salt = String(object.salt);
        }
        else {
            message.salt = "";
        }
        if (object.exchangeRates !== undefined && object.exchangeRates !== null) {
            message.exchangeRates = String(object.exchangeRates);
        }
        else {
            message.exchangeRates = "";
        }
        if (object.feeder !== undefined && object.feeder !== null) {
            message.feeder = String(object.feeder);
        }
        else {
            message.feeder = "";
        }
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = String(object.validator);
        }
        else {
            message.validator = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.salt !== undefined && (obj.salt = message.salt);
        message.exchangeRates !== undefined &&
            (obj.exchangeRates = message.exchangeRates);
        message.feeder !== undefined && (obj.feeder = message.feeder);
        message.validator !== undefined && (obj.validator = message.validator);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseMsgAggregateExchangeRateVote,
        };
        if (object.salt !== undefined && object.salt !== null) {
            message.salt = object.salt;
        }
        else {
            message.salt = "";
        }
        if (object.exchangeRates !== undefined && object.exchangeRates !== null) {
            message.exchangeRates = object.exchangeRates;
        }
        else {
            message.exchangeRates = "";
        }
        if (object.feeder !== undefined && object.feeder !== null) {
            message.feeder = object.feeder;
        }
        else {
            message.feeder = "";
        }
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = object.validator;
        }
        else {
            message.validator = "";
        }
        return message;
    },
};
const baseMsgAggregateExchangeRateVoteResponse = {};
export const MsgAggregateExchangeRateVoteResponse = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseMsgAggregateExchangeRateVoteResponse,
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
            ...baseMsgAggregateExchangeRateVoteResponse,
        };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = {
            ...baseMsgAggregateExchangeRateVoteResponse,
        };
        return message;
    },
};
const baseMsgAggregateExchangeRateCombinedVote = {
    voteSalt: "",
    voteExchangeRates: "",
    prevoteHash: "",
    feeder: "",
    validator: "",
};
export const MsgAggregateExchangeRateCombinedVote = {
    encode(message, writer = Writer.create()) {
        if (message.voteSalt !== "") {
            writer.uint32(10).string(message.voteSalt);
        }
        if (message.voteExchangeRates !== "") {
            writer.uint32(18).string(message.voteExchangeRates);
        }
        if (message.prevoteHash !== "") {
            writer.uint32(26).string(message.prevoteHash);
        }
        if (message.feeder !== "") {
            writer.uint32(34).string(message.feeder);
        }
        if (message.validator !== "") {
            writer.uint32(42).string(message.validator);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseMsgAggregateExchangeRateCombinedVote,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.voteSalt = reader.string();
                    break;
                case 2:
                    message.voteExchangeRates = reader.string();
                    break;
                case 3:
                    message.prevoteHash = reader.string();
                    break;
                case 4:
                    message.feeder = reader.string();
                    break;
                case 5:
                    message.validator = reader.string();
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
            ...baseMsgAggregateExchangeRateCombinedVote,
        };
        if (object.voteSalt !== undefined && object.voteSalt !== null) {
            message.voteSalt = String(object.voteSalt);
        }
        else {
            message.voteSalt = "";
        }
        if (object.voteExchangeRates !== undefined &&
            object.voteExchangeRates !== null) {
            message.voteExchangeRates = String(object.voteExchangeRates);
        }
        else {
            message.voteExchangeRates = "";
        }
        if (object.prevoteHash !== undefined && object.prevoteHash !== null) {
            message.prevoteHash = String(object.prevoteHash);
        }
        else {
            message.prevoteHash = "";
        }
        if (object.feeder !== undefined && object.feeder !== null) {
            message.feeder = String(object.feeder);
        }
        else {
            message.feeder = "";
        }
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = String(object.validator);
        }
        else {
            message.validator = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.voteSalt !== undefined && (obj.voteSalt = message.voteSalt);
        message.voteExchangeRates !== undefined &&
            (obj.voteExchangeRates = message.voteExchangeRates);
        message.prevoteHash !== undefined &&
            (obj.prevoteHash = message.prevoteHash);
        message.feeder !== undefined && (obj.feeder = message.feeder);
        message.validator !== undefined && (obj.validator = message.validator);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseMsgAggregateExchangeRateCombinedVote,
        };
        if (object.voteSalt !== undefined && object.voteSalt !== null) {
            message.voteSalt = object.voteSalt;
        }
        else {
            message.voteSalt = "";
        }
        if (object.voteExchangeRates !== undefined &&
            object.voteExchangeRates !== null) {
            message.voteExchangeRates = object.voteExchangeRates;
        }
        else {
            message.voteExchangeRates = "";
        }
        if (object.prevoteHash !== undefined && object.prevoteHash !== null) {
            message.prevoteHash = object.prevoteHash;
        }
        else {
            message.prevoteHash = "";
        }
        if (object.feeder !== undefined && object.feeder !== null) {
            message.feeder = object.feeder;
        }
        else {
            message.feeder = "";
        }
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = object.validator;
        }
        else {
            message.validator = "";
        }
        return message;
    },
};
const baseMsgAggregateExchangeRateCombinedVoteResponse = {};
export const MsgAggregateExchangeRateCombinedVoteResponse = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseMsgAggregateExchangeRateCombinedVoteResponse,
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
            ...baseMsgAggregateExchangeRateCombinedVoteResponse,
        };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = {
            ...baseMsgAggregateExchangeRateCombinedVoteResponse,
        };
        return message;
    },
};
const baseMsgDelegateFeedConsent = { operator: "", delegate: "" };
export const MsgDelegateFeedConsent = {
    encode(message, writer = Writer.create()) {
        if (message.operator !== "") {
            writer.uint32(10).string(message.operator);
        }
        if (message.delegate !== "") {
            writer.uint32(18).string(message.delegate);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgDelegateFeedConsent };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.operator = reader.string();
                    break;
                case 2:
                    message.delegate = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgDelegateFeedConsent };
        if (object.operator !== undefined && object.operator !== null) {
            message.operator = String(object.operator);
        }
        else {
            message.operator = "";
        }
        if (object.delegate !== undefined && object.delegate !== null) {
            message.delegate = String(object.delegate);
        }
        else {
            message.delegate = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.operator !== undefined && (obj.operator = message.operator);
        message.delegate !== undefined && (obj.delegate = message.delegate);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgDelegateFeedConsent };
        if (object.operator !== undefined && object.operator !== null) {
            message.operator = object.operator;
        }
        else {
            message.operator = "";
        }
        if (object.delegate !== undefined && object.delegate !== null) {
            message.delegate = object.delegate;
        }
        else {
            message.delegate = "";
        }
        return message;
    },
};
const baseMsgDelegateFeedConsentResponse = {};
export const MsgDelegateFeedConsentResponse = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseMsgDelegateFeedConsentResponse,
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
            ...baseMsgDelegateFeedConsentResponse,
        };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = {
            ...baseMsgDelegateFeedConsentResponse,
        };
        return message;
    },
};
export class MsgClientImpl {
    constructor(rpc) {
        this.rpc = rpc;
    }
    AggregateExchangeRatePrevote(request) {
        const data = MsgAggregateExchangeRatePrevote.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Msg", "AggregateExchangeRatePrevote", data);
        return promise.then((data) => MsgAggregateExchangeRatePrevoteResponse.decode(new Reader(data)));
    }
    AggregateExchangeRateVote(request) {
        const data = MsgAggregateExchangeRateVote.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Msg", "AggregateExchangeRateVote", data);
        return promise.then((data) => MsgAggregateExchangeRateVoteResponse.decode(new Reader(data)));
    }
    AggregateExchangeRateCombinedVote(request) {
        const data = MsgAggregateExchangeRateCombinedVote.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Msg", "AggregateExchangeRateCombinedVote", data);
        return promise.then((data) => MsgAggregateExchangeRateCombinedVoteResponse.decode(new Reader(data)));
    }
    DelegateFeedConsent(request) {
        const data = MsgDelegateFeedConsent.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.oracle.Msg", "DelegateFeedConsent", data);
        return promise.then((data) => MsgDelegateFeedConsentResponse.decode(new Reader(data)));
    }
}
