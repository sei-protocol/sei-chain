/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import { PageRequest, PageResponse, } from "../../../cosmos/base/query/v1beta1/pagination";
import { Validator, DelegationResponse, UnbondingDelegation, RedelegationResponse, HistoricalInfo, Pool, Params, } from "../../../cosmos/staking/v1beta1/staking";
export const protobufPackage = "cosmos.staking.v1beta1";
const baseQueryValidatorsRequest = { status: "" };
export const QueryValidatorsRequest = {
    encode(message, writer = Writer.create()) {
        if (message.status !== "") {
            writer.uint32(10).string(message.status);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryValidatorsRequest };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.status = reader.string();
                    break;
                case 2:
                    message.pagination = PageRequest.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseQueryValidatorsRequest };
        if (object.status !== undefined && object.status !== null) {
            message.status = String(object.status);
        }
        else {
            message.status = "";
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromJSON(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.status !== undefined && (obj.status = message.status);
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageRequest.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryValidatorsRequest };
        if (object.status !== undefined && object.status !== null) {
            message.status = object.status;
        }
        else {
            message.status = "";
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
};
const baseQueryValidatorsResponse = {};
export const QueryValidatorsResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.validators) {
            Validator.encode(v, writer.uint32(10).fork()).ldelim();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryValidatorsResponse,
        };
        message.validators = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.validators.push(Validator.decode(reader, reader.uint32()));
                    break;
                case 2:
                    message.pagination = PageResponse.decode(reader, reader.uint32());
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
            ...baseQueryValidatorsResponse,
        };
        message.validators = [];
        if (object.validators !== undefined && object.validators !== null) {
            for (const e of object.validators) {
                message.validators.push(Validator.fromJSON(e));
            }
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageResponse.fromJSON(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.validators) {
            obj.validators = message.validators.map((e) => e ? Validator.toJSON(e) : undefined);
        }
        else {
            obj.validators = [];
        }
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageResponse.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryValidatorsResponse,
        };
        message.validators = [];
        if (object.validators !== undefined && object.validators !== null) {
            for (const e of object.validators) {
                message.validators.push(Validator.fromPartial(e));
            }
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageResponse.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
};
const baseQueryValidatorRequest = { validatorAddr: "" };
export const QueryValidatorRequest = {
    encode(message, writer = Writer.create()) {
        if (message.validatorAddr !== "") {
            writer.uint32(10).string(message.validatorAddr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryValidatorRequest };
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
        const message = { ...baseQueryValidatorRequest };
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
        const message = { ...baseQueryValidatorRequest };
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = object.validatorAddr;
        }
        else {
            message.validatorAddr = "";
        }
        return message;
    },
};
const baseQueryValidatorResponse = {};
export const QueryValidatorResponse = {
    encode(message, writer = Writer.create()) {
        if (message.validator !== undefined) {
            Validator.encode(message.validator, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryValidatorResponse };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.validator = Validator.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseQueryValidatorResponse };
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = Validator.fromJSON(object.validator);
        }
        else {
            message.validator = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.validator !== undefined &&
            (obj.validator = message.validator
                ? Validator.toJSON(message.validator)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryValidatorResponse };
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = Validator.fromPartial(object.validator);
        }
        else {
            message.validator = undefined;
        }
        return message;
    },
};
const baseQueryValidatorDelegationsRequest = { validatorAddr: "" };
export const QueryValidatorDelegationsRequest = {
    encode(message, writer = Writer.create()) {
        if (message.validatorAddr !== "") {
            writer.uint32(10).string(message.validatorAddr);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryValidatorDelegationsRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.validatorAddr = reader.string();
                    break;
                case 2:
                    message.pagination = PageRequest.decode(reader, reader.uint32());
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
            ...baseQueryValidatorDelegationsRequest,
        };
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = String(object.validatorAddr);
        }
        else {
            message.validatorAddr = "";
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromJSON(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.validatorAddr !== undefined &&
            (obj.validatorAddr = message.validatorAddr);
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageRequest.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryValidatorDelegationsRequest,
        };
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = object.validatorAddr;
        }
        else {
            message.validatorAddr = "";
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
};
const baseQueryValidatorDelegationsResponse = {};
export const QueryValidatorDelegationsResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.delegationResponses) {
            DelegationResponse.encode(v, writer.uint32(10).fork()).ldelim();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryValidatorDelegationsResponse,
        };
        message.delegationResponses = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.delegationResponses.push(DelegationResponse.decode(reader, reader.uint32()));
                    break;
                case 2:
                    message.pagination = PageResponse.decode(reader, reader.uint32());
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
            ...baseQueryValidatorDelegationsResponse,
        };
        message.delegationResponses = [];
        if (object.delegationResponses !== undefined &&
            object.delegationResponses !== null) {
            for (const e of object.delegationResponses) {
                message.delegationResponses.push(DelegationResponse.fromJSON(e));
            }
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageResponse.fromJSON(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.delegationResponses) {
            obj.delegationResponses = message.delegationResponses.map((e) => e ? DelegationResponse.toJSON(e) : undefined);
        }
        else {
            obj.delegationResponses = [];
        }
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageResponse.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryValidatorDelegationsResponse,
        };
        message.delegationResponses = [];
        if (object.delegationResponses !== undefined &&
            object.delegationResponses !== null) {
            for (const e of object.delegationResponses) {
                message.delegationResponses.push(DelegationResponse.fromPartial(e));
            }
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageResponse.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
};
const baseQueryValidatorUnbondingDelegationsRequest = {
    validatorAddr: "",
};
export const QueryValidatorUnbondingDelegationsRequest = {
    encode(message, writer = Writer.create()) {
        if (message.validatorAddr !== "") {
            writer.uint32(10).string(message.validatorAddr);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryValidatorUnbondingDelegationsRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.validatorAddr = reader.string();
                    break;
                case 2:
                    message.pagination = PageRequest.decode(reader, reader.uint32());
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
            ...baseQueryValidatorUnbondingDelegationsRequest,
        };
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = String(object.validatorAddr);
        }
        else {
            message.validatorAddr = "";
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromJSON(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.validatorAddr !== undefined &&
            (obj.validatorAddr = message.validatorAddr);
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageRequest.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryValidatorUnbondingDelegationsRequest,
        };
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = object.validatorAddr;
        }
        else {
            message.validatorAddr = "";
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
};
const baseQueryValidatorUnbondingDelegationsResponse = {};
export const QueryValidatorUnbondingDelegationsResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.unbondingResponses) {
            UnbondingDelegation.encode(v, writer.uint32(10).fork()).ldelim();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryValidatorUnbondingDelegationsResponse,
        };
        message.unbondingResponses = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.unbondingResponses.push(UnbondingDelegation.decode(reader, reader.uint32()));
                    break;
                case 2:
                    message.pagination = PageResponse.decode(reader, reader.uint32());
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
            ...baseQueryValidatorUnbondingDelegationsResponse,
        };
        message.unbondingResponses = [];
        if (object.unbondingResponses !== undefined &&
            object.unbondingResponses !== null) {
            for (const e of object.unbondingResponses) {
                message.unbondingResponses.push(UnbondingDelegation.fromJSON(e));
            }
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageResponse.fromJSON(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.unbondingResponses) {
            obj.unbondingResponses = message.unbondingResponses.map((e) => e ? UnbondingDelegation.toJSON(e) : undefined);
        }
        else {
            obj.unbondingResponses = [];
        }
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageResponse.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryValidatorUnbondingDelegationsResponse,
        };
        message.unbondingResponses = [];
        if (object.unbondingResponses !== undefined &&
            object.unbondingResponses !== null) {
            for (const e of object.unbondingResponses) {
                message.unbondingResponses.push(UnbondingDelegation.fromPartial(e));
            }
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageResponse.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
};
const baseQueryDelegationRequest = {
    delegatorAddr: "",
    validatorAddr: "",
};
export const QueryDelegationRequest = {
    encode(message, writer = Writer.create()) {
        if (message.delegatorAddr !== "") {
            writer.uint32(10).string(message.delegatorAddr);
        }
        if (message.validatorAddr !== "") {
            writer.uint32(18).string(message.validatorAddr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryDelegationRequest };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.delegatorAddr = reader.string();
                    break;
                case 2:
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
        const message = { ...baseQueryDelegationRequest };
        if (object.delegatorAddr !== undefined && object.delegatorAddr !== null) {
            message.delegatorAddr = String(object.delegatorAddr);
        }
        else {
            message.delegatorAddr = "";
        }
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
        message.delegatorAddr !== undefined &&
            (obj.delegatorAddr = message.delegatorAddr);
        message.validatorAddr !== undefined &&
            (obj.validatorAddr = message.validatorAddr);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryDelegationRequest };
        if (object.delegatorAddr !== undefined && object.delegatorAddr !== null) {
            message.delegatorAddr = object.delegatorAddr;
        }
        else {
            message.delegatorAddr = "";
        }
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = object.validatorAddr;
        }
        else {
            message.validatorAddr = "";
        }
        return message;
    },
};
const baseQueryDelegationResponse = {};
export const QueryDelegationResponse = {
    encode(message, writer = Writer.create()) {
        if (message.delegationResponse !== undefined) {
            DelegationResponse.encode(message.delegationResponse, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryDelegationResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.delegationResponse = DelegationResponse.decode(reader, reader.uint32());
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
            ...baseQueryDelegationResponse,
        };
        if (object.delegationResponse !== undefined &&
            object.delegationResponse !== null) {
            message.delegationResponse = DelegationResponse.fromJSON(object.delegationResponse);
        }
        else {
            message.delegationResponse = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.delegationResponse !== undefined &&
            (obj.delegationResponse = message.delegationResponse
                ? DelegationResponse.toJSON(message.delegationResponse)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryDelegationResponse,
        };
        if (object.delegationResponse !== undefined &&
            object.delegationResponse !== null) {
            message.delegationResponse = DelegationResponse.fromPartial(object.delegationResponse);
        }
        else {
            message.delegationResponse = undefined;
        }
        return message;
    },
};
const baseQueryUnbondingDelegationRequest = {
    delegatorAddr: "",
    validatorAddr: "",
};
export const QueryUnbondingDelegationRequest = {
    encode(message, writer = Writer.create()) {
        if (message.delegatorAddr !== "") {
            writer.uint32(10).string(message.delegatorAddr);
        }
        if (message.validatorAddr !== "") {
            writer.uint32(18).string(message.validatorAddr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryUnbondingDelegationRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.delegatorAddr = reader.string();
                    break;
                case 2:
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
            ...baseQueryUnbondingDelegationRequest,
        };
        if (object.delegatorAddr !== undefined && object.delegatorAddr !== null) {
            message.delegatorAddr = String(object.delegatorAddr);
        }
        else {
            message.delegatorAddr = "";
        }
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
        message.delegatorAddr !== undefined &&
            (obj.delegatorAddr = message.delegatorAddr);
        message.validatorAddr !== undefined &&
            (obj.validatorAddr = message.validatorAddr);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryUnbondingDelegationRequest,
        };
        if (object.delegatorAddr !== undefined && object.delegatorAddr !== null) {
            message.delegatorAddr = object.delegatorAddr;
        }
        else {
            message.delegatorAddr = "";
        }
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = object.validatorAddr;
        }
        else {
            message.validatorAddr = "";
        }
        return message;
    },
};
const baseQueryUnbondingDelegationResponse = {};
export const QueryUnbondingDelegationResponse = {
    encode(message, writer = Writer.create()) {
        if (message.unbond !== undefined) {
            UnbondingDelegation.encode(message.unbond, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryUnbondingDelegationResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.unbond = UnbondingDelegation.decode(reader, reader.uint32());
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
            ...baseQueryUnbondingDelegationResponse,
        };
        if (object.unbond !== undefined && object.unbond !== null) {
            message.unbond = UnbondingDelegation.fromJSON(object.unbond);
        }
        else {
            message.unbond = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.unbond !== undefined &&
            (obj.unbond = message.unbond
                ? UnbondingDelegation.toJSON(message.unbond)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryUnbondingDelegationResponse,
        };
        if (object.unbond !== undefined && object.unbond !== null) {
            message.unbond = UnbondingDelegation.fromPartial(object.unbond);
        }
        else {
            message.unbond = undefined;
        }
        return message;
    },
};
const baseQueryDelegatorDelegationsRequest = { delegatorAddr: "" };
export const QueryDelegatorDelegationsRequest = {
    encode(message, writer = Writer.create()) {
        if (message.delegatorAddr !== "") {
            writer.uint32(10).string(message.delegatorAddr);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryDelegatorDelegationsRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.delegatorAddr = reader.string();
                    break;
                case 2:
                    message.pagination = PageRequest.decode(reader, reader.uint32());
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
            ...baseQueryDelegatorDelegationsRequest,
        };
        if (object.delegatorAddr !== undefined && object.delegatorAddr !== null) {
            message.delegatorAddr = String(object.delegatorAddr);
        }
        else {
            message.delegatorAddr = "";
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromJSON(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.delegatorAddr !== undefined &&
            (obj.delegatorAddr = message.delegatorAddr);
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageRequest.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryDelegatorDelegationsRequest,
        };
        if (object.delegatorAddr !== undefined && object.delegatorAddr !== null) {
            message.delegatorAddr = object.delegatorAddr;
        }
        else {
            message.delegatorAddr = "";
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
};
const baseQueryDelegatorDelegationsResponse = {};
export const QueryDelegatorDelegationsResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.delegationResponses) {
            DelegationResponse.encode(v, writer.uint32(10).fork()).ldelim();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryDelegatorDelegationsResponse,
        };
        message.delegationResponses = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.delegationResponses.push(DelegationResponse.decode(reader, reader.uint32()));
                    break;
                case 2:
                    message.pagination = PageResponse.decode(reader, reader.uint32());
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
            ...baseQueryDelegatorDelegationsResponse,
        };
        message.delegationResponses = [];
        if (object.delegationResponses !== undefined &&
            object.delegationResponses !== null) {
            for (const e of object.delegationResponses) {
                message.delegationResponses.push(DelegationResponse.fromJSON(e));
            }
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageResponse.fromJSON(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.delegationResponses) {
            obj.delegationResponses = message.delegationResponses.map((e) => e ? DelegationResponse.toJSON(e) : undefined);
        }
        else {
            obj.delegationResponses = [];
        }
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageResponse.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryDelegatorDelegationsResponse,
        };
        message.delegationResponses = [];
        if (object.delegationResponses !== undefined &&
            object.delegationResponses !== null) {
            for (const e of object.delegationResponses) {
                message.delegationResponses.push(DelegationResponse.fromPartial(e));
            }
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageResponse.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
};
const baseQueryDelegatorUnbondingDelegationsRequest = {
    delegatorAddr: "",
};
export const QueryDelegatorUnbondingDelegationsRequest = {
    encode(message, writer = Writer.create()) {
        if (message.delegatorAddr !== "") {
            writer.uint32(10).string(message.delegatorAddr);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryDelegatorUnbondingDelegationsRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.delegatorAddr = reader.string();
                    break;
                case 2:
                    message.pagination = PageRequest.decode(reader, reader.uint32());
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
            ...baseQueryDelegatorUnbondingDelegationsRequest,
        };
        if (object.delegatorAddr !== undefined && object.delegatorAddr !== null) {
            message.delegatorAddr = String(object.delegatorAddr);
        }
        else {
            message.delegatorAddr = "";
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromJSON(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.delegatorAddr !== undefined &&
            (obj.delegatorAddr = message.delegatorAddr);
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageRequest.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryDelegatorUnbondingDelegationsRequest,
        };
        if (object.delegatorAddr !== undefined && object.delegatorAddr !== null) {
            message.delegatorAddr = object.delegatorAddr;
        }
        else {
            message.delegatorAddr = "";
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
};
const baseQueryDelegatorUnbondingDelegationsResponse = {};
export const QueryDelegatorUnbondingDelegationsResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.unbondingResponses) {
            UnbondingDelegation.encode(v, writer.uint32(10).fork()).ldelim();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryDelegatorUnbondingDelegationsResponse,
        };
        message.unbondingResponses = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.unbondingResponses.push(UnbondingDelegation.decode(reader, reader.uint32()));
                    break;
                case 2:
                    message.pagination = PageResponse.decode(reader, reader.uint32());
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
            ...baseQueryDelegatorUnbondingDelegationsResponse,
        };
        message.unbondingResponses = [];
        if (object.unbondingResponses !== undefined &&
            object.unbondingResponses !== null) {
            for (const e of object.unbondingResponses) {
                message.unbondingResponses.push(UnbondingDelegation.fromJSON(e));
            }
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageResponse.fromJSON(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.unbondingResponses) {
            obj.unbondingResponses = message.unbondingResponses.map((e) => e ? UnbondingDelegation.toJSON(e) : undefined);
        }
        else {
            obj.unbondingResponses = [];
        }
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageResponse.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryDelegatorUnbondingDelegationsResponse,
        };
        message.unbondingResponses = [];
        if (object.unbondingResponses !== undefined &&
            object.unbondingResponses !== null) {
            for (const e of object.unbondingResponses) {
                message.unbondingResponses.push(UnbondingDelegation.fromPartial(e));
            }
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageResponse.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
};
const baseQueryRedelegationsRequest = {
    delegatorAddr: "",
    srcValidatorAddr: "",
    dstValidatorAddr: "",
};
export const QueryRedelegationsRequest = {
    encode(message, writer = Writer.create()) {
        if (message.delegatorAddr !== "") {
            writer.uint32(10).string(message.delegatorAddr);
        }
        if (message.srcValidatorAddr !== "") {
            writer.uint32(18).string(message.srcValidatorAddr);
        }
        if (message.dstValidatorAddr !== "") {
            writer.uint32(26).string(message.dstValidatorAddr);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(34).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryRedelegationsRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.delegatorAddr = reader.string();
                    break;
                case 2:
                    message.srcValidatorAddr = reader.string();
                    break;
                case 3:
                    message.dstValidatorAddr = reader.string();
                    break;
                case 4:
                    message.pagination = PageRequest.decode(reader, reader.uint32());
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
            ...baseQueryRedelegationsRequest,
        };
        if (object.delegatorAddr !== undefined && object.delegatorAddr !== null) {
            message.delegatorAddr = String(object.delegatorAddr);
        }
        else {
            message.delegatorAddr = "";
        }
        if (object.srcValidatorAddr !== undefined &&
            object.srcValidatorAddr !== null) {
            message.srcValidatorAddr = String(object.srcValidatorAddr);
        }
        else {
            message.srcValidatorAddr = "";
        }
        if (object.dstValidatorAddr !== undefined &&
            object.dstValidatorAddr !== null) {
            message.dstValidatorAddr = String(object.dstValidatorAddr);
        }
        else {
            message.dstValidatorAddr = "";
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromJSON(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.delegatorAddr !== undefined &&
            (obj.delegatorAddr = message.delegatorAddr);
        message.srcValidatorAddr !== undefined &&
            (obj.srcValidatorAddr = message.srcValidatorAddr);
        message.dstValidatorAddr !== undefined &&
            (obj.dstValidatorAddr = message.dstValidatorAddr);
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageRequest.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryRedelegationsRequest,
        };
        if (object.delegatorAddr !== undefined && object.delegatorAddr !== null) {
            message.delegatorAddr = object.delegatorAddr;
        }
        else {
            message.delegatorAddr = "";
        }
        if (object.srcValidatorAddr !== undefined &&
            object.srcValidatorAddr !== null) {
            message.srcValidatorAddr = object.srcValidatorAddr;
        }
        else {
            message.srcValidatorAddr = "";
        }
        if (object.dstValidatorAddr !== undefined &&
            object.dstValidatorAddr !== null) {
            message.dstValidatorAddr = object.dstValidatorAddr;
        }
        else {
            message.dstValidatorAddr = "";
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
};
const baseQueryRedelegationsResponse = {};
export const QueryRedelegationsResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.redelegationResponses) {
            RedelegationResponse.encode(v, writer.uint32(10).fork()).ldelim();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryRedelegationsResponse,
        };
        message.redelegationResponses = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.redelegationResponses.push(RedelegationResponse.decode(reader, reader.uint32()));
                    break;
                case 2:
                    message.pagination = PageResponse.decode(reader, reader.uint32());
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
            ...baseQueryRedelegationsResponse,
        };
        message.redelegationResponses = [];
        if (object.redelegationResponses !== undefined &&
            object.redelegationResponses !== null) {
            for (const e of object.redelegationResponses) {
                message.redelegationResponses.push(RedelegationResponse.fromJSON(e));
            }
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageResponse.fromJSON(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.redelegationResponses) {
            obj.redelegationResponses = message.redelegationResponses.map((e) => e ? RedelegationResponse.toJSON(e) : undefined);
        }
        else {
            obj.redelegationResponses = [];
        }
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageResponse.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryRedelegationsResponse,
        };
        message.redelegationResponses = [];
        if (object.redelegationResponses !== undefined &&
            object.redelegationResponses !== null) {
            for (const e of object.redelegationResponses) {
                message.redelegationResponses.push(RedelegationResponse.fromPartial(e));
            }
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageResponse.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
};
const baseQueryDelegatorValidatorsRequest = { delegatorAddr: "" };
export const QueryDelegatorValidatorsRequest = {
    encode(message, writer = Writer.create()) {
        if (message.delegatorAddr !== "") {
            writer.uint32(10).string(message.delegatorAddr);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryDelegatorValidatorsRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.delegatorAddr = reader.string();
                    break;
                case 2:
                    message.pagination = PageRequest.decode(reader, reader.uint32());
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
            ...baseQueryDelegatorValidatorsRequest,
        };
        if (object.delegatorAddr !== undefined && object.delegatorAddr !== null) {
            message.delegatorAddr = String(object.delegatorAddr);
        }
        else {
            message.delegatorAddr = "";
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromJSON(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.delegatorAddr !== undefined &&
            (obj.delegatorAddr = message.delegatorAddr);
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageRequest.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryDelegatorValidatorsRequest,
        };
        if (object.delegatorAddr !== undefined && object.delegatorAddr !== null) {
            message.delegatorAddr = object.delegatorAddr;
        }
        else {
            message.delegatorAddr = "";
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
};
const baseQueryDelegatorValidatorsResponse = {};
export const QueryDelegatorValidatorsResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.validators) {
            Validator.encode(v, writer.uint32(10).fork()).ldelim();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryDelegatorValidatorsResponse,
        };
        message.validators = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.validators.push(Validator.decode(reader, reader.uint32()));
                    break;
                case 2:
                    message.pagination = PageResponse.decode(reader, reader.uint32());
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
            ...baseQueryDelegatorValidatorsResponse,
        };
        message.validators = [];
        if (object.validators !== undefined && object.validators !== null) {
            for (const e of object.validators) {
                message.validators.push(Validator.fromJSON(e));
            }
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageResponse.fromJSON(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.validators) {
            obj.validators = message.validators.map((e) => e ? Validator.toJSON(e) : undefined);
        }
        else {
            obj.validators = [];
        }
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageResponse.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryDelegatorValidatorsResponse,
        };
        message.validators = [];
        if (object.validators !== undefined && object.validators !== null) {
            for (const e of object.validators) {
                message.validators.push(Validator.fromPartial(e));
            }
        }
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageResponse.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
};
const baseQueryDelegatorValidatorRequest = {
    delegatorAddr: "",
    validatorAddr: "",
};
export const QueryDelegatorValidatorRequest = {
    encode(message, writer = Writer.create()) {
        if (message.delegatorAddr !== "") {
            writer.uint32(10).string(message.delegatorAddr);
        }
        if (message.validatorAddr !== "") {
            writer.uint32(18).string(message.validatorAddr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryDelegatorValidatorRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.delegatorAddr = reader.string();
                    break;
                case 2:
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
            ...baseQueryDelegatorValidatorRequest,
        };
        if (object.delegatorAddr !== undefined && object.delegatorAddr !== null) {
            message.delegatorAddr = String(object.delegatorAddr);
        }
        else {
            message.delegatorAddr = "";
        }
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
        message.delegatorAddr !== undefined &&
            (obj.delegatorAddr = message.delegatorAddr);
        message.validatorAddr !== undefined &&
            (obj.validatorAddr = message.validatorAddr);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryDelegatorValidatorRequest,
        };
        if (object.delegatorAddr !== undefined && object.delegatorAddr !== null) {
            message.delegatorAddr = object.delegatorAddr;
        }
        else {
            message.delegatorAddr = "";
        }
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = object.validatorAddr;
        }
        else {
            message.validatorAddr = "";
        }
        return message;
    },
};
const baseQueryDelegatorValidatorResponse = {};
export const QueryDelegatorValidatorResponse = {
    encode(message, writer = Writer.create()) {
        if (message.validator !== undefined) {
            Validator.encode(message.validator, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryDelegatorValidatorResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.validator = Validator.decode(reader, reader.uint32());
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
            ...baseQueryDelegatorValidatorResponse,
        };
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = Validator.fromJSON(object.validator);
        }
        else {
            message.validator = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.validator !== undefined &&
            (obj.validator = message.validator
                ? Validator.toJSON(message.validator)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryDelegatorValidatorResponse,
        };
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = Validator.fromPartial(object.validator);
        }
        else {
            message.validator = undefined;
        }
        return message;
    },
};
const baseQueryHistoricalInfoRequest = { height: 0 };
export const QueryHistoricalInfoRequest = {
    encode(message, writer = Writer.create()) {
        if (message.height !== 0) {
            writer.uint32(8).int64(message.height);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryHistoricalInfoRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.height = longToNumber(reader.int64());
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
            ...baseQueryHistoricalInfoRequest,
        };
        if (object.height !== undefined && object.height !== null) {
            message.height = Number(object.height);
        }
        else {
            message.height = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.height !== undefined && (obj.height = message.height);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryHistoricalInfoRequest,
        };
        if (object.height !== undefined && object.height !== null) {
            message.height = object.height;
        }
        else {
            message.height = 0;
        }
        return message;
    },
};
const baseQueryHistoricalInfoResponse = {};
export const QueryHistoricalInfoResponse = {
    encode(message, writer = Writer.create()) {
        if (message.hist !== undefined) {
            HistoricalInfo.encode(message.hist, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryHistoricalInfoResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.hist = HistoricalInfo.decode(reader, reader.uint32());
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
            ...baseQueryHistoricalInfoResponse,
        };
        if (object.hist !== undefined && object.hist !== null) {
            message.hist = HistoricalInfo.fromJSON(object.hist);
        }
        else {
            message.hist = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.hist !== undefined &&
            (obj.hist = message.hist
                ? HistoricalInfo.toJSON(message.hist)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryHistoricalInfoResponse,
        };
        if (object.hist !== undefined && object.hist !== null) {
            message.hist = HistoricalInfo.fromPartial(object.hist);
        }
        else {
            message.hist = undefined;
        }
        return message;
    },
};
const baseQueryPoolRequest = {};
export const QueryPoolRequest = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryPoolRequest };
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
        const message = { ...baseQueryPoolRequest };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = { ...baseQueryPoolRequest };
        return message;
    },
};
const baseQueryPoolResponse = {};
export const QueryPoolResponse = {
    encode(message, writer = Writer.create()) {
        if (message.pool !== undefined) {
            Pool.encode(message.pool, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryPoolResponse };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.pool = Pool.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseQueryPoolResponse };
        if (object.pool !== undefined && object.pool !== null) {
            message.pool = Pool.fromJSON(object.pool);
        }
        else {
            message.pool = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.pool !== undefined &&
            (obj.pool = message.pool ? Pool.toJSON(message.pool) : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryPoolResponse };
        if (object.pool !== undefined && object.pool !== null) {
            message.pool = Pool.fromPartial(object.pool);
        }
        else {
            message.pool = undefined;
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
    Validators(request) {
        const data = QueryValidatorsRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.staking.v1beta1.Query", "Validators", data);
        return promise.then((data) => QueryValidatorsResponse.decode(new Reader(data)));
    }
    Validator(request) {
        const data = QueryValidatorRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.staking.v1beta1.Query", "Validator", data);
        return promise.then((data) => QueryValidatorResponse.decode(new Reader(data)));
    }
    ValidatorDelegations(request) {
        const data = QueryValidatorDelegationsRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.staking.v1beta1.Query", "ValidatorDelegations", data);
        return promise.then((data) => QueryValidatorDelegationsResponse.decode(new Reader(data)));
    }
    ValidatorUnbondingDelegations(request) {
        const data = QueryValidatorUnbondingDelegationsRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.staking.v1beta1.Query", "ValidatorUnbondingDelegations", data);
        return promise.then((data) => QueryValidatorUnbondingDelegationsResponse.decode(new Reader(data)));
    }
    Delegation(request) {
        const data = QueryDelegationRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.staking.v1beta1.Query", "Delegation", data);
        return promise.then((data) => QueryDelegationResponse.decode(new Reader(data)));
    }
    UnbondingDelegation(request) {
        const data = QueryUnbondingDelegationRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.staking.v1beta1.Query", "UnbondingDelegation", data);
        return promise.then((data) => QueryUnbondingDelegationResponse.decode(new Reader(data)));
    }
    DelegatorDelegations(request) {
        const data = QueryDelegatorDelegationsRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.staking.v1beta1.Query", "DelegatorDelegations", data);
        return promise.then((data) => QueryDelegatorDelegationsResponse.decode(new Reader(data)));
    }
    DelegatorUnbondingDelegations(request) {
        const data = QueryDelegatorUnbondingDelegationsRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.staking.v1beta1.Query", "DelegatorUnbondingDelegations", data);
        return promise.then((data) => QueryDelegatorUnbondingDelegationsResponse.decode(new Reader(data)));
    }
    Redelegations(request) {
        const data = QueryRedelegationsRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.staking.v1beta1.Query", "Redelegations", data);
        return promise.then((data) => QueryRedelegationsResponse.decode(new Reader(data)));
    }
    DelegatorValidators(request) {
        const data = QueryDelegatorValidatorsRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.staking.v1beta1.Query", "DelegatorValidators", data);
        return promise.then((data) => QueryDelegatorValidatorsResponse.decode(new Reader(data)));
    }
    DelegatorValidator(request) {
        const data = QueryDelegatorValidatorRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.staking.v1beta1.Query", "DelegatorValidator", data);
        return promise.then((data) => QueryDelegatorValidatorResponse.decode(new Reader(data)));
    }
    HistoricalInfo(request) {
        const data = QueryHistoricalInfoRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.staking.v1beta1.Query", "HistoricalInfo", data);
        return promise.then((data) => QueryHistoricalInfoResponse.decode(new Reader(data)));
    }
    Pool(request) {
        const data = QueryPoolRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.staking.v1beta1.Query", "Pool", data);
        return promise.then((data) => QueryPoolResponse.decode(new Reader(data)));
    }
    Params(request) {
        const data = QueryParamsRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.staking.v1beta1.Query", "Params", data);
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
