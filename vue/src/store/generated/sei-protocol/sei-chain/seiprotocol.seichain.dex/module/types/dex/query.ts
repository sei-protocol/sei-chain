/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import { Params } from "../dex/params";
import { LongBook } from "../dex/long_book";
import {
  PageRequest,
  PageResponse,
} from "../cosmos/base/query/v1beta1/pagination";
import { ShortBook } from "../dex/short_book";
import { Settlements } from "../dex/settlement";
import { Price, PriceCandlestick } from "../dex/price";
import { Twap } from "../dex/twap";
import { AssetMetadata } from "../dex/asset_list";
import { Pair } from "../dex/pair";
import { Order } from "../dex/order";

export const protobufPackage = "seiprotocol.seichain.dex";

/** QueryParamsRequest is request type for the Query/Params RPC method. */
export interface QueryParamsRequest {}

/** QueryParamsResponse is response type for the Query/Params RPC method. */
export interface QueryParamsResponse {
  /** params holds all the parameters of this module. */
  params: Params | undefined;
}

export interface QueryGetLongBookRequest {
  price: string;
  contractAddr: string;
  priceDenom: string;
  assetDenom: string;
}

export interface QueryGetLongBookResponse {
  LongBook: LongBook | undefined;
}

export interface QueryAllLongBookRequest {
  pagination: PageRequest | undefined;
  contractAddr: string;
  priceDenom: string;
  assetDenom: string;
}

export interface QueryAllLongBookResponse {
  LongBook: LongBook[];
  pagination: PageResponse | undefined;
}

export interface QueryGetShortBookRequest {
  price: string;
  contractAddr: string;
  priceDenom: string;
  assetDenom: string;
}

export interface QueryGetShortBookResponse {
  ShortBook: ShortBook | undefined;
}

export interface QueryAllShortBookRequest {
  pagination: PageRequest | undefined;
  contractAddr: string;
  priceDenom: string;
  assetDenom: string;
}

export interface QueryAllShortBookResponse {
  ShortBook: ShortBook[];
  pagination: PageResponse | undefined;
}

export interface QueryGetSettlementsRequest {
  contractAddr: string;
  orderId: number;
  priceDenom: string;
  assetDenom: string;
}

export interface QueryGetSettlementsResponse {
  Settlements: Settlements | undefined;
}

export interface QueryGetPricesRequest {
  priceDenom: string;
  assetDenom: string;
  contractAddr: string;
}

export interface QueryGetPricesResponse {
  prices: Price[];
}

export interface QueryGetTwapsRequest {
  contractAddr: string;
  lookbackSeconds: number;
}

export interface QueryGetTwapsResponse {
  twaps: Twap[];
}

export interface QueryAssetListRequest {}

export interface QueryAssetListResponse {
  assetList: AssetMetadata[];
}

export interface QueryAssetMetadataRequest {
  denom: string;
}

export interface QueryAssetMetadataResponse {
  metadata: AssetMetadata | undefined;
}

export interface QueryRegisteredPairsRequest {
  contractAddr: string;
}

export interface QueryRegisteredPairsResponse {
  pairs: Pair[];
}

export interface QueryGetOrdersRequest {
  contractAddr: string;
  account: string;
}

export interface QueryGetOrdersResponse {
  orders: Order[];
}

export interface QueryGetOrderByIDRequest {
  contractAddr: string;
  priceDenom: string;
  assetDenom: string;
  id: number;
}

export interface QueryGetOrderByIDResponse {
  order: Order | undefined;
}

export interface QueryGetHistoricalPricesRequest {
  contractAddr: string;
  priceDenom: string;
  assetDenom: string;
  periodLengthInSeconds: number;
  numOfPeriods: number;
}

export interface QueryGetHistoricalPricesResponse {
  prices: PriceCandlestick[];
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
  price: "",
  contractAddr: "",
  priceDenom: "",
  assetDenom: "",
};

export const QueryGetLongBookRequest = {
  encode(
    message: QueryGetLongBookRequest,
    writer: Writer = Writer.create()
  ): Writer {
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

  fromJSON(object: any): QueryGetLongBookRequest {
    const message = {
      ...baseQueryGetLongBookRequest,
    } as QueryGetLongBookRequest;
    if (object.price !== undefined && object.price !== null) {
      message.price = String(object.price);
    } else {
      message.price = "";
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
    message.price !== undefined && (obj.price = message.price);
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
    if (object.price !== undefined && object.price !== null) {
      message.price = object.price;
    } else {
      message.price = "";
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

const baseQueryAllLongBookRequest: object = {
  contractAddr: "",
  priceDenom: "",
  assetDenom: "",
};

export const QueryAllLongBookRequest = {
  encode(
    message: QueryAllLongBookRequest,
    writer: Writer = Writer.create()
  ): Writer {
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

  fromJSON(object: any): QueryAllLongBookRequest {
    const message = {
      ...baseQueryAllLongBookRequest,
    } as QueryAllLongBookRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
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

  toJSON(message: QueryAllLongBookRequest): unknown {
    const obj: any = {};
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
  price: "",
  contractAddr: "",
  priceDenom: "",
  assetDenom: "",
};

export const QueryGetShortBookRequest = {
  encode(
    message: QueryGetShortBookRequest,
    writer: Writer = Writer.create()
  ): Writer {
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

  fromJSON(object: any): QueryGetShortBookRequest {
    const message = {
      ...baseQueryGetShortBookRequest,
    } as QueryGetShortBookRequest;
    if (object.price !== undefined && object.price !== null) {
      message.price = String(object.price);
    } else {
      message.price = "";
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
    message.price !== undefined && (obj.price = message.price);
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
    if (object.price !== undefined && object.price !== null) {
      message.price = object.price;
    } else {
      message.price = "";
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

const baseQueryAllShortBookRequest: object = {
  contractAddr: "",
  priceDenom: "",
  assetDenom: "",
};

export const QueryAllShortBookRequest = {
  encode(
    message: QueryAllShortBookRequest,
    writer: Writer = Writer.create()
  ): Writer {
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

  fromJSON(object: any): QueryAllShortBookRequest {
    const message = {
      ...baseQueryAllShortBookRequest,
    } as QueryAllShortBookRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
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

  toJSON(message: QueryAllShortBookRequest): unknown {
    const obj: any = {};
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
  orderId: 0,
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
    if (message.orderId !== 0) {
      writer.uint32(16).uint64(message.orderId);
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
          message.orderId = longToNumber(reader.uint64() as Long);
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
    if (object.orderId !== undefined && object.orderId !== null) {
      message.orderId = Number(object.orderId);
    } else {
      message.orderId = 0;
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
    message.orderId !== undefined && (obj.orderId = message.orderId);
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
    if (object.orderId !== undefined && object.orderId !== null) {
      message.orderId = object.orderId;
    } else {
      message.orderId = 0;
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

const baseQueryGetPricesRequest: object = {
  priceDenom: "",
  assetDenom: "",
  contractAddr: "",
};

export const QueryGetPricesRequest = {
  encode(
    message: QueryGetPricesRequest,
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

  decode(input: Reader | Uint8Array, length?: number): QueryGetPricesRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryGetPricesRequest } as QueryGetPricesRequest;
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

  fromJSON(object: any): QueryGetPricesRequest {
    const message = { ...baseQueryGetPricesRequest } as QueryGetPricesRequest;
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

  toJSON(message: QueryGetPricesRequest): unknown {
    const obj: any = {};
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetPricesRequest>
  ): QueryGetPricesRequest {
    const message = { ...baseQueryGetPricesRequest } as QueryGetPricesRequest;
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

const baseQueryGetPricesResponse: object = {};

export const QueryGetPricesResponse = {
  encode(
    message: QueryGetPricesResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.prices) {
      Price.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryGetPricesResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryGetPricesResponse } as QueryGetPricesResponse;
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

  fromJSON(object: any): QueryGetPricesResponse {
    const message = { ...baseQueryGetPricesResponse } as QueryGetPricesResponse;
    message.prices = [];
    if (object.prices !== undefined && object.prices !== null) {
      for (const e of object.prices) {
        message.prices.push(Price.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: QueryGetPricesResponse): unknown {
    const obj: any = {};
    if (message.prices) {
      obj.prices = message.prices.map((e) => (e ? Price.toJSON(e) : undefined));
    } else {
      obj.prices = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetPricesResponse>
  ): QueryGetPricesResponse {
    const message = { ...baseQueryGetPricesResponse } as QueryGetPricesResponse;
    message.prices = [];
    if (object.prices !== undefined && object.prices !== null) {
      for (const e of object.prices) {
        message.prices.push(Price.fromPartial(e));
      }
    }
    return message;
  },
};

const baseQueryGetTwapsRequest: object = {
  contractAddr: "",
  lookbackSeconds: 0,
};

export const QueryGetTwapsRequest = {
  encode(
    message: QueryGetTwapsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.contractAddr !== "") {
      writer.uint32(10).string(message.contractAddr);
    }
    if (message.lookbackSeconds !== 0) {
      writer.uint32(16).uint64(message.lookbackSeconds);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryGetTwapsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryGetTwapsRequest } as QueryGetTwapsRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.contractAddr = reader.string();
          break;
        case 2:
          message.lookbackSeconds = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryGetTwapsRequest {
    const message = { ...baseQueryGetTwapsRequest } as QueryGetTwapsRequest;
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
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

  toJSON(message: QueryGetTwapsRequest): unknown {
    const obj: any = {};
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    message.lookbackSeconds !== undefined &&
      (obj.lookbackSeconds = message.lookbackSeconds);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryGetTwapsRequest>): QueryGetTwapsRequest {
    const message = { ...baseQueryGetTwapsRequest } as QueryGetTwapsRequest;
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
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

const baseQueryGetTwapsResponse: object = {};

export const QueryGetTwapsResponse = {
  encode(
    message: QueryGetTwapsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.twaps) {
      Twap.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryGetTwapsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryGetTwapsResponse } as QueryGetTwapsResponse;
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

  fromJSON(object: any): QueryGetTwapsResponse {
    const message = { ...baseQueryGetTwapsResponse } as QueryGetTwapsResponse;
    message.twaps = [];
    if (object.twaps !== undefined && object.twaps !== null) {
      for (const e of object.twaps) {
        message.twaps.push(Twap.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: QueryGetTwapsResponse): unknown {
    const obj: any = {};
    if (message.twaps) {
      obj.twaps = message.twaps.map((e) => (e ? Twap.toJSON(e) : undefined));
    } else {
      obj.twaps = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetTwapsResponse>
  ): QueryGetTwapsResponse {
    const message = { ...baseQueryGetTwapsResponse } as QueryGetTwapsResponse;
    message.twaps = [];
    if (object.twaps !== undefined && object.twaps !== null) {
      for (const e of object.twaps) {
        message.twaps.push(Twap.fromPartial(e));
      }
    }
    return message;
  },
};

const baseQueryAssetListRequest: object = {};

export const QueryAssetListRequest = {
  encode(_: QueryAssetListRequest, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryAssetListRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryAssetListRequest } as QueryAssetListRequest;
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

  fromJSON(_: any): QueryAssetListRequest {
    const message = { ...baseQueryAssetListRequest } as QueryAssetListRequest;
    return message;
  },

  toJSON(_: QueryAssetListRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(_: DeepPartial<QueryAssetListRequest>): QueryAssetListRequest {
    const message = { ...baseQueryAssetListRequest } as QueryAssetListRequest;
    return message;
  },
};

const baseQueryAssetListResponse: object = {};

export const QueryAssetListResponse = {
  encode(
    message: QueryAssetListResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.assetList) {
      AssetMetadata.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryAssetListResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryAssetListResponse } as QueryAssetListResponse;
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

  fromJSON(object: any): QueryAssetListResponse {
    const message = { ...baseQueryAssetListResponse } as QueryAssetListResponse;
    message.assetList = [];
    if (object.assetList !== undefined && object.assetList !== null) {
      for (const e of object.assetList) {
        message.assetList.push(AssetMetadata.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: QueryAssetListResponse): unknown {
    const obj: any = {};
    if (message.assetList) {
      obj.assetList = message.assetList.map((e) =>
        e ? AssetMetadata.toJSON(e) : undefined
      );
    } else {
      obj.assetList = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryAssetListResponse>
  ): QueryAssetListResponse {
    const message = { ...baseQueryAssetListResponse } as QueryAssetListResponse;
    message.assetList = [];
    if (object.assetList !== undefined && object.assetList !== null) {
      for (const e of object.assetList) {
        message.assetList.push(AssetMetadata.fromPartial(e));
      }
    }
    return message;
  },
};

const baseQueryAssetMetadataRequest: object = { denom: "" };

export const QueryAssetMetadataRequest = {
  encode(
    message: QueryAssetMetadataRequest,
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
  ): QueryAssetMetadataRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryAssetMetadataRequest,
    } as QueryAssetMetadataRequest;
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

  fromJSON(object: any): QueryAssetMetadataRequest {
    const message = {
      ...baseQueryAssetMetadataRequest,
    } as QueryAssetMetadataRequest;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = String(object.denom);
    } else {
      message.denom = "";
    }
    return message;
  },

  toJSON(message: QueryAssetMetadataRequest): unknown {
    const obj: any = {};
    message.denom !== undefined && (obj.denom = message.denom);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryAssetMetadataRequest>
  ): QueryAssetMetadataRequest {
    const message = {
      ...baseQueryAssetMetadataRequest,
    } as QueryAssetMetadataRequest;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = object.denom;
    } else {
      message.denom = "";
    }
    return message;
  },
};

const baseQueryAssetMetadataResponse: object = {};

export const QueryAssetMetadataResponse = {
  encode(
    message: QueryAssetMetadataResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.metadata !== undefined) {
      AssetMetadata.encode(message.metadata, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryAssetMetadataResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryAssetMetadataResponse,
    } as QueryAssetMetadataResponse;
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

  fromJSON(object: any): QueryAssetMetadataResponse {
    const message = {
      ...baseQueryAssetMetadataResponse,
    } as QueryAssetMetadataResponse;
    if (object.metadata !== undefined && object.metadata !== null) {
      message.metadata = AssetMetadata.fromJSON(object.metadata);
    } else {
      message.metadata = undefined;
    }
    return message;
  },

  toJSON(message: QueryAssetMetadataResponse): unknown {
    const obj: any = {};
    message.metadata !== undefined &&
      (obj.metadata = message.metadata
        ? AssetMetadata.toJSON(message.metadata)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryAssetMetadataResponse>
  ): QueryAssetMetadataResponse {
    const message = {
      ...baseQueryAssetMetadataResponse,
    } as QueryAssetMetadataResponse;
    if (object.metadata !== undefined && object.metadata !== null) {
      message.metadata = AssetMetadata.fromPartial(object.metadata);
    } else {
      message.metadata = undefined;
    }
    return message;
  },
};

const baseQueryRegisteredPairsRequest: object = { contractAddr: "" };

export const QueryRegisteredPairsRequest = {
  encode(
    message: QueryRegisteredPairsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.contractAddr !== "") {
      writer.uint32(10).string(message.contractAddr);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryRegisteredPairsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryRegisteredPairsRequest,
    } as QueryRegisteredPairsRequest;
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

  fromJSON(object: any): QueryRegisteredPairsRequest {
    const message = {
      ...baseQueryRegisteredPairsRequest,
    } as QueryRegisteredPairsRequest;
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    return message;
  },

  toJSON(message: QueryRegisteredPairsRequest): unknown {
    const obj: any = {};
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryRegisteredPairsRequest>
  ): QueryRegisteredPairsRequest {
    const message = {
      ...baseQueryRegisteredPairsRequest,
    } as QueryRegisteredPairsRequest;
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    return message;
  },
};

const baseQueryRegisteredPairsResponse: object = {};

export const QueryRegisteredPairsResponse = {
  encode(
    message: QueryRegisteredPairsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.pairs) {
      Pair.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryRegisteredPairsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryRegisteredPairsResponse,
    } as QueryRegisteredPairsResponse;
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

  fromJSON(object: any): QueryRegisteredPairsResponse {
    const message = {
      ...baseQueryRegisteredPairsResponse,
    } as QueryRegisteredPairsResponse;
    message.pairs = [];
    if (object.pairs !== undefined && object.pairs !== null) {
      for (const e of object.pairs) {
        message.pairs.push(Pair.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: QueryRegisteredPairsResponse): unknown {
    const obj: any = {};
    if (message.pairs) {
      obj.pairs = message.pairs.map((e) => (e ? Pair.toJSON(e) : undefined));
    } else {
      obj.pairs = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryRegisteredPairsResponse>
  ): QueryRegisteredPairsResponse {
    const message = {
      ...baseQueryRegisteredPairsResponse,
    } as QueryRegisteredPairsResponse;
    message.pairs = [];
    if (object.pairs !== undefined && object.pairs !== null) {
      for (const e of object.pairs) {
        message.pairs.push(Pair.fromPartial(e));
      }
    }
    return message;
  },
};

const baseQueryGetOrdersRequest: object = { contractAddr: "", account: "" };

export const QueryGetOrdersRequest = {
  encode(
    message: QueryGetOrdersRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.contractAddr !== "") {
      writer.uint32(10).string(message.contractAddr);
    }
    if (message.account !== "") {
      writer.uint32(18).string(message.account);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryGetOrdersRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryGetOrdersRequest } as QueryGetOrdersRequest;
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

  fromJSON(object: any): QueryGetOrdersRequest {
    const message = { ...baseQueryGetOrdersRequest } as QueryGetOrdersRequest;
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    if (object.account !== undefined && object.account !== null) {
      message.account = String(object.account);
    } else {
      message.account = "";
    }
    return message;
  },

  toJSON(message: QueryGetOrdersRequest): unknown {
    const obj: any = {};
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    message.account !== undefined && (obj.account = message.account);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetOrdersRequest>
  ): QueryGetOrdersRequest {
    const message = { ...baseQueryGetOrdersRequest } as QueryGetOrdersRequest;
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    if (object.account !== undefined && object.account !== null) {
      message.account = object.account;
    } else {
      message.account = "";
    }
    return message;
  },
};

const baseQueryGetOrdersResponse: object = {};

export const QueryGetOrdersResponse = {
  encode(
    message: QueryGetOrdersResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.orders) {
      Order.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryGetOrdersResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryGetOrdersResponse } as QueryGetOrdersResponse;
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

  fromJSON(object: any): QueryGetOrdersResponse {
    const message = { ...baseQueryGetOrdersResponse } as QueryGetOrdersResponse;
    message.orders = [];
    if (object.orders !== undefined && object.orders !== null) {
      for (const e of object.orders) {
        message.orders.push(Order.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: QueryGetOrdersResponse): unknown {
    const obj: any = {};
    if (message.orders) {
      obj.orders = message.orders.map((e) => (e ? Order.toJSON(e) : undefined));
    } else {
      obj.orders = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetOrdersResponse>
  ): QueryGetOrdersResponse {
    const message = { ...baseQueryGetOrdersResponse } as QueryGetOrdersResponse;
    message.orders = [];
    if (object.orders !== undefined && object.orders !== null) {
      for (const e of object.orders) {
        message.orders.push(Order.fromPartial(e));
      }
    }
    return message;
  },
};

const baseQueryGetOrderByIDRequest: object = {
  contractAddr: "",
  priceDenom: "",
  assetDenom: "",
  id: 0,
};

export const QueryGetOrderByIDRequest = {
  encode(
    message: QueryGetOrderByIDRequest,
    writer: Writer = Writer.create()
  ): Writer {
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

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryGetOrderByIDRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetOrderByIDRequest,
    } as QueryGetOrderByIDRequest;
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
          message.id = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryGetOrderByIDRequest {
    const message = {
      ...baseQueryGetOrderByIDRequest,
    } as QueryGetOrderByIDRequest;
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
    if (object.id !== undefined && object.id !== null) {
      message.id = Number(object.id);
    } else {
      message.id = 0;
    }
    return message;
  },

  toJSON(message: QueryGetOrderByIDRequest): unknown {
    const obj: any = {};
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    message.id !== undefined && (obj.id = message.id);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetOrderByIDRequest>
  ): QueryGetOrderByIDRequest {
    const message = {
      ...baseQueryGetOrderByIDRequest,
    } as QueryGetOrderByIDRequest;
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
    if (object.id !== undefined && object.id !== null) {
      message.id = object.id;
    } else {
      message.id = 0;
    }
    return message;
  },
};

const baseQueryGetOrderByIDResponse: object = {};

export const QueryGetOrderByIDResponse = {
  encode(
    message: QueryGetOrderByIDResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.order !== undefined) {
      Order.encode(message.order, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryGetOrderByIDResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetOrderByIDResponse,
    } as QueryGetOrderByIDResponse;
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

  fromJSON(object: any): QueryGetOrderByIDResponse {
    const message = {
      ...baseQueryGetOrderByIDResponse,
    } as QueryGetOrderByIDResponse;
    if (object.order !== undefined && object.order !== null) {
      message.order = Order.fromJSON(object.order);
    } else {
      message.order = undefined;
    }
    return message;
  },

  toJSON(message: QueryGetOrderByIDResponse): unknown {
    const obj: any = {};
    message.order !== undefined &&
      (obj.order = message.order ? Order.toJSON(message.order) : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetOrderByIDResponse>
  ): QueryGetOrderByIDResponse {
    const message = {
      ...baseQueryGetOrderByIDResponse,
    } as QueryGetOrderByIDResponse;
    if (object.order !== undefined && object.order !== null) {
      message.order = Order.fromPartial(object.order);
    } else {
      message.order = undefined;
    }
    return message;
  },
};

const baseQueryGetHistoricalPricesRequest: object = {
  contractAddr: "",
  priceDenom: "",
  assetDenom: "",
  periodLengthInSeconds: 0,
  numOfPeriods: 0,
};

export const QueryGetHistoricalPricesRequest = {
  encode(
    message: QueryGetHistoricalPricesRequest,
    writer: Writer = Writer.create()
  ): Writer {
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

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryGetHistoricalPricesRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetHistoricalPricesRequest,
    } as QueryGetHistoricalPricesRequest;
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
          message.periodLengthInSeconds = longToNumber(reader.uint64() as Long);
          break;
        case 5:
          message.numOfPeriods = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryGetHistoricalPricesRequest {
    const message = {
      ...baseQueryGetHistoricalPricesRequest,
    } as QueryGetHistoricalPricesRequest;
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
    if (
      object.periodLengthInSeconds !== undefined &&
      object.periodLengthInSeconds !== null
    ) {
      message.periodLengthInSeconds = Number(object.periodLengthInSeconds);
    } else {
      message.periodLengthInSeconds = 0;
    }
    if (object.numOfPeriods !== undefined && object.numOfPeriods !== null) {
      message.numOfPeriods = Number(object.numOfPeriods);
    } else {
      message.numOfPeriods = 0;
    }
    return message;
  },

  toJSON(message: QueryGetHistoricalPricesRequest): unknown {
    const obj: any = {};
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

  fromPartial(
    object: DeepPartial<QueryGetHistoricalPricesRequest>
  ): QueryGetHistoricalPricesRequest {
    const message = {
      ...baseQueryGetHistoricalPricesRequest,
    } as QueryGetHistoricalPricesRequest;
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
    if (
      object.periodLengthInSeconds !== undefined &&
      object.periodLengthInSeconds !== null
    ) {
      message.periodLengthInSeconds = object.periodLengthInSeconds;
    } else {
      message.periodLengthInSeconds = 0;
    }
    if (object.numOfPeriods !== undefined && object.numOfPeriods !== null) {
      message.numOfPeriods = object.numOfPeriods;
    } else {
      message.numOfPeriods = 0;
    }
    return message;
  },
};

const baseQueryGetHistoricalPricesResponse: object = {};

export const QueryGetHistoricalPricesResponse = {
  encode(
    message: QueryGetHistoricalPricesResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.prices) {
      PriceCandlestick.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryGetHistoricalPricesResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetHistoricalPricesResponse,
    } as QueryGetHistoricalPricesResponse;
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

  fromJSON(object: any): QueryGetHistoricalPricesResponse {
    const message = {
      ...baseQueryGetHistoricalPricesResponse,
    } as QueryGetHistoricalPricesResponse;
    message.prices = [];
    if (object.prices !== undefined && object.prices !== null) {
      for (const e of object.prices) {
        message.prices.push(PriceCandlestick.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: QueryGetHistoricalPricesResponse): unknown {
    const obj: any = {};
    if (message.prices) {
      obj.prices = message.prices.map((e) =>
        e ? PriceCandlestick.toJSON(e) : undefined
      );
    } else {
      obj.prices = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetHistoricalPricesResponse>
  ): QueryGetHistoricalPricesResponse {
    const message = {
      ...baseQueryGetHistoricalPricesResponse,
    } as QueryGetHistoricalPricesResponse;
    message.prices = [];
    if (object.prices !== undefined && object.prices !== null) {
      for (const e of object.prices) {
        message.prices.push(PriceCandlestick.fromPartial(e));
      }
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
  GetSettlements(
    request: QueryGetSettlementsRequest
  ): Promise<QueryGetSettlementsResponse>;
  GetPrices(request: QueryGetPricesRequest): Promise<QueryGetPricesResponse>;
  GetTwaps(request: QueryGetTwapsRequest): Promise<QueryGetTwapsResponse>;
  /** Returns the metadata for a specified denom / display type */
  AssetMetadata(
    request: QueryAssetMetadataRequest
  ): Promise<QueryAssetMetadataResponse>;
  /** Returns metadata for all the assets */
  AssetList(request: QueryAssetListRequest): Promise<QueryAssetListResponse>;
  /** Returns all registered pairs for specified contract address */
  GetRegisteredPairs(
    request: QueryRegisteredPairsRequest
  ): Promise<QueryRegisteredPairsResponse>;
  GetOrders(request: QueryGetOrdersRequest): Promise<QueryGetOrdersResponse>;
  GetOrderByID(
    request: QueryGetOrderByIDRequest
  ): Promise<QueryGetOrderByIDResponse>;
  GetHistoricalPrices(
    request: QueryGetHistoricalPricesRequest
  ): Promise<QueryGetHistoricalPricesResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  Params(request: QueryParamsRequest): Promise<QueryParamsResponse> {
    const data = QueryParamsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
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
      "seiprotocol.seichain.dex.Query",
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
      "seiprotocol.seichain.dex.Query",
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
      "seiprotocol.seichain.dex.Query",
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
      "seiprotocol.seichain.dex.Query",
      "ShortBookAll",
      data
    );
    return promise.then((data) =>
      QueryAllShortBookResponse.decode(new Reader(data))
    );
  }

  GetSettlements(
    request: QueryGetSettlementsRequest
  ): Promise<QueryGetSettlementsResponse> {
    const data = QueryGetSettlementsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "GetSettlements",
      data
    );
    return promise.then((data) =>
      QueryGetSettlementsResponse.decode(new Reader(data))
    );
  }

  GetPrices(request: QueryGetPricesRequest): Promise<QueryGetPricesResponse> {
    const data = QueryGetPricesRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "GetPrices",
      data
    );
    return promise.then((data) =>
      QueryGetPricesResponse.decode(new Reader(data))
    );
  }

  GetTwaps(request: QueryGetTwapsRequest): Promise<QueryGetTwapsResponse> {
    const data = QueryGetTwapsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "GetTwaps",
      data
    );
    return promise.then((data) =>
      QueryGetTwapsResponse.decode(new Reader(data))
    );
  }

  AssetMetadata(
    request: QueryAssetMetadataRequest
  ): Promise<QueryAssetMetadataResponse> {
    const data = QueryAssetMetadataRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "AssetMetadata",
      data
    );
    return promise.then((data) =>
      QueryAssetMetadataResponse.decode(new Reader(data))
    );
  }

  AssetList(request: QueryAssetListRequest): Promise<QueryAssetListResponse> {
    const data = QueryAssetListRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "AssetList",
      data
    );
    return promise.then((data) =>
      QueryAssetListResponse.decode(new Reader(data))
    );
  }

  GetRegisteredPairs(
    request: QueryRegisteredPairsRequest
  ): Promise<QueryRegisteredPairsResponse> {
    const data = QueryRegisteredPairsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "GetRegisteredPairs",
      data
    );
    return promise.then((data) =>
      QueryRegisteredPairsResponse.decode(new Reader(data))
    );
  }

  GetOrders(request: QueryGetOrdersRequest): Promise<QueryGetOrdersResponse> {
    const data = QueryGetOrdersRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "GetOrders",
      data
    );
    return promise.then((data) =>
      QueryGetOrdersResponse.decode(new Reader(data))
    );
  }

  GetOrderByID(
    request: QueryGetOrderByIDRequest
  ): Promise<QueryGetOrderByIDResponse> {
    const data = QueryGetOrderByIDRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "GetOrderByID",
      data
    );
    return promise.then((data) =>
      QueryGetOrderByIDResponse.decode(new Reader(data))
    );
  }

  GetHistoricalPrices(
    request: QueryGetHistoricalPricesRequest
  ): Promise<QueryGetHistoricalPricesResponse> {
    const data = QueryGetHistoricalPricesRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "GetHistoricalPrices",
      data
    );
    return promise.then((data) =>
      QueryGetHistoricalPricesResponse.decode(new Reader(data))
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
