/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import { Params } from "../dex/params";
import { LongBook } from "../dex/long_book";
import { PageRequest, PageResponse, } from "../cosmos/base/query/v1beta1/pagination";
import { ShortBook } from "../dex/short_book";
import { Settlements } from "../dex/settlement";
import { Price, PriceCandlestick } from "../dex/price";
import { Twap } from "../dex/twap";
import { AssetMetadata } from "../dex/asset_list";
import { Pair } from "../dex/pair";
import { Order } from "../dex/order";
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
    priceDenom: "",
    assetDenom: "",
};
export const QueryGetLongBookRequest = {
    encode(message, writer = Writer.create()) {
        if (message.price !== "") {
            writer.uint32(10).string(message.price);
        }
        if (message.contractAddr !== "") {
            writer.uint32(18).string(message.contractAddr);
        }
        if (message.priceDenom !== "") {
            writer.uint32(26).string(message.priceDenom);
        }
        if (message.assetDenom !== "") {
            writer.uint32(34).string(message.assetDenom);
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
                    message.priceDenom = reader.string();
                    break;
                case 4:
                    message.assetDenom = reader.string();
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
            message.priceDenom = String(object.priceDenom);
        }
        else {
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = String(object.assetDenom);
        }
        else {
            message.assetDenom = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.price !== undefined && (obj.price = message.price);
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
        message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
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
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = "";
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
    priceDenom: "",
    assetDenom: "",
};
export const QueryAllLongBookRequest = {
    encode(message, writer = Writer.create()) {
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(10).fork()).ldelim();
        }
        if (message.contractAddr !== "") {
            writer.uint32(18).string(message.contractAddr);
        }
        if (message.priceDenom !== "") {
            writer.uint32(26).string(message.priceDenom);
        }
        if (message.assetDenom !== "") {
            writer.uint32(34).string(message.assetDenom);
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
                    message.priceDenom = reader.string();
                    break;
                case 4:
                    message.assetDenom = reader.string();
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
            message.priceDenom = String(object.priceDenom);
        }
        else {
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = String(object.assetDenom);
        }
        else {
            message.assetDenom = "";
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
        message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
        message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
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
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = "";
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
    priceDenom: "",
    assetDenom: "",
};
export const QueryGetShortBookRequest = {
    encode(message, writer = Writer.create()) {
        if (message.price !== "") {
            writer.uint32(10).string(message.price);
        }
        if (message.contractAddr !== "") {
            writer.uint32(18).string(message.contractAddr);
        }
        if (message.priceDenom !== "") {
            writer.uint32(26).string(message.priceDenom);
        }
        if (message.assetDenom !== "") {
            writer.uint32(34).string(message.assetDenom);
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
                    message.priceDenom = reader.string();
                    break;
                case 4:
                    message.assetDenom = reader.string();
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
            message.priceDenom = String(object.priceDenom);
        }
        else {
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = String(object.assetDenom);
        }
        else {
            message.assetDenom = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.price !== undefined && (obj.price = message.price);
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
        message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
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
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = "";
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
    priceDenom: "",
    assetDenom: "",
};
export const QueryAllShortBookRequest = {
    encode(message, writer = Writer.create()) {
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(10).fork()).ldelim();
        }
        if (message.contractAddr !== "") {
            writer.uint32(18).string(message.contractAddr);
        }
        if (message.priceDenom !== "") {
            writer.uint32(26).string(message.priceDenom);
        }
        if (message.assetDenom !== "") {
            writer.uint32(34).string(message.assetDenom);
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
                    message.priceDenom = reader.string();
                    break;
                case 4:
                    message.assetDenom = reader.string();
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
            message.priceDenom = String(object.priceDenom);
        }
        else {
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = String(object.assetDenom);
        }
        else {
            message.assetDenom = "";
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
        message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
        message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
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
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = "";
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
    orderId: 0,
    priceDenom: "",
    assetDenom: "",
    account: "",
};
export const QueryGetSettlementsRequest = {
    encode(message, writer = Writer.create()) {
        if (message.contractAddr !== "") {
            writer.uint32(10).string(message.contractAddr);
        }
        if (message.orderId !== 0) {
            writer.uint32(16).uint64(message.orderId);
        }
        if (message.priceDenom !== "") {
            writer.uint32(26).string(message.priceDenom);
        }
        if (message.assetDenom !== "") {
            writer.uint32(34).string(message.assetDenom);
        }
        if (message.account !== "") {
            writer.uint32(42).string(message.account);
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
                    message.orderId = longToNumber(reader.uint64());
                    break;
                case 3:
                    message.priceDenom = reader.string();
                    break;
                case 4:
                    message.assetDenom = reader.string();
                    break;
                case 5:
                    message.account = reader.string();
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
        if (object.orderId !== undefined && object.orderId !== null) {
            message.orderId = Number(object.orderId);
        }
        else {
            message.orderId = 0;
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = String(object.priceDenom);
        }
        else {
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = String(object.assetDenom);
        }
        else {
            message.assetDenom = "";
        }
        if (object.account !== undefined && object.account !== null) {
            message.account = String(object.account);
        }
        else {
            message.account = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.orderId !== undefined && (obj.orderId = message.orderId);
        message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
        message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
        message.account !== undefined && (obj.account = message.account);
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
        if (object.orderId !== undefined && object.orderId !== null) {
            message.orderId = object.orderId;
        }
        else {
            message.orderId = 0;
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = object.priceDenom;
        }
        else {
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = "";
        }
        if (object.account !== undefined && object.account !== null) {
            message.account = object.account;
        }
        else {
            message.account = "";
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
const baseQueryGetSettlementsForAccountRequest = {
    contractAddr: "",
    account: "",
    priceDenom: "",
    assetDenom: "",
};
export const QueryGetSettlementsForAccountRequest = {
    encode(message, writer = Writer.create()) {
        if (message.contractAddr !== "") {
            writer.uint32(10).string(message.contractAddr);
        }
        if (message.account !== "") {
            writer.uint32(18).string(message.account);
        }
        if (message.priceDenom !== "") {
            writer.uint32(26).string(message.priceDenom);
        }
        if (message.assetDenom !== "") {
            writer.uint32(34).string(message.assetDenom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryGetSettlementsForAccountRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.contractAddr = reader.string();
                    break;
                case 2:
                    message.account = reader.string();
                    break;
                case 3:
                    message.priceDenom = reader.string();
                    break;
                case 4:
                    message.assetDenom = reader.string();
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
            ...baseQueryGetSettlementsForAccountRequest,
        };
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        if (object.account !== undefined && object.account !== null) {
            message.account = String(object.account);
        }
        else {
            message.account = "";
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = String(object.priceDenom);
        }
        else {
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = String(object.assetDenom);
        }
        else {
            message.assetDenom = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.account !== undefined && (obj.account = message.account);
        message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
        message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryGetSettlementsForAccountRequest,
        };
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = object.contractAddr;
        }
        else {
            message.contractAddr = "";
        }
        if (object.account !== undefined && object.account !== null) {
            message.account = object.account;
        }
        else {
            message.account = "";
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = object.priceDenom;
        }
        else {
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = "";
        }
        return message;
    },
};
const baseQueryGetSettlementsForAccountResponse = {};
export const QueryGetSettlementsForAccountResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.SettlementsList) {
            Settlements.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryGetSettlementsForAccountResponse,
        };
        message.SettlementsList = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.SettlementsList.push(Settlements.decode(reader, reader.uint32()));
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
            ...baseQueryGetSettlementsForAccountResponse,
        };
        message.SettlementsList = [];
        if (object.SettlementsList !== undefined &&
            object.SettlementsList !== null) {
            for (const e of object.SettlementsList) {
                message.SettlementsList.push(Settlements.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.SettlementsList) {
            obj.SettlementsList = message.SettlementsList.map((e) => e ? Settlements.toJSON(e) : undefined);
        }
        else {
            obj.SettlementsList = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryGetSettlementsForAccountResponse,
        };
        message.SettlementsList = [];
        if (object.SettlementsList !== undefined &&
            object.SettlementsList !== null) {
            for (const e of object.SettlementsList) {
                message.SettlementsList.push(Settlements.fromPartial(e));
            }
        }
        return message;
    },
};
const baseQueryGetAllSettlementsRequest = {
    contractAddr: "",
    priceDenom: "",
    assetDenom: "",
    limit: 0,
};
export const QueryGetAllSettlementsRequest = {
    encode(message, writer = Writer.create()) {
        if (message.contractAddr !== "") {
            writer.uint32(10).string(message.contractAddr);
        }
        if (message.priceDenom !== "") {
            writer.uint32(18).string(message.priceDenom);
        }
        if (message.assetDenom !== "") {
            writer.uint32(26).string(message.assetDenom);
        }
        if (message.limit !== 0) {
            writer.uint32(32).uint64(message.limit);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryGetAllSettlementsRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.contractAddr = reader.string();
                    break;
                case 2:
                    message.priceDenom = reader.string();
                    break;
                case 3:
                    message.assetDenom = reader.string();
                    break;
                case 4:
                    message.limit = longToNumber(reader.uint64());
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
            ...baseQueryGetAllSettlementsRequest,
        };
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = String(object.priceDenom);
        }
        else {
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = String(object.assetDenom);
        }
        else {
            message.assetDenom = "";
        }
        if (object.limit !== undefined && object.limit !== null) {
            message.limit = Number(object.limit);
        }
        else {
            message.limit = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
        message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
        message.limit !== undefined && (obj.limit = message.limit);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryGetAllSettlementsRequest,
        };
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
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = "";
        }
        if (object.limit !== undefined && object.limit !== null) {
            message.limit = object.limit;
        }
        else {
            message.limit = 0;
        }
        return message;
    },
};
const baseQueryGetAllSettlementsResponse = {};
export const QueryGetAllSettlementsResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.SettlementsList) {
            Settlements.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryGetAllSettlementsResponse,
        };
        message.SettlementsList = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.SettlementsList.push(Settlements.decode(reader, reader.uint32()));
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
            ...baseQueryGetAllSettlementsResponse,
        };
        message.SettlementsList = [];
        if (object.SettlementsList !== undefined &&
            object.SettlementsList !== null) {
            for (const e of object.SettlementsList) {
                message.SettlementsList.push(Settlements.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.SettlementsList) {
            obj.SettlementsList = message.SettlementsList.map((e) => e ? Settlements.toJSON(e) : undefined);
        }
        else {
            obj.SettlementsList = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryGetAllSettlementsResponse,
        };
        message.SettlementsList = [];
        if (object.SettlementsList !== undefined &&
            object.SettlementsList !== null) {
            for (const e of object.SettlementsList) {
                message.SettlementsList.push(Settlements.fromPartial(e));
            }
        }
        return message;
    },
};
const baseQueryGetPricesRequest = {
    priceDenom: "",
    assetDenom: "",
    contractAddr: "",
};
export const QueryGetPricesRequest = {
    encode(message, writer = Writer.create()) {
        if (message.priceDenom !== "") {
            writer.uint32(10).string(message.priceDenom);
        }
        if (message.assetDenom !== "") {
            writer.uint32(18).string(message.assetDenom);
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
                    message.priceDenom = reader.string();
                    break;
                case 2:
                    message.assetDenom = reader.string();
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
            message.priceDenom = String(object.priceDenom);
        }
        else {
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = String(object.assetDenom);
        }
        else {
            message.assetDenom = "";
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
        message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
        message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
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
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = "";
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
const baseQueryAssetListRequest = {};
export const QueryAssetListRequest = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryAssetListRequest };
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
        const message = { ...baseQueryAssetListRequest };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = { ...baseQueryAssetListRequest };
        return message;
    },
};
const baseQueryAssetListResponse = {};
export const QueryAssetListResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.assetList) {
            AssetMetadata.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryAssetListResponse };
        message.assetList = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.assetList.push(AssetMetadata.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseQueryAssetListResponse };
        message.assetList = [];
        if (object.assetList !== undefined && object.assetList !== null) {
            for (const e of object.assetList) {
                message.assetList.push(AssetMetadata.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.assetList) {
            obj.assetList = message.assetList.map((e) => e ? AssetMetadata.toJSON(e) : undefined);
        }
        else {
            obj.assetList = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryAssetListResponse };
        message.assetList = [];
        if (object.assetList !== undefined && object.assetList !== null) {
            for (const e of object.assetList) {
                message.assetList.push(AssetMetadata.fromPartial(e));
            }
        }
        return message;
    },
};
const baseQueryAssetMetadataRequest = { denom: "" };
export const QueryAssetMetadataRequest = {
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
            ...baseQueryAssetMetadataRequest,
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
            ...baseQueryAssetMetadataRequest,
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
            ...baseQueryAssetMetadataRequest,
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
const baseQueryAssetMetadataResponse = {};
export const QueryAssetMetadataResponse = {
    encode(message, writer = Writer.create()) {
        if (message.metadata !== undefined) {
            AssetMetadata.encode(message.metadata, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryAssetMetadataResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.metadata = AssetMetadata.decode(reader, reader.uint32());
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
            ...baseQueryAssetMetadataResponse,
        };
        if (object.metadata !== undefined && object.metadata !== null) {
            message.metadata = AssetMetadata.fromJSON(object.metadata);
        }
        else {
            message.metadata = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.metadata !== undefined &&
            (obj.metadata = message.metadata
                ? AssetMetadata.toJSON(message.metadata)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryAssetMetadataResponse,
        };
        if (object.metadata !== undefined && object.metadata !== null) {
            message.metadata = AssetMetadata.fromPartial(object.metadata);
        }
        else {
            message.metadata = undefined;
        }
        return message;
    },
};
const baseQueryRegisteredPairsRequest = { contractAddr: "" };
export const QueryRegisteredPairsRequest = {
    encode(message, writer = Writer.create()) {
        if (message.contractAddr !== "") {
            writer.uint32(10).string(message.contractAddr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryRegisteredPairsRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
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
        const message = {
            ...baseQueryRegisteredPairsRequest,
        };
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
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryRegisteredPairsRequest,
        };
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = object.contractAddr;
        }
        else {
            message.contractAddr = "";
        }
        return message;
    },
};
const baseQueryRegisteredPairsResponse = {};
export const QueryRegisteredPairsResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.pairs) {
            Pair.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryRegisteredPairsResponse,
        };
        message.pairs = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.pairs.push(Pair.decode(reader, reader.uint32()));
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
            ...baseQueryRegisteredPairsResponse,
        };
        message.pairs = [];
        if (object.pairs !== undefined && object.pairs !== null) {
            for (const e of object.pairs) {
                message.pairs.push(Pair.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.pairs) {
            obj.pairs = message.pairs.map((e) => (e ? Pair.toJSON(e) : undefined));
        }
        else {
            obj.pairs = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryRegisteredPairsResponse,
        };
        message.pairs = [];
        if (object.pairs !== undefined && object.pairs !== null) {
            for (const e of object.pairs) {
                message.pairs.push(Pair.fromPartial(e));
            }
        }
        return message;
    },
};
const baseQueryGetOrdersRequest = { contractAddr: "", account: "" };
export const QueryGetOrdersRequest = {
    encode(message, writer = Writer.create()) {
        if (message.contractAddr !== "") {
            writer.uint32(10).string(message.contractAddr);
        }
        if (message.account !== "") {
            writer.uint32(18).string(message.account);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryGetOrdersRequest };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.contractAddr = reader.string();
                    break;
                case 2:
                    message.account = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseQueryGetOrdersRequest };
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        if (object.account !== undefined && object.account !== null) {
            message.account = String(object.account);
        }
        else {
            message.account = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.account !== undefined && (obj.account = message.account);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryGetOrdersRequest };
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = object.contractAddr;
        }
        else {
            message.contractAddr = "";
        }
        if (object.account !== undefined && object.account !== null) {
            message.account = object.account;
        }
        else {
            message.account = "";
        }
        return message;
    },
};
const baseQueryGetOrdersResponse = {};
export const QueryGetOrdersResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.orders) {
            Order.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryGetOrdersResponse };
        message.orders = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.orders.push(Order.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseQueryGetOrdersResponse };
        message.orders = [];
        if (object.orders !== undefined && object.orders !== null) {
            for (const e of object.orders) {
                message.orders.push(Order.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.orders) {
            obj.orders = message.orders.map((e) => (e ? Order.toJSON(e) : undefined));
        }
        else {
            obj.orders = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryGetOrdersResponse };
        message.orders = [];
        if (object.orders !== undefined && object.orders !== null) {
            for (const e of object.orders) {
                message.orders.push(Order.fromPartial(e));
            }
        }
        return message;
    },
};
const baseQueryGetOrderByIDRequest = {
    contractAddr: "",
    priceDenom: "",
    assetDenom: "",
    id: 0,
};
export const QueryGetOrderByIDRequest = {
    encode(message, writer = Writer.create()) {
        if (message.contractAddr !== "") {
            writer.uint32(10).string(message.contractAddr);
        }
        if (message.priceDenom !== "") {
            writer.uint32(18).string(message.priceDenom);
        }
        if (message.assetDenom !== "") {
            writer.uint32(26).string(message.assetDenom);
        }
        if (message.id !== 0) {
            writer.uint32(32).uint64(message.id);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryGetOrderByIDRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.contractAddr = reader.string();
                    break;
                case 2:
                    message.priceDenom = reader.string();
                    break;
                case 3:
                    message.assetDenom = reader.string();
                    break;
                case 4:
                    message.id = longToNumber(reader.uint64());
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
            ...baseQueryGetOrderByIDRequest,
        };
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = String(object.priceDenom);
        }
        else {
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = String(object.assetDenom);
        }
        else {
            message.assetDenom = "";
        }
        if (object.id !== undefined && object.id !== null) {
            message.id = Number(object.id);
        }
        else {
            message.id = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
        message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
        message.id !== undefined && (obj.id = message.id);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryGetOrderByIDRequest,
        };
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
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = "";
        }
        if (object.id !== undefined && object.id !== null) {
            message.id = object.id;
        }
        else {
            message.id = 0;
        }
        return message;
    },
};
const baseQueryGetOrderByIDResponse = {};
export const QueryGetOrderByIDResponse = {
    encode(message, writer = Writer.create()) {
        if (message.order !== undefined) {
            Order.encode(message.order, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryGetOrderByIDResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.order = Order.decode(reader, reader.uint32());
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
            ...baseQueryGetOrderByIDResponse,
        };
        if (object.order !== undefined && object.order !== null) {
            message.order = Order.fromJSON(object.order);
        }
        else {
            message.order = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.order !== undefined &&
            (obj.order = message.order ? Order.toJSON(message.order) : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryGetOrderByIDResponse,
        };
        if (object.order !== undefined && object.order !== null) {
            message.order = Order.fromPartial(object.order);
        }
        else {
            message.order = undefined;
        }
        return message;
    },
};
const baseQueryGetHistoricalPricesRequest = {
    contractAddr: "",
    priceDenom: "",
    assetDenom: "",
    periodLengthInSeconds: 0,
    numOfPeriods: 0,
};
export const QueryGetHistoricalPricesRequest = {
    encode(message, writer = Writer.create()) {
        if (message.contractAddr !== "") {
            writer.uint32(10).string(message.contractAddr);
        }
        if (message.priceDenom !== "") {
            writer.uint32(18).string(message.priceDenom);
        }
        if (message.assetDenom !== "") {
            writer.uint32(26).string(message.assetDenom);
        }
        if (message.periodLengthInSeconds !== 0) {
            writer.uint32(32).uint64(message.periodLengthInSeconds);
        }
        if (message.numOfPeriods !== 0) {
            writer.uint32(40).uint64(message.numOfPeriods);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryGetHistoricalPricesRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.contractAddr = reader.string();
                    break;
                case 2:
                    message.priceDenom = reader.string();
                    break;
                case 3:
                    message.assetDenom = reader.string();
                    break;
                case 4:
                    message.periodLengthInSeconds = longToNumber(reader.uint64());
                    break;
                case 5:
                    message.numOfPeriods = longToNumber(reader.uint64());
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
            ...baseQueryGetHistoricalPricesRequest,
        };
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = String(object.priceDenom);
        }
        else {
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = String(object.assetDenom);
        }
        else {
            message.assetDenom = "";
        }
        if (object.periodLengthInSeconds !== undefined &&
            object.periodLengthInSeconds !== null) {
            message.periodLengthInSeconds = Number(object.periodLengthInSeconds);
        }
        else {
            message.periodLengthInSeconds = 0;
        }
        if (object.numOfPeriods !== undefined && object.numOfPeriods !== null) {
            message.numOfPeriods = Number(object.numOfPeriods);
        }
        else {
            message.numOfPeriods = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
        message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
        message.periodLengthInSeconds !== undefined &&
            (obj.periodLengthInSeconds = message.periodLengthInSeconds);
        message.numOfPeriods !== undefined &&
            (obj.numOfPeriods = message.numOfPeriods);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryGetHistoricalPricesRequest,
        };
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
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = "";
        }
        if (object.periodLengthInSeconds !== undefined &&
            object.periodLengthInSeconds !== null) {
            message.periodLengthInSeconds = object.periodLengthInSeconds;
        }
        else {
            message.periodLengthInSeconds = 0;
        }
        if (object.numOfPeriods !== undefined && object.numOfPeriods !== null) {
            message.numOfPeriods = object.numOfPeriods;
        }
        else {
            message.numOfPeriods = 0;
        }
        return message;
    },
};
const baseQueryGetHistoricalPricesResponse = {};
export const QueryGetHistoricalPricesResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.prices) {
            PriceCandlestick.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryGetHistoricalPricesResponse,
        };
        message.prices = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.prices.push(PriceCandlestick.decode(reader, reader.uint32()));
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
            ...baseQueryGetHistoricalPricesResponse,
        };
        message.prices = [];
        if (object.prices !== undefined && object.prices !== null) {
            for (const e of object.prices) {
                message.prices.push(PriceCandlestick.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.prices) {
            obj.prices = message.prices.map((e) => e ? PriceCandlestick.toJSON(e) : undefined);
        }
        else {
            obj.prices = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryGetHistoricalPricesResponse,
        };
        message.prices = [];
        if (object.prices !== undefined && object.prices !== null) {
            for (const e of object.prices) {
                message.prices.push(PriceCandlestick.fromPartial(e));
            }
        }
        return message;
    },
};
const baseQueryGetMarketSummaryRequest = {
    contractAddr: "",
    priceDenom: "",
    assetDenom: "",
    lookbackInSeconds: 0,
};
export const QueryGetMarketSummaryRequest = {
    encode(message, writer = Writer.create()) {
        if (message.contractAddr !== "") {
            writer.uint32(10).string(message.contractAddr);
        }
        if (message.priceDenom !== "") {
            writer.uint32(18).string(message.priceDenom);
        }
        if (message.assetDenom !== "") {
            writer.uint32(26).string(message.assetDenom);
        }
        if (message.lookbackInSeconds !== 0) {
            writer.uint32(32).uint64(message.lookbackInSeconds);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryGetMarketSummaryRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.contractAddr = reader.string();
                    break;
                case 2:
                    message.priceDenom = reader.string();
                    break;
                case 3:
                    message.assetDenom = reader.string();
                    break;
                case 4:
                    message.lookbackInSeconds = longToNumber(reader.uint64());
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
            ...baseQueryGetMarketSummaryRequest,
        };
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = String(object.priceDenom);
        }
        else {
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = String(object.assetDenom);
        }
        else {
            message.assetDenom = "";
        }
        if (object.lookbackInSeconds !== undefined &&
            object.lookbackInSeconds !== null) {
            message.lookbackInSeconds = Number(object.lookbackInSeconds);
        }
        else {
            message.lookbackInSeconds = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
        message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
        message.lookbackInSeconds !== undefined &&
            (obj.lookbackInSeconds = message.lookbackInSeconds);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryGetMarketSummaryRequest,
        };
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
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = "";
        }
        if (object.lookbackInSeconds !== undefined &&
            object.lookbackInSeconds !== null) {
            message.lookbackInSeconds = object.lookbackInSeconds;
        }
        else {
            message.lookbackInSeconds = 0;
        }
        return message;
    },
};
const baseQueryGetMarketSummaryResponse = {
    totalVolume: "",
    totalVolumeNotional: "",
    highPrice: "",
    lowPrice: "",
    lastPrice: "",
};
export const QueryGetMarketSummaryResponse = {
    encode(message, writer = Writer.create()) {
        if (message.totalVolume !== "") {
            writer.uint32(10).string(message.totalVolume);
        }
        if (message.totalVolumeNotional !== "") {
            writer.uint32(18).string(message.totalVolumeNotional);
        }
        if (message.highPrice !== "") {
            writer.uint32(26).string(message.highPrice);
        }
        if (message.lowPrice !== "") {
            writer.uint32(34).string(message.lowPrice);
        }
        if (message.lastPrice !== "") {
            writer.uint32(42).string(message.lastPrice);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryGetMarketSummaryResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.totalVolume = reader.string();
                    break;
                case 2:
                    message.totalVolumeNotional = reader.string();
                    break;
                case 3:
                    message.highPrice = reader.string();
                    break;
                case 4:
                    message.lowPrice = reader.string();
                    break;
                case 5:
                    message.lastPrice = reader.string();
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
            ...baseQueryGetMarketSummaryResponse,
        };
        if (object.totalVolume !== undefined && object.totalVolume !== null) {
            message.totalVolume = String(object.totalVolume);
        }
        else {
            message.totalVolume = "";
        }
        if (object.totalVolumeNotional !== undefined &&
            object.totalVolumeNotional !== null) {
            message.totalVolumeNotional = String(object.totalVolumeNotional);
        }
        else {
            message.totalVolumeNotional = "";
        }
        if (object.highPrice !== undefined && object.highPrice !== null) {
            message.highPrice = String(object.highPrice);
        }
        else {
            message.highPrice = "";
        }
        if (object.lowPrice !== undefined && object.lowPrice !== null) {
            message.lowPrice = String(object.lowPrice);
        }
        else {
            message.lowPrice = "";
        }
        if (object.lastPrice !== undefined && object.lastPrice !== null) {
            message.lastPrice = String(object.lastPrice);
        }
        else {
            message.lastPrice = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.totalVolume !== undefined &&
            (obj.totalVolume = message.totalVolume);
        message.totalVolumeNotional !== undefined &&
            (obj.totalVolumeNotional = message.totalVolumeNotional);
        message.highPrice !== undefined && (obj.highPrice = message.highPrice);
        message.lowPrice !== undefined && (obj.lowPrice = message.lowPrice);
        message.lastPrice !== undefined && (obj.lastPrice = message.lastPrice);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryGetMarketSummaryResponse,
        };
        if (object.totalVolume !== undefined && object.totalVolume !== null) {
            message.totalVolume = object.totalVolume;
        }
        else {
            message.totalVolume = "";
        }
        if (object.totalVolumeNotional !== undefined &&
            object.totalVolumeNotional !== null) {
            message.totalVolumeNotional = object.totalVolumeNotional;
        }
        else {
            message.totalVolumeNotional = "";
        }
        if (object.highPrice !== undefined && object.highPrice !== null) {
            message.highPrice = object.highPrice;
        }
        else {
            message.highPrice = "";
        }
        if (object.lowPrice !== undefined && object.lowPrice !== null) {
            message.lowPrice = object.lowPrice;
        }
        else {
            message.lowPrice = "";
        }
        if (object.lastPrice !== undefined && object.lastPrice !== null) {
            message.lastPrice = object.lastPrice;
        }
        else {
            message.lastPrice = "";
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
    GetSettlements(request) {
        const data = QueryGetSettlementsRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "GetSettlements", data);
        return promise.then((data) => QueryGetSettlementsResponse.decode(new Reader(data)));
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
    AssetMetadata(request) {
        const data = QueryAssetMetadataRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "AssetMetadata", data);
        return promise.then((data) => QueryAssetMetadataResponse.decode(new Reader(data)));
    }
    AssetList(request) {
        const data = QueryAssetListRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "AssetList", data);
        return promise.then((data) => QueryAssetListResponse.decode(new Reader(data)));
    }
    GetRegisteredPairs(request) {
        const data = QueryRegisteredPairsRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "GetRegisteredPairs", data);
        return promise.then((data) => QueryRegisteredPairsResponse.decode(new Reader(data)));
    }
    GetOrders(request) {
        const data = QueryGetOrdersRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "GetOrders", data);
        return promise.then((data) => QueryGetOrdersResponse.decode(new Reader(data)));
    }
    GetOrder(request) {
        const data = QueryGetOrderByIDRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "GetOrder", data);
        return promise.then((data) => QueryGetOrderByIDResponse.decode(new Reader(data)));
    }
    GetHistoricalPrices(request) {
        const data = QueryGetHistoricalPricesRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "GetHistoricalPrices", data);
        return promise.then((data) => QueryGetHistoricalPricesResponse.decode(new Reader(data)));
    }
    GetMarketSummary(request) {
        const data = QueryGetMarketSummaryRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "GetMarketSummary", data);
        return promise.then((data) => QueryGetMarketSummaryResponse.decode(new Reader(data)));
    }
    GetSettlementsForAccount(request) {
        const data = QueryGetSettlementsForAccountRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "GetSettlementsForAccount", data);
        return promise.then((data) => QueryGetSettlementsForAccountResponse.decode(new Reader(data)));
    }
    GetAllSettlements(request) {
        const data = QueryGetAllSettlementsRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Query", "GetAllSettlements", data);
        return promise.then((data) => QueryGetAllSettlementsResponse.decode(new Reader(data)));
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
