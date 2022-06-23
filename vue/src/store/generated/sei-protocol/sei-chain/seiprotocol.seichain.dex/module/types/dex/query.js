/* eslint-disable */
import { denomFromJSON, denomToJSON } from "../dex/enums";
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import { Params } from "../dex/params";
import { LongBook } from "../dex/long_book";
import { PageRequest, PageResponse, } from "../cosmos/base/query/v1beta1/pagination";
import { ShortBook } from "../dex/short_book";
import { Settlements } from "../dex/settlement";
import { Price } from "../dex/price";
import { Twap } from "../dex/twap";
export const protobufPackage = "seiprotocol.seichain.dex";
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
const baseQueryGetLongBookRequest = {
    price: "",
    contractAddr: "",
    priceDenom: 0,
    assetDenom: 0,
};
export const QueryGetLongBookRequest = {
    encode(message, writer = Writer.create()) {
        if (message.price !== "") {
            writer.uint32(10).string(message.price);
        }
        if (message.contractAddr !== "") {
            writer.uint32(18).string(message.contractAddr);
        }
        if (message.priceDenom !== 0) {
            writer.uint32(24).int32(message.priceDenom);
        }
        if (message.assetDenom !== 0) {
            writer.uint32(32).int32(message.assetDenom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryGetLongBookRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.price = reader.string();
                    break;
                case 2:
                    message.contractAddr = reader.string();
                    break;
                case 3:
                    message.priceDenom = reader.int32();
                    break;
                case 4:
                    message.assetDenom = reader.int32();
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
            ...baseQueryGetLongBookRequest,
        };
        if (object.price !== undefined && object.price !== null) {
            message.price = String(object.price);
        }
        else {
            message.price = "";
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = denomFromJSON(object.priceDenom);
        }
        else {
            message.priceDenom = 0;
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = denomFromJSON(object.assetDenom);
        }
        else {
            message.assetDenom = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.price !== undefined && (obj.price = message.price);
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.priceDenom !== undefined &&
            (obj.priceDenom = denomToJSON(message.priceDenom));
        message.assetDenom !== undefined &&
            (obj.assetDenom = denomToJSON(message.assetDenom));
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryGetLongBookRequest,
        };
        if (object.price !== undefined && object.price !== null) {
            message.price = object.price;
        }
        else {
            message.price = "";
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = object.contractAddr;
        }
        else {
            message.contractAddr = "";
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = object.priceDenom;
        }
        else {
            message.priceDenom = 0;
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = 0;
        }
        return message;
    },
};
const baseQueryGetLongBookResponse = {};
export const QueryGetLongBookResponse = {
    encode(message, writer = Writer.create()) {
        if (message.LongBook !== undefined) {
            LongBook.encode(message.LongBook, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryGetLongBookResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.LongBook = LongBook.decode(reader, reader.uint32());
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
            ...baseQueryGetLongBookResponse,
        };
        if (object.LongBook !== undefined && object.LongBook !== null) {
            message.LongBook = LongBook.fromJSON(object.LongBook);
        }
        else {
            message.LongBook = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.LongBook !== undefined &&
            (obj.LongBook = message.LongBook
                ? LongBook.toJSON(message.LongBook)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryGetLongBookResponse,
        };
        if (object.LongBook !== undefined && object.LongBook !== null) {
            message.LongBook = LongBook.fromPartial(object.LongBook);
        }
        else {
            message.LongBook = undefined;
        }
        return message;
    },
};
const baseQueryAllLongBookRequest = {
    contractAddr: "",
    priceDenom: 0,
    assetDenom: 0,
};
export const QueryAllLongBookRequest = {
    encode(message, writer = Writer.create()) {
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(10).fork()).ldelim();
        }
        if (message.contractAddr !== "") {
            writer.uint32(18).string(message.contractAddr);
        }
        if (message.priceDenom !== 0) {
            writer.uint32(24).int32(message.priceDenom);
        }
        if (message.assetDenom !== 0) {
            writer.uint32(32).int32(message.assetDenom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryAllLongBookRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.pagination = PageRequest.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.contractAddr = reader.string();
                    break;
                case 3:
                    message.priceDenom = reader.int32();
                    break;
                case 4:
                    message.assetDenom = reader.int32();
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
            ...baseQueryAllLongBookRequest,
        };
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromJSON(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = denomFromJSON(object.priceDenom);
        }
        else {
            message.priceDenom = 0;
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = denomFromJSON(object.assetDenom);
        }
        else {
            message.assetDenom = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageRequest.toJSON(message.pagination)
                : undefined);
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.priceDenom !== undefined &&
            (obj.priceDenom = denomToJSON(message.priceDenom));
        message.assetDenom !== undefined &&
            (obj.assetDenom = denomToJSON(message.assetDenom));
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryAllLongBookRequest,
        };
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = object.contractAddr;
        }
        else {
            message.contractAddr = "";
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = object.priceDenom;
        }
        else {
            message.priceDenom = 0;
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = 0;
        }
        return message;
    },
};
const baseQueryAllLongBookResponse = {};
export const QueryAllLongBookResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.LongBook) {
            LongBook.encode(v, writer.uint32(10).fork()).ldelim();
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
            ...baseQueryAllLongBookResponse,
        };
        message.LongBook = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.LongBook.push(LongBook.decode(reader, reader.uint32()));
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
            ...baseQueryAllLongBookResponse,
        };
        message.LongBook = [];
        if (object.LongBook !== undefined && object.LongBook !== null) {
            for (const e of object.LongBook) {
                message.LongBook.push(LongBook.fromJSON(e));
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
        if (message.LongBook) {
            obj.LongBook = message.LongBook.map((e) => e ? LongBook.toJSON(e) : undefined);
        }
        else {
            obj.LongBook = [];
        }
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageResponse.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryAllLongBookResponse,
        };
        message.LongBook = [];
        if (object.LongBook !== undefined && object.LongBook !== null) {
            for (const e of object.LongBook) {
                message.LongBook.push(LongBook.fromPartial(e));
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
const baseQueryGetShortBookRequest = {
    price: "",
    contractAddr: "",
    priceDenom: 0,
    assetDenom: 0,
};
export const QueryGetShortBookRequest = {
    encode(message, writer = Writer.create()) {
        if (message.price !== "") {
            writer.uint32(10).string(message.price);
        }
        if (message.contractAddr !== "") {
            writer.uint32(18).string(message.contractAddr);
        }
        if (message.priceDenom !== 0) {
            writer.uint32(24).int32(message.priceDenom);
        }
        if (message.assetDenom !== 0) {
            writer.uint32(32).int32(message.assetDenom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryGetShortBookRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.price = reader.string();
                    break;
                case 2:
                    message.contractAddr = reader.string();
                    break;
                case 3:
                    message.priceDenom = reader.int32();
                    break;
                case 4:
                    message.assetDenom = reader.int32();
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
            ...baseQueryGetShortBookRequest,
        };
        if (object.price !== undefined && object.price !== null) {
            message.price = String(object.price);
        }
        else {
            message.price = "";
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = denomFromJSON(object.priceDenom);
        }
        else {
            message.priceDenom = 0;
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = denomFromJSON(object.assetDenom);
        }
        else {
            message.assetDenom = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.price !== undefined && (obj.price = message.price);
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.priceDenom !== undefined &&
            (obj.priceDenom = denomToJSON(message.priceDenom));
        message.assetDenom !== undefined &&
            (obj.assetDenom = denomToJSON(message.assetDenom));
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryGetShortBookRequest,
        };
        if (object.price !== undefined && object.price !== null) {
            message.price = object.price;
        }
        else {
            message.price = "";
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = object.contractAddr;
        }
        else {
            message.contractAddr = "";
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = object.priceDenom;
        }
        else {
            message.priceDenom = 0;
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = 0;
        }
        return message;
    },
};
const baseQueryGetShortBookResponse = {};
export const QueryGetShortBookResponse = {
    encode(message, writer = Writer.create()) {
        if (message.ShortBook !== undefined) {
            ShortBook.encode(message.ShortBook, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryGetShortBookResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.ShortBook = ShortBook.decode(reader, reader.uint32());
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
            ...baseQueryGetShortBookResponse,
        };
        if (object.ShortBook !== undefined && object.ShortBook !== null) {
            message.ShortBook = ShortBook.fromJSON(object.ShortBook);
        }
        else {
            message.ShortBook = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.ShortBook !== undefined &&
            (obj.ShortBook = message.ShortBook
                ? ShortBook.toJSON(message.ShortBook)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryGetShortBookResponse,
        };
        if (object.ShortBook !== undefined && object.ShortBook !== null) {
            message.ShortBook = ShortBook.fromPartial(object.ShortBook);
        }
        else {
            message.ShortBook = undefined;
        }
        return message;
    },
};
const baseQueryAllShortBookRequest = {
    contractAddr: "",
    priceDenom: 0,
    assetDenom: 0,
};
export const QueryAllShortBookRequest = {
    encode(message, writer = Writer.create()) {
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(10).fork()).ldelim();
        }
        if (message.contractAddr !== "") {
            writer.uint32(18).string(message.contractAddr);
        }
        if (message.priceDenom !== 0) {
            writer.uint32(24).int32(message.priceDenom);
        }
        if (message.assetDenom !== 0) {
            writer.uint32(32).int32(message.assetDenom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryAllShortBookRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.pagination = PageRequest.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.contractAddr = reader.string();
                    break;
                case 3:
                    message.priceDenom = reader.int32();
                    break;
                case 4:
                    message.assetDenom = reader.int32();
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
            ...baseQueryAllShortBookRequest,
        };
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromJSON(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = denomFromJSON(object.priceDenom);
        }
        else {
            message.priceDenom = 0;
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = denomFromJSON(object.assetDenom);
        }
        else {
            message.assetDenom = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageRequest.toJSON(message.pagination)
                : undefined);
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.priceDenom !== undefined &&
            (obj.priceDenom = denomToJSON(message.priceDenom));
        message.assetDenom !== undefined &&
            (obj.assetDenom = denomToJSON(message.assetDenom));
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryAllShortBookRequest,
        };
        if (object.pagination !== undefined && object.pagination !== null) {
            message.pagination = PageRequest.fromPartial(object.pagination);
        }
        else {
            message.pagination = undefined;
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = object.contractAddr;
        }
        else {
            message.contractAddr = "";
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = object.priceDenom;
        }
        else {
            message.priceDenom = 0;
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = 0;
        }
        return message;
    },
};
const baseQueryAllShortBookResponse = {};
export const QueryAllShortBookResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.ShortBook) {
            ShortBook.encode(v, writer.uint32(10).fork()).ldelim();
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
            ...baseQueryAllShortBookResponse,
        };
        message.ShortBook = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.ShortBook.push(ShortBook.decode(reader, reader.uint32()));
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
            ...baseQueryAllShortBookResponse,
        };
        message.ShortBook = [];
        if (object.ShortBook !== undefined && object.ShortBook !== null) {
            for (const e of object.ShortBook) {
                message.ShortBook.push(ShortBook.fromJSON(e));
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
        if (message.ShortBook) {
            obj.ShortBook = message.ShortBook.map((e) => e ? ShortBook.toJSON(e) : undefined);
        }
        else {
            obj.ShortBook = [];
        }
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageResponse.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryAllShortBookResponse,
        };
        message.ShortBook = [];
        if (object.ShortBook !== undefined && object.ShortBook !== null) {
            for (const e of object.ShortBook) {
                message.ShortBook.push(ShortBook.fromPartial(e));
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
const baseQueryGetSettlementsRequest = {
    contractAddr: "",
    blockHeight: 0,
    priceDenom: 0,
    assetDenom: 0,
};
export const QueryGetSettlementsRequest = {
    encode(message, writer = Writer.create()) {
        if (message.contractAddr !== "") {
            writer.uint32(10).string(message.contractAddr);
        }
        if (message.blockHeight !== 0) {
            writer.uint32(16).uint64(message.blockHeight);
        }
        if (message.priceDenom !== 0) {
            writer.uint32(24).int32(message.priceDenom);
        }
        if (message.assetDenom !== 0) {
            writer.uint32(32).int32(message.assetDenom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryGetSettlementsRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.contractAddr = reader.string();
                    break;
                case 2:
                    message.blockHeight = longToNumber(reader.uint64());
                    break;
                case 3:
                    message.priceDenom = reader.int32();
                    break;
                case 4:
                    message.assetDenom = reader.int32();
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
            ...baseQueryGetSettlementsRequest,
        };
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        if (object.blockHeight !== undefined && object.blockHeight !== null) {
            message.blockHeight = Number(object.blockHeight);
        }
        else {
            message.blockHeight = 0;
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = denomFromJSON(object.priceDenom);
        }
        else {
            message.priceDenom = 0;
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = denomFromJSON(object.assetDenom);
        }
        else {
            message.assetDenom = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.blockHeight !== undefined &&
            (obj.blockHeight = message.blockHeight);
        message.priceDenom !== undefined &&
            (obj.priceDenom = denomToJSON(message.priceDenom));
        message.assetDenom !== undefined &&
            (obj.assetDenom = denomToJSON(message.assetDenom));
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryGetSettlementsRequest,
        };
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = object.contractAddr;
        }
        else {
            message.contractAddr = "";
        }
        if (object.blockHeight !== undefined && object.blockHeight !== null) {
            message.blockHeight = object.blockHeight;
        }
        else {
            message.blockHeight = 0;
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = object.priceDenom;
        }
        else {
            message.priceDenom = 0;
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = 0;
        }
        return message;
    },
};
const baseQueryGetSettlementsResponse = {};
export const QueryGetSettlementsResponse = {
    encode(message, writer = Writer.create()) {
        if (message.Settlements !== undefined) {
            Settlements.encode(message.Settlements, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryGetSettlementsResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.Settlements = Settlements.decode(reader, reader.uint32());
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
            ...baseQueryGetSettlementsResponse,
        };
        if (object.Settlements !== undefined && object.Settlements !== null) {
            message.Settlements = Settlements.fromJSON(object.Settlements);
        }
        else {
            message.Settlements = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.Settlements !== undefined &&
            (obj.Settlements = message.Settlements
                ? Settlements.toJSON(message.Settlements)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryGetSettlementsResponse,
        };
        if (object.Settlements !== undefined && object.Settlements !== null) {
            message.Settlements = Settlements.fromPartial(object.Settlements);
        }
        else {
            message.Settlements = undefined;
        }
        return message;
    },
};
const baseQueryAllSettlementsRequest = {};
export const QueryAllSettlementsRequest = {
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
            ...baseQueryAllSettlementsRequest,
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
            ...baseQueryAllSettlementsRequest,
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
            ...baseQueryAllSettlementsRequest,
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
const baseQueryAllSettlementsResponse = {};
export const QueryAllSettlementsResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.Settlements) {
            Settlements.encode(v, writer.uint32(10).fork()).ldelim();
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
            ...baseQueryAllSettlementsResponse,
        };
        message.Settlements = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.Settlements.push(Settlements.decode(reader, reader.uint32()));
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
            ...baseQueryAllSettlementsResponse,
        };
        message.Settlements = [];
        if (object.Settlements !== undefined && object.Settlements !== null) {
            for (const e of object.Settlements) {
                message.Settlements.push(Settlements.fromJSON(e));
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
        if (message.Settlements) {
            obj.Settlements = message.Settlements.map((e) => e ? Settlements.toJSON(e) : undefined);
        }
        else {
            obj.Settlements = [];
        }
        message.pagination !== undefined &&
            (obj.pagination = message.pagination
                ? PageResponse.toJSON(message.pagination)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryAllSettlementsResponse,
        };
        message.Settlements = [];
        if (object.Settlements !== undefined && object.Settlements !== null) {
            for (const e of object.Settlements) {
                message.Settlements.push(Settlements.fromPartial(e));
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
const baseQueryGetPricesRequest = {
    priceDenom: 0,
    assetDenom: 0,
    contractAddr: "",
};
export const QueryGetPricesRequest = {
    encode(message, writer = Writer.create()) {
        if (message.priceDenom !== 0) {
            writer.uint32(8).int32(message.priceDenom);
        }
        if (message.assetDenom !== 0) {
            writer.uint32(16).int32(message.assetDenom);
        }
        if (message.contractAddr !== "") {
            writer.uint32(26).string(message.contractAddr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryGetPricesRequest };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.priceDenom = reader.int32();
                    break;
                case 2:
                    message.assetDenom = reader.int32();
                    break;
                case 3:
                    message.contractAddr = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseQueryGetPricesRequest };
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = denomFromJSON(object.priceDenom);
        }
        else {
            message.priceDenom = 0;
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = denomFromJSON(object.assetDenom);
        }
        else {
            message.assetDenom = 0;
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.priceDenom !== undefined &&
            (obj.priceDenom = denomToJSON(message.priceDenom));
        message.assetDenom !== undefined &&
            (obj.assetDenom = denomToJSON(message.assetDenom));
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryGetPricesRequest };
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = object.priceDenom;
        }
        else {
            message.priceDenom = 0;
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = 0;
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = object.contractAddr;
        }
        else {
            message.contractAddr = "";
        }
        return message;
    },
};
const baseQueryGetPricesResponse = {};
export const QueryGetPricesResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.prices) {
            Price.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryGetPricesResponse };
        message.prices = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.prices.push(Price.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseQueryGetPricesResponse };
        message.prices = [];
        if (object.prices !== undefined && object.prices !== null) {
            for (const e of object.prices) {
                message.prices.push(Price.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.prices) {
            obj.prices = message.prices.map((e) => (e ? Price.toJSON(e) : undefined));
        }
        else {
            obj.prices = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryGetPricesResponse };
        message.prices = [];
        if (object.prices !== undefined && object.prices !== null) {
            for (const e of object.prices) {
                message.prices.push(Price.fromPartial(e));
            }
        }
        return message;
    },
};
const baseQueryGetTwapsRequest = {
    contractAddr: "",
    lookbackSeconds: 0,
};
export const QueryGetTwapsRequest = {
    encode(message, writer = Writer.create()) {
        if (message.contractAddr !== "") {
            writer.uint32(10).string(message.contractAddr);
        }
        if (message.lookbackSeconds !== 0) {
            writer.uint32(16).uint64(message.lookbackSeconds);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryGetTwapsRequest };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.contractAddr = reader.string();
                    break;
                case 2:
                    message.lookbackSeconds = longToNumber(reader.uint64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseQueryGetTwapsRequest };
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
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
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.lookbackSeconds !== undefined &&
            (obj.lookbackSeconds = message.lookbackSeconds);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryGetTwapsRequest };
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = object.contractAddr;
        }
        else {
            message.contractAddr = "";
        }
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
const baseQueryGetTwapsResponse = {};
export const QueryGetTwapsResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.twaps) {
            Twap.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryGetTwapsResponse };
        message.twaps = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.twaps.push(Twap.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseQueryGetTwapsResponse };
        message.twaps = [];
        if (object.twaps !== undefined && object.twaps !== null) {
            for (const e of object.twaps) {
                message.twaps.push(Twap.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.twaps) {
            obj.twaps = message.twaps.map((e) => (e ? Twap.toJSON(e) : undefined));
        }
        else {
            obj.twaps = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryGetTwapsResponse };
        message.twaps = [];
        if (object.twaps !== undefined && object.twaps !== null) {
            for (const e of object.twaps) {
                message.twaps.push(Twap.fromPartial(e));
            }
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
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "Params", data);
        return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
    }
    LongBook(request) {
        const data = QueryGetLongBookRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "LongBook", data);
        return promise.then((data) => QueryGetLongBookResponse.decode(new Reader(data)));
    }
    LongBookAll(request) {
        const data = QueryAllLongBookRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "LongBookAll", data);
        return promise.then((data) => QueryAllLongBookResponse.decode(new Reader(data)));
    }
    ShortBook(request) {
        const data = QueryGetShortBookRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "ShortBook", data);
        return promise.then((data) => QueryGetShortBookResponse.decode(new Reader(data)));
    }
    ShortBookAll(request) {
        const data = QueryAllShortBookRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "ShortBookAll", data);
        return promise.then((data) => QueryAllShortBookResponse.decode(new Reader(data)));
    }
    SettlementsAll(request) {
        const data = QueryAllSettlementsRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "SettlementsAll", data);
        return promise.then((data) => QueryAllSettlementsResponse.decode(new Reader(data)));
    }
    GetPrices(request) {
        const data = QueryGetPricesRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "GetPrices", data);
        return promise.then((data) => QueryGetPricesResponse.decode(new Reader(data)));
    }
    GetTwaps(request) {
        const data = QueryGetTwapsRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "GetTwaps", data);
        return promise.then((data) => QueryGetTwapsResponse.decode(new Reader(data)));
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
