/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Grant } from "../../../cosmos/feegrant/v1beta1/feegrant";
import { PageRequest, PageResponse, } from "../../../cosmos/base/query/v1beta1/pagination";
export const protobufPackage = "cosmos.feegrant.v1beta1";
const baseQueryAllowanceRequest = { granter: "", grantee: "" };
export const QueryAllowanceRequest = {
    encode(message, writer = Writer.create()) {
        if (message.granter !== "") {
            writer.uint32(10).string(message.granter);
        }
        if (message.grantee !== "") {
            writer.uint32(18).string(message.grantee);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryAllowanceRequest };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.granter = reader.string();
                    break;
                case 2:
                    message.grantee = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseQueryAllowanceRequest };
        if (object.granter !== undefined && object.granter !== null) {
            message.granter = String(object.granter);
        }
        else {
            message.granter = "";
        }
        if (object.grantee !== undefined && object.grantee !== null) {
            message.grantee = String(object.grantee);
        }
        else {
            message.grantee = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.granter !== undefined && (obj.granter = message.granter);
        message.grantee !== undefined && (obj.grantee = message.grantee);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryAllowanceRequest };
        if (object.granter !== undefined && object.granter !== null) {
            message.granter = object.granter;
        }
        else {
            message.granter = "";
        }
        if (object.grantee !== undefined && object.grantee !== null) {
            message.grantee = object.grantee;
        }
        else {
            message.grantee = "";
        }
        return message;
    },
};
const baseQueryAllowanceResponse = {};
export const QueryAllowanceResponse = {
    encode(message, writer = Writer.create()) {
        if (message.allowance !== undefined) {
            Grant.encode(message.allowance, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryAllowanceResponse };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.allowance = Grant.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseQueryAllowanceResponse };
        if (object.allowance !== undefined && object.allowance !== null) {
            message.allowance = Grant.fromJSON(object.allowance);
        }
        else {
            message.allowance = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.allowance !== undefined &&
            (obj.allowance = message.allowance
                ? Grant.toJSON(message.allowance)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryAllowanceResponse };
        if (object.allowance !== undefined && object.allowance !== null) {
            message.allowance = Grant.fromPartial(object.allowance);
        }
        else {
            message.allowance = undefined;
        }
        return message;
    },
};
const baseQueryAllowancesRequest = { grantee: "" };
export const QueryAllowancesRequest = {
    encode(message, writer = Writer.create()) {
        if (message.grantee !== "") {
            writer.uint32(10).string(message.grantee);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryAllowancesRequest };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.grantee = reader.string();
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
        const message = { ...baseQueryAllowancesRequest };
        if (object.grantee !== undefined && object.grantee !== null) {
            message.grantee = String(object.grantee);
        }
        else {
            message.grantee = "";
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
        message.grantee !== undefined && (obj.grantee = message.grantee);
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageRequest.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryAllowancesRequest };
        if (object.grantee !== undefined && object.grantee !== null) {
            message.grantee = object.grantee;
        }
        else {
            message.grantee = "";
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
const baseQueryAllowancesResponse = {};
export const QueryAllowancesResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.allowances) {
            Grant.encode(v, writer.uint32(10).fork()).ldelim();
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
            ...baseQueryAllowancesResponse,
        };
        message.allowances = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.allowances.push(Grant.decode(reader, reader.uint32()));
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
            ...baseQueryAllowancesResponse,
        };
        message.allowances = [];
        if (object.allowances !== undefined && object.allowances !== null) {
            for (const e of object.allowances) {
                message.allowances.push(Grant.fromJSON(e));
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
        if (message.allowances) {
            obj.allowances = message.allowances.map((e) => e ? Grant.toJSON(e) : undefined);
        }
        else {
            obj.allowances = [];
        }
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageResponse.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryAllowancesResponse,
        };
        message.allowances = [];
        if (object.allowances !== undefined && object.allowances !== null) {
            for (const e of object.allowances) {
                message.allowances.push(Grant.fromPartial(e));
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
export class QueryClientImpl {
    constructor(rpc) {
        this.rpc = rpc;
    }
    Allowance(request) {
        const data = QueryAllowanceRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.feegrant.v1beta1.Query", "Allowance", data);
        return promise.then((data) => QueryAllowanceResponse.decode(new Reader(data)));
    }
    Allowances(request) {
        const data = QueryAllowancesRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.feegrant.v1beta1.Query", "Allowances", data);
        return promise.then((data) => QueryAllowancesResponse.decode(new Reader(data)));
    }
}
