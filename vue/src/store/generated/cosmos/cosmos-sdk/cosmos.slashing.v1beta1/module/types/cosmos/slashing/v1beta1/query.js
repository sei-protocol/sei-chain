/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Params, ValidatorSigningInfo, } from "../../../cosmos/slashing/v1beta1/slashing";
import { PageRequest, PageResponse, } from "../../../cosmos/base/query/v1beta1/pagination";
export const protobufPackage = "cosmos.slashing.v1beta1";
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
const baseQuerySigningInfoRequest = { consAddress: "" };
export const QuerySigningInfoRequest = {
    encode(message, writer = Writer.create()) {
        if (message.consAddress !== "") {
            writer.uint32(10).string(message.consAddress);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQuerySigningInfoRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.consAddress = reader.string();
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
            ...baseQuerySigningInfoRequest,
        };
        if (object.consAddress !== undefined && object.consAddress !== null) {
            message.consAddress = String(object.consAddress);
        }
        else {
            message.consAddress = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.consAddress !== undefined &&
            (obj.consAddress = message.consAddress);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQuerySigningInfoRequest,
        };
        if (object.consAddress !== undefined && object.consAddress !== null) {
            message.consAddress = object.consAddress;
        }
        else {
            message.consAddress = "";
        }
        return message;
    },
};
const baseQuerySigningInfoResponse = {};
export const QuerySigningInfoResponse = {
    encode(message, writer = Writer.create()) {
        if (message.valSigningInfo !== undefined) {
            ValidatorSigningInfo.encode(message.valSigningInfo, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQuerySigningInfoResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.valSigningInfo = ValidatorSigningInfo.decode(reader, reader.uint32());
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
            ...baseQuerySigningInfoResponse,
        };
        if (object.valSigningInfo !== undefined && object.valSigningInfo !== null) {
            message.valSigningInfo = ValidatorSigningInfo.fromJSON(object.valSigningInfo);
        }
        else {
            message.valSigningInfo = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.valSigningInfo !== undefined &&
            (obj.valSigningInfo = message.valSigningInfo
                ? ValidatorSigningInfo.toJSON(message.valSigningInfo)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQuerySigningInfoResponse,
        };
        if (object.valSigningInfo !== undefined && object.valSigningInfo !== null) {
            message.valSigningInfo = ValidatorSigningInfo.fromPartial(object.valSigningInfo);
        }
        else {
            message.valSigningInfo = undefined;
        }
        return message;
    },
};
const baseQuerySigningInfosRequest = {};
export const QuerySigningInfosRequest = {
    encode(message, writer = Writer.create()) {
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQuerySigningInfosRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
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
            ...baseQuerySigningInfosRequest,
        };
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
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageRequest.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQuerySigningInfosRequest,
        };
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        return message;
    },
};
const baseQuerySigningInfosResponse = {};
export const QuerySigningInfosResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.info) {
            ValidatorSigningInfo.encode(v, writer.uint32(10).fork()).ldelim();
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
            ...baseQuerySigningInfosResponse,
        };
        message.info = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.info.push(ValidatorSigningInfo.decode(reader, reader.uint32()));
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
            ...baseQuerySigningInfosResponse,
        };
        message.info = [];
        if (object.info !== undefined && object.info !== null) {
            for (const e of object.info) {
                message.info.push(ValidatorSigningInfo.fromJSON(e));
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
        if (message.info) {
            obj.info = message.info.map((e) => e ? ValidatorSigningInfo.toJSON(e) : undefined);
        }
        else {
            obj.info = [];
        }
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageResponse.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQuerySigningInfosResponse,
        };
        message.info = [];
        if (object.info !== undefined && object.info !== null) {
            for (const e of object.info) {
                message.info.push(ValidatorSigningInfo.fromPartial(e));
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
    Params(request) {
        const data = QueryParamsRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.slashing.v1beta1.Query", "Params", data);
        return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
    }
    SigningInfo(request) {
        const data = QuerySigningInfoRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.slashing.v1beta1.Query", "SigningInfo", data);
        return promise.then((data) => QuerySigningInfoResponse.decode(new Reader(data)));
    }
    SigningInfos(request) {
        const data = QuerySigningInfosRequest.encode(request).finish();
        const promise = this.rpc.request("cosmos.slashing.v1beta1.Query", "SigningInfos", data);
        return promise.then((data) => QuerySigningInfosResponse.decode(new Reader(data)));
    }
}
