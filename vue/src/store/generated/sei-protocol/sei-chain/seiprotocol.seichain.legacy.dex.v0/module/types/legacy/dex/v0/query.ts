/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import { Params } from "../../../legacy/dex/v0/params";
import { LongBook } from "../../../legacy/dex/v0/long_book";
import {
  PageRequest,
  PageResponse,
} from "../../../cosmos/base/query/v1beta1/pagination";
import { ShortBook } from "../../../legacy/dex/v0/short_book";
import { Settlements } from "../../../legacy/dex/v0/settlement";
import { Twap } from "../../../legacy/dex/v0/twap";

export const protobufPackage = "seiprotocol.seichain.legacy.dex.v0";

/** QueryParamsRequest is request type for the Query/Params RPC method. */
export interface QueryParamsRequest {}

/** QueryParamsResponse is response type for the Query/Params RPC method. */
export interface QueryParamsResponse {
  /** params holds all the parameters of this module. */
  params: Params | undefined;
}

export interface QueryGetLongBookRequest {
  id: number;
  contractAddr: string;
  priceDenom: string;
  assetDenom: string;
}

export interface QueryGetLongBookResponse {
  LongBook: LongBook | undefined;
}

export interface QueryAllLongBookRequest {
  pagination: PageRequest | undefined;
}

export interface QueryAllLongBookResponse {
  LongBook: LongBook[];
  pagination: PageResponse | undefined;
}

export interface QueryGetShortBookRequest {
  id: number;
  contractAddr: string;
  priceDenom: string;
  assetDenom: string;
}

export interface QueryGetShortBookResponse {
  ShortBook: ShortBook | undefined;
}

export interface QueryAllShortBookRequest {
  pagination: PageRequest | undefined;
}

export interface QueryAllShortBookResponse {
  ShortBook: ShortBook[];
  pagination: PageResponse | undefined;
}

export interface QueryGetSettlementsRequest {
  contractAddr: string;
  blockHeight: number;
  priceDenom: string;
  assetDenom: string;
}

export interface QueryGetSettlementsResponse {
  Settlements: Settlements | undefined;
}

export interface QueryAllSettlementsRequest {
  pagination: PageRequest | undefined;
}

export interface QueryAllSettlementsResponse {
  Settlements: Settlements[];
  pagination: PageResponse | undefined;
}

export interface QueryGetTwapRequest {
  priceDenom: string;
  assetDenom: string;
  contractAddr: string;
}

export interface QueryGetTwapResponse {
  twaps: Twap | undefined;
}

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

const baseQueryGetLongBookRequest: object = {
  id: 0,
  contractAddr: "",
  priceDenom: "",
  assetDenom: "",
};

export const QueryGetLongBookRequest = {
  encode(
    message: QueryGetLongBookRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.id !== 0) {
      writer.uint32(8).uint64(message.id);
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

  decode(input: Reader | Uint8Array, length?: number): QueryGetLongBookRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetLongBookRequest,
    } as QueryGetLongBookRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.id = longToNumber(reader.uint64() as Long);
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

  fromJSON(object: any): QueryGetLongBookRequest {
    const message = {
      ...baseQueryGetLongBookRequest,
    } as QueryGetLongBookRequest;
    if (object.id !== undefined && object.id !== null) {
      message.id = Number(object.id);
    } else {
      message.id = 0;
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    if (object.priceDenom !== undefined && object.priceDenom !== null) {
      message.priceDenom = String(object.priceDenom);
    } else {
      message.priceDenom = "";
    }
    if (object.assetDenom !== undefined && object.assetDenom !== null) {
      message.assetDenom = String(object.assetDenom);
    } else {
      message.assetDenom = "";
    }
    return message;
  },

  toJSON(message: QueryGetLongBookRequest): unknown {
    const obj: any = {};
    message.id !== undefined && (obj.id = message.id);
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetLongBookRequest>
  ): QueryGetLongBookRequest {
    const message = {
      ...baseQueryGetLongBookRequest,
    } as QueryGetLongBookRequest;
    if (object.id !== undefined && object.id !== null) {
      message.id = object.id;
    } else {
      message.id = 0;
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    if (object.priceDenom !== undefined && object.priceDenom !== null) {
      message.priceDenom = object.priceDenom;
    } else {
      message.priceDenom = "";
    }
    if (object.assetDenom !== undefined && object.assetDenom !== null) {
      message.assetDenom = object.assetDenom;
    } else {
      message.assetDenom = "";
    }
    return message;
  },
};

const baseQueryGetLongBookResponse: object = {};

export const QueryGetLongBookResponse = {
  encode(
    message: QueryGetLongBookResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.LongBook !== undefined) {
      LongBook.encode(message.LongBook, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryGetLongBookResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetLongBookResponse,
    } as QueryGetLongBookResponse;
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

  fromJSON(object: any): QueryGetLongBookResponse {
    const message = {
      ...baseQueryGetLongBookResponse,
    } as QueryGetLongBookResponse;
    if (object.LongBook !== undefined && object.LongBook !== null) {
      message.LongBook = LongBook.fromJSON(object.LongBook);
    } else {
      message.LongBook = undefined;
    }
    return message;
  },

  toJSON(message: QueryGetLongBookResponse): unknown {
    const obj: any = {};
    message.LongBook !== undefined &&
      (obj.LongBook = message.LongBook
        ? LongBook.toJSON(message.LongBook)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetLongBookResponse>
  ): QueryGetLongBookResponse {
    const message = {
      ...baseQueryGetLongBookResponse,
    } as QueryGetLongBookResponse;
    if (object.LongBook !== undefined && object.LongBook !== null) {
      message.LongBook = LongBook.fromPartial(object.LongBook);
    } else {
      message.LongBook = undefined;
    }
    return message;
  },
};

const baseQueryAllLongBookRequest: object = {};

export const QueryAllLongBookRequest = {
  encode(
    message: QueryAllLongBookRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryAllLongBookRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryAllLongBookRequest,
    } as QueryAllLongBookRequest;
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

  fromJSON(object: any): QueryAllLongBookRequest {
    const message = {
      ...baseQueryAllLongBookRequest,
    } as QueryAllLongBookRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryAllLongBookRequest): unknown {
    const obj: any = {};
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryAllLongBookRequest>
  ): QueryAllLongBookRequest {
    const message = {
      ...baseQueryAllLongBookRequest,
    } as QueryAllLongBookRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryAllLongBookResponse: object = {};

export const QueryAllLongBookResponse = {
  encode(
    message: QueryAllLongBookResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.LongBook) {
      LongBook.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.pagination !== undefined) {
      PageResponse.encode(
        message.pagination,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryAllLongBookResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryAllLongBookResponse,
    } as QueryAllLongBookResponse;
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

  fromJSON(object: any): QueryAllLongBookResponse {
    const message = {
      ...baseQueryAllLongBookResponse,
    } as QueryAllLongBookResponse;
    message.LongBook = [];
    if (object.LongBook !== undefined && object.LongBook !== null) {
      for (const e of object.LongBook) {
        message.LongBook.push(LongBook.fromJSON(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryAllLongBookResponse): unknown {
    const obj: any = {};
    if (message.LongBook) {
      obj.LongBook = message.LongBook.map((e) =>
        e ? LongBook.toJSON(e) : undefined
      );
    } else {
      obj.LongBook = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryAllLongBookResponse>
  ): QueryAllLongBookResponse {
    const message = {
      ...baseQueryAllLongBookResponse,
    } as QueryAllLongBookResponse;
    message.LongBook = [];
    if (object.LongBook !== undefined && object.LongBook !== null) {
      for (const e of object.LongBook) {
        message.LongBook.push(LongBook.fromPartial(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryGetShortBookRequest: object = {
  id: 0,
  contractAddr: "",
  priceDenom: "",
  assetDenom: "",
};

export const QueryGetShortBookRequest = {
  encode(
    message: QueryGetShortBookRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.id !== 0) {
      writer.uint32(8).uint64(message.id);
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

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryGetShortBookRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetShortBookRequest,
    } as QueryGetShortBookRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.id = longToNumber(reader.uint64() as Long);
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

  fromJSON(object: any): QueryGetShortBookRequest {
    const message = {
      ...baseQueryGetShortBookRequest,
    } as QueryGetShortBookRequest;
    if (object.id !== undefined && object.id !== null) {
      message.id = Number(object.id);
    } else {
      message.id = 0;
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    if (object.priceDenom !== undefined && object.priceDenom !== null) {
      message.priceDenom = String(object.priceDenom);
    } else {
      message.priceDenom = "";
    }
    if (object.assetDenom !== undefined && object.assetDenom !== null) {
      message.assetDenom = String(object.assetDenom);
    } else {
      message.assetDenom = "";
    }
    return message;
  },

  toJSON(message: QueryGetShortBookRequest): unknown {
    const obj: any = {};
    message.id !== undefined && (obj.id = message.id);
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetShortBookRequest>
  ): QueryGetShortBookRequest {
    const message = {
      ...baseQueryGetShortBookRequest,
    } as QueryGetShortBookRequest;
    if (object.id !== undefined && object.id !== null) {
      message.id = object.id;
    } else {
      message.id = 0;
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    if (object.priceDenom !== undefined && object.priceDenom !== null) {
      message.priceDenom = object.priceDenom;
    } else {
      message.priceDenom = "";
    }
    if (object.assetDenom !== undefined && object.assetDenom !== null) {
      message.assetDenom = object.assetDenom;
    } else {
      message.assetDenom = "";
    }
    return message;
  },
};

const baseQueryGetShortBookResponse: object = {};

export const QueryGetShortBookResponse = {
  encode(
    message: QueryGetShortBookResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.ShortBook !== undefined) {
      ShortBook.encode(message.ShortBook, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryGetShortBookResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetShortBookResponse,
    } as QueryGetShortBookResponse;
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

  fromJSON(object: any): QueryGetShortBookResponse {
    const message = {
      ...baseQueryGetShortBookResponse,
    } as QueryGetShortBookResponse;
    if (object.ShortBook !== undefined && object.ShortBook !== null) {
      message.ShortBook = ShortBook.fromJSON(object.ShortBook);
    } else {
      message.ShortBook = undefined;
    }
    return message;
  },

  toJSON(message: QueryGetShortBookResponse): unknown {
    const obj: any = {};
    message.ShortBook !== undefined &&
      (obj.ShortBook = message.ShortBook
        ? ShortBook.toJSON(message.ShortBook)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetShortBookResponse>
  ): QueryGetShortBookResponse {
    const message = {
      ...baseQueryGetShortBookResponse,
    } as QueryGetShortBookResponse;
    if (object.ShortBook !== undefined && object.ShortBook !== null) {
      message.ShortBook = ShortBook.fromPartial(object.ShortBook);
    } else {
      message.ShortBook = undefined;
    }
    return message;
  },
};

const baseQueryAllShortBookRequest: object = {};

export const QueryAllShortBookRequest = {
  encode(
    message: QueryAllShortBookRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryAllShortBookRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryAllShortBookRequest,
    } as QueryAllShortBookRequest;
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

  fromJSON(object: any): QueryAllShortBookRequest {
    const message = {
      ...baseQueryAllShortBookRequest,
    } as QueryAllShortBookRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryAllShortBookRequest): unknown {
    const obj: any = {};
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryAllShortBookRequest>
  ): QueryAllShortBookRequest {
    const message = {
      ...baseQueryAllShortBookRequest,
    } as QueryAllShortBookRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryAllShortBookResponse: object = {};

export const QueryAllShortBookResponse = {
  encode(
    message: QueryAllShortBookResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.ShortBook) {
      ShortBook.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.pagination !== undefined) {
      PageResponse.encode(
        message.pagination,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryAllShortBookResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryAllShortBookResponse,
    } as QueryAllShortBookResponse;
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

  fromJSON(object: any): QueryAllShortBookResponse {
    const message = {
      ...baseQueryAllShortBookResponse,
    } as QueryAllShortBookResponse;
    message.ShortBook = [];
    if (object.ShortBook !== undefined && object.ShortBook !== null) {
      for (const e of object.ShortBook) {
        message.ShortBook.push(ShortBook.fromJSON(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryAllShortBookResponse): unknown {
    const obj: any = {};
    if (message.ShortBook) {
      obj.ShortBook = message.ShortBook.map((e) =>
        e ? ShortBook.toJSON(e) : undefined
      );
    } else {
      obj.ShortBook = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryAllShortBookResponse>
  ): QueryAllShortBookResponse {
    const message = {
      ...baseQueryAllShortBookResponse,
    } as QueryAllShortBookResponse;
    message.ShortBook = [];
    if (object.ShortBook !== undefined && object.ShortBook !== null) {
      for (const e of object.ShortBook) {
        message.ShortBook.push(ShortBook.fromPartial(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryGetSettlementsRequest: object = {
  contractAddr: "",
  blockHeight: 0,
  priceDenom: "",
  assetDenom: "",
};

export const QueryGetSettlementsRequest = {
  encode(
    message: QueryGetSettlementsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.contractAddr !== "") {
      writer.uint32(10).string(message.contractAddr);
    }
    if (message.blockHeight !== 0) {
      writer.uint32(16).uint64(message.blockHeight);
    }
    if (message.priceDenom !== "") {
      writer.uint32(26).string(message.priceDenom);
    }
    if (message.assetDenom !== "") {
      writer.uint32(34).string(message.assetDenom);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryGetSettlementsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetSettlementsRequest,
    } as QueryGetSettlementsRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.contractAddr = reader.string();
          break;
        case 2:
          message.blockHeight = longToNumber(reader.uint64() as Long);
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

  fromJSON(object: any): QueryGetSettlementsRequest {
    const message = {
      ...baseQueryGetSettlementsRequest,
    } as QueryGetSettlementsRequest;
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    if (object.blockHeight !== undefined && object.blockHeight !== null) {
      message.blockHeight = Number(object.blockHeight);
    } else {
      message.blockHeight = 0;
    }
    if (object.priceDenom !== undefined && object.priceDenom !== null) {
      message.priceDenom = String(object.priceDenom);
    } else {
      message.priceDenom = "";
    }
    if (object.assetDenom !== undefined && object.assetDenom !== null) {
      message.assetDenom = String(object.assetDenom);
    } else {
      message.assetDenom = "";
    }
    return message;
  },

  toJSON(message: QueryGetSettlementsRequest): unknown {
    const obj: any = {};
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    message.blockHeight !== undefined &&
      (obj.blockHeight = message.blockHeight);
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetSettlementsRequest>
  ): QueryGetSettlementsRequest {
    const message = {
      ...baseQueryGetSettlementsRequest,
    } as QueryGetSettlementsRequest;
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    if (object.blockHeight !== undefined && object.blockHeight !== null) {
      message.blockHeight = object.blockHeight;
    } else {
      message.blockHeight = 0;
    }
    if (object.priceDenom !== undefined && object.priceDenom !== null) {
      message.priceDenom = object.priceDenom;
    } else {
      message.priceDenom = "";
    }
    if (object.assetDenom !== undefined && object.assetDenom !== null) {
      message.assetDenom = object.assetDenom;
    } else {
      message.assetDenom = "";
    }
    return message;
  },
};

const baseQueryGetSettlementsResponse: object = {};

export const QueryGetSettlementsResponse = {
  encode(
    message: QueryGetSettlementsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.Settlements !== undefined) {
      Settlements.encode(
        message.Settlements,
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryGetSettlementsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetSettlementsResponse,
    } as QueryGetSettlementsResponse;
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

  fromJSON(object: any): QueryGetSettlementsResponse {
    const message = {
      ...baseQueryGetSettlementsResponse,
    } as QueryGetSettlementsResponse;
    if (object.Settlements !== undefined && object.Settlements !== null) {
      message.Settlements = Settlements.fromJSON(object.Settlements);
    } else {
      message.Settlements = undefined;
    }
    return message;
  },

  toJSON(message: QueryGetSettlementsResponse): unknown {
    const obj: any = {};
    message.Settlements !== undefined &&
      (obj.Settlements = message.Settlements
        ? Settlements.toJSON(message.Settlements)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetSettlementsResponse>
  ): QueryGetSettlementsResponse {
    const message = {
      ...baseQueryGetSettlementsResponse,
    } as QueryGetSettlementsResponse;
    if (object.Settlements !== undefined && object.Settlements !== null) {
      message.Settlements = Settlements.fromPartial(object.Settlements);
    } else {
      message.Settlements = undefined;
    }
    return message;
  },
};

const baseQueryAllSettlementsRequest: object = {};

export const QueryAllSettlementsRequest = {
  encode(
    message: QueryAllSettlementsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryAllSettlementsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryAllSettlementsRequest,
    } as QueryAllSettlementsRequest;
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

  fromJSON(object: any): QueryAllSettlementsRequest {
    const message = {
      ...baseQueryAllSettlementsRequest,
    } as QueryAllSettlementsRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryAllSettlementsRequest): unknown {
    const obj: any = {};
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryAllSettlementsRequest>
  ): QueryAllSettlementsRequest {
    const message = {
      ...baseQueryAllSettlementsRequest,
    } as QueryAllSettlementsRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryAllSettlementsResponse: object = {};

export const QueryAllSettlementsResponse = {
  encode(
    message: QueryAllSettlementsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.Settlements) {
      Settlements.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.pagination !== undefined) {
      PageResponse.encode(
        message.pagination,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryAllSettlementsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryAllSettlementsResponse,
    } as QueryAllSettlementsResponse;
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

  fromJSON(object: any): QueryAllSettlementsResponse {
    const message = {
      ...baseQueryAllSettlementsResponse,
    } as QueryAllSettlementsResponse;
    message.Settlements = [];
    if (object.Settlements !== undefined && object.Settlements !== null) {
      for (const e of object.Settlements) {
        message.Settlements.push(Settlements.fromJSON(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryAllSettlementsResponse): unknown {
    const obj: any = {};
    if (message.Settlements) {
      obj.Settlements = message.Settlements.map((e) =>
        e ? Settlements.toJSON(e) : undefined
      );
    } else {
      obj.Settlements = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryAllSettlementsResponse>
  ): QueryAllSettlementsResponse {
    const message = {
      ...baseQueryAllSettlementsResponse,
    } as QueryAllSettlementsResponse;
    message.Settlements = [];
    if (object.Settlements !== undefined && object.Settlements !== null) {
      for (const e of object.Settlements) {
        message.Settlements.push(Settlements.fromPartial(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryGetTwapRequest: object = {
  priceDenom: "",
  assetDenom: "",
  contractAddr: "",
};

export const QueryGetTwapRequest = {
  encode(
    message: QueryGetTwapRequest,
    writer: Writer = Writer.create()
  ): Writer {
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

  decode(input: Reader | Uint8Array, length?: number): QueryGetTwapRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryGetTwapRequest } as QueryGetTwapRequest;
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

  fromJSON(object: any): QueryGetTwapRequest {
    const message = { ...baseQueryGetTwapRequest } as QueryGetTwapRequest;
    if (object.priceDenom !== undefined && object.priceDenom !== null) {
      message.priceDenom = String(object.priceDenom);
    } else {
      message.priceDenom = "";
    }
    if (object.assetDenom !== undefined && object.assetDenom !== null) {
      message.assetDenom = String(object.assetDenom);
    } else {
      message.assetDenom = "";
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    return message;
  },

  toJSON(message: QueryGetTwapRequest): unknown {
    const obj: any = {};
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryGetTwapRequest>): QueryGetTwapRequest {
    const message = { ...baseQueryGetTwapRequest } as QueryGetTwapRequest;
    if (object.priceDenom !== undefined && object.priceDenom !== null) {
      message.priceDenom = object.priceDenom;
    } else {
      message.priceDenom = "";
    }
    if (object.assetDenom !== undefined && object.assetDenom !== null) {
      message.assetDenom = object.assetDenom;
    } else {
      message.assetDenom = "";
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    return message;
  },
};

const baseQueryGetTwapResponse: object = {};

export const QueryGetTwapResponse = {
  encode(
    message: QueryGetTwapResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.twaps !== undefined) {
      Twap.encode(message.twaps, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryGetTwapResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryGetTwapResponse } as QueryGetTwapResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.twaps = Twap.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryGetTwapResponse {
    const message = { ...baseQueryGetTwapResponse } as QueryGetTwapResponse;
    if (object.twaps !== undefined && object.twaps !== null) {
      message.twaps = Twap.fromJSON(object.twaps);
    } else {
      message.twaps = undefined;
    }
    return message;
  },

  toJSON(message: QueryGetTwapResponse): unknown {
    const obj: any = {};
    message.twaps !== undefined &&
      (obj.twaps = message.twaps ? Twap.toJSON(message.twaps) : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryGetTwapResponse>): QueryGetTwapResponse {
    const message = { ...baseQueryGetTwapResponse } as QueryGetTwapResponse;
    if (object.twaps !== undefined && object.twaps !== null) {
      message.twaps = Twap.fromPartial(object.twaps);
    } else {
      message.twaps = undefined;
    }
    return message;
  },
};

/** Query defines the gRPC querier service. */
export interface Query {
  /** Parameters queries the parameters of the module. */
  Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
  /** Queries a LongBook by id. */
  LongBook(request: QueryGetLongBookRequest): Promise<QueryGetLongBookResponse>;
  /** Queries a list of LongBook items. */
  LongBookAll(
    request: QueryAllLongBookRequest
  ): Promise<QueryAllLongBookResponse>;
  /** Queries a ShortBook by id. */
  ShortBook(
    request: QueryGetShortBookRequest
  ): Promise<QueryGetShortBookResponse>;
  /** Queries a list of ShortBook items. */
  ShortBookAll(
    request: QueryAllShortBookRequest
  ): Promise<QueryAllShortBookResponse>;
  SettlementsAll(
    request: QueryAllSettlementsRequest
  ): Promise<QueryAllSettlementsResponse>;
  /** Queries a list of GetTwap items. */
  GetTwap(request: QueryGetTwapRequest): Promise<QueryGetTwapResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  Params(request: QueryParamsRequest): Promise<QueryParamsResponse> {
    const data = QueryParamsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.legacy.dex.v0.Query",
      "Params",
      data
    );
    return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
  }

  LongBook(
    request: QueryGetLongBookRequest
  ): Promise<QueryGetLongBookResponse> {
    const data = QueryGetLongBookRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.legacy.dex.v0.Query",
      "LongBook",
      data
    );
    return promise.then((data) =>
      QueryGetLongBookResponse.decode(new Reader(data))
    );
  }

  LongBookAll(
    request: QueryAllLongBookRequest
  ): Promise<QueryAllLongBookResponse> {
    const data = QueryAllLongBookRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.legacy.dex.v0.Query",
      "LongBookAll",
      data
    );
    return promise.then((data) =>
      QueryAllLongBookResponse.decode(new Reader(data))
    );
  }

  ShortBook(
    request: QueryGetShortBookRequest
  ): Promise<QueryGetShortBookResponse> {
    const data = QueryGetShortBookRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.legacy.dex.v0.Query",
      "ShortBook",
      data
    );
    return promise.then((data) =>
      QueryGetShortBookResponse.decode(new Reader(data))
    );
  }

  ShortBookAll(
    request: QueryAllShortBookRequest
  ): Promise<QueryAllShortBookResponse> {
    const data = QueryAllShortBookRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.legacy.dex.v0.Query",
      "ShortBookAll",
      data
    );
    return promise.then((data) =>
      QueryAllShortBookResponse.decode(new Reader(data))
    );
  }

  SettlementsAll(
    request: QueryAllSettlementsRequest
  ): Promise<QueryAllSettlementsResponse> {
    const data = QueryAllSettlementsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.legacy.dex.v0.Query",
      "SettlementsAll",
      data
    );
    return promise.then((data) =>
      QueryAllSettlementsResponse.decode(new Reader(data))
    );
  }

  GetTwap(request: QueryGetTwapRequest): Promise<QueryGetTwapResponse> {
    const data = QueryGetTwapRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.legacy.dex.v0.Query",
      "GetTwap",
      data
    );
    return promise.then((data) =>
      QueryGetTwapResponse.decode(new Reader(data))
    );
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
