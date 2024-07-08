/* eslint-disable */
import {
  PositionDirection,
  positionDirectionFromJSON,
  positionDirectionToJSON,
} from "../dex/enums";
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import { Params } from "../dex/params";
import { LongBook } from "../dex/long_book";
import {
  PageRequest,
  PageResponse,
} from "../cosmos/base/query/v1beta1/pagination";
import { ShortBook } from "../dex/short_book";
import { Price, PriceCandlestick } from "../dex/price";
import { Twap } from "../dex/twap";
import { AssetMetadata } from "../dex/asset_list";
import { Pair } from "../dex/pair";
import { ContractInfoV2 } from "../dex/contract";
import { Order } from "../dex/order";
import { MatchResult } from "../dex/match_result";

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

export interface QueryGetPricesRequest {
  priceDenom: string;
  assetDenom: string;
  contractAddr: string;
}

export interface QueryGetPricesResponse {
  prices: Price[];
}

export interface QueryGetPriceRequest {
  priceDenom: string;
  assetDenom: string;
  contractAddr: string;
  timestamp: number;
}

export interface QueryGetPriceResponse {
  price: Price | undefined;
  found: boolean;
}

export interface QueryGetLatestPriceRequest {
  priceDenom: string;
  assetDenom: string;
  contractAddr: string;
}

export interface QueryGetLatestPriceResponse {
  price: Price | undefined;
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

export interface QueryRegisteredContractRequest {
  contractAddr: string;
}

export interface QueryRegisteredContractResponse {
  contractInfo: ContractInfoV2 | undefined;
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

export interface QueryGetMarketSummaryRequest {
  contractAddr: string;
  priceDenom: string;
  assetDenom: string;
  lookbackInSeconds: number;
}

export interface QueryGetMarketSummaryResponse {
  totalVolume: string;
  totalVolumeNotional: string;
  highPrice: string;
  lowPrice: string;
  lastPrice: string;
}

export interface QueryOrderSimulationRequest {
  order: Order | undefined;
  contractAddr: string;
}

export interface QueryOrderSimulationResponse {
  ExecutedQuantity: string;
}

export interface QueryGetMatchResultRequest {
  contractAddr: string;
}

export interface QueryGetMatchResultResponse {
  result: MatchResult | undefined;
}

export interface QueryGetOrderCountRequest {
  contractAddr: string;
  priceDenom: string;
  assetDenom: string;
  price: string;
  positionDirection: PositionDirection;
}

export interface QueryGetOrderCountResponse {
  count: number;
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

const baseQueryGetPriceRequest: object = {
  priceDenom: "",
  assetDenom: "",
  contractAddr: "",
  timestamp: 0,
};

export const QueryGetPriceRequest = {
  encode(
    message: QueryGetPriceRequest,
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
    if (message.timestamp !== 0) {
      writer.uint32(32).uint64(message.timestamp);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryGetPriceRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryGetPriceRequest } as QueryGetPriceRequest;
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
        case 4:
          message.timestamp = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryGetPriceRequest {
    const message = { ...baseQueryGetPriceRequest } as QueryGetPriceRequest;
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
    if (object.timestamp !== undefined && object.timestamp !== null) {
      message.timestamp = Number(object.timestamp);
    } else {
      message.timestamp = 0;
    }
    return message;
  },

  toJSON(message: QueryGetPriceRequest): unknown {
    const obj: any = {};
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    message.timestamp !== undefined && (obj.timestamp = message.timestamp);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryGetPriceRequest>): QueryGetPriceRequest {
    const message = { ...baseQueryGetPriceRequest } as QueryGetPriceRequest;
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
    if (object.timestamp !== undefined && object.timestamp !== null) {
      message.timestamp = object.timestamp;
    } else {
      message.timestamp = 0;
    }
    return message;
  },
};

const baseQueryGetPriceResponse: object = { found: false };

export const QueryGetPriceResponse = {
  encode(
    message: QueryGetPriceResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.price !== undefined) {
      Price.encode(message.price, writer.uint32(10).fork()).ldelim();
    }
    if (message.found === true) {
      writer.uint32(16).bool(message.found);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryGetPriceResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryGetPriceResponse } as QueryGetPriceResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.price = Price.decode(reader, reader.uint32());
          break;
        case 2:
          message.found = reader.bool();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryGetPriceResponse {
    const message = { ...baseQueryGetPriceResponse } as QueryGetPriceResponse;
    if (object.price !== undefined && object.price !== null) {
      message.price = Price.fromJSON(object.price);
    } else {
      message.price = undefined;
    }
    if (object.found !== undefined && object.found !== null) {
      message.found = Boolean(object.found);
    } else {
      message.found = false;
    }
    return message;
  },

  toJSON(message: QueryGetPriceResponse): unknown {
    const obj: any = {};
    message.price !== undefined &&
      (obj.price = message.price ? Price.toJSON(message.price) : undefined);
    message.found !== undefined && (obj.found = message.found);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetPriceResponse>
  ): QueryGetPriceResponse {
    const message = { ...baseQueryGetPriceResponse } as QueryGetPriceResponse;
    if (object.price !== undefined && object.price !== null) {
      message.price = Price.fromPartial(object.price);
    } else {
      message.price = undefined;
    }
    if (object.found !== undefined && object.found !== null) {
      message.found = object.found;
    } else {
      message.found = false;
    }
    return message;
  },
};

const baseQueryGetLatestPriceRequest: object = {
  priceDenom: "",
  assetDenom: "",
  contractAddr: "",
};

export const QueryGetLatestPriceRequest = {
  encode(
    message: QueryGetLatestPriceRequest,
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

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryGetLatestPriceRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetLatestPriceRequest,
    } as QueryGetLatestPriceRequest;
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

  fromJSON(object: any): QueryGetLatestPriceRequest {
    const message = {
      ...baseQueryGetLatestPriceRequest,
    } as QueryGetLatestPriceRequest;
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

  toJSON(message: QueryGetLatestPriceRequest): unknown {
    const obj: any = {};
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetLatestPriceRequest>
  ): QueryGetLatestPriceRequest {
    const message = {
      ...baseQueryGetLatestPriceRequest,
    } as QueryGetLatestPriceRequest;
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

const baseQueryGetLatestPriceResponse: object = {};

export const QueryGetLatestPriceResponse = {
  encode(
    message: QueryGetLatestPriceResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.price !== undefined) {
      Price.encode(message.price, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryGetLatestPriceResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetLatestPriceResponse,
    } as QueryGetLatestPriceResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.price = Price.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryGetLatestPriceResponse {
    const message = {
      ...baseQueryGetLatestPriceResponse,
    } as QueryGetLatestPriceResponse;
    if (object.price !== undefined && object.price !== null) {
      message.price = Price.fromJSON(object.price);
    } else {
      message.price = undefined;
    }
    return message;
  },

  toJSON(message: QueryGetLatestPriceResponse): unknown {
    const obj: any = {};
    message.price !== undefined &&
      (obj.price = message.price ? Price.toJSON(message.price) : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetLatestPriceResponse>
  ): QueryGetLatestPriceResponse {
    const message = {
      ...baseQueryGetLatestPriceResponse,
    } as QueryGetLatestPriceResponse;
    if (object.price !== undefined && object.price !== null) {
      message.price = Price.fromPartial(object.price);
    } else {
      message.price = undefined;
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

const baseQueryRegisteredContractRequest: object = { contractAddr: "" };

export const QueryRegisteredContractRequest = {
  encode(
    message: QueryRegisteredContractRequest,
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
  ): QueryRegisteredContractRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryRegisteredContractRequest,
    } as QueryRegisteredContractRequest;
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

  fromJSON(object: any): QueryRegisteredContractRequest {
    const message = {
      ...baseQueryRegisteredContractRequest,
    } as QueryRegisteredContractRequest;
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    return message;
  },

  toJSON(message: QueryRegisteredContractRequest): unknown {
    const obj: any = {};
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryRegisteredContractRequest>
  ): QueryRegisteredContractRequest {
    const message = {
      ...baseQueryRegisteredContractRequest,
    } as QueryRegisteredContractRequest;
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    return message;
  },
};

const baseQueryRegisteredContractResponse: object = {};

export const QueryRegisteredContractResponse = {
  encode(
    message: QueryRegisteredContractResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.contractInfo !== undefined) {
      ContractInfoV2.encode(
        message.contractInfo,
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryRegisteredContractResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryRegisteredContractResponse,
    } as QueryRegisteredContractResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.contractInfo = ContractInfoV2.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryRegisteredContractResponse {
    const message = {
      ...baseQueryRegisteredContractResponse,
    } as QueryRegisteredContractResponse;
    if (object.contractInfo !== undefined && object.contractInfo !== null) {
      message.contractInfo = ContractInfoV2.fromJSON(object.contractInfo);
    } else {
      message.contractInfo = undefined;
    }
    return message;
  },

  toJSON(message: QueryRegisteredContractResponse): unknown {
    const obj: any = {};
    message.contractInfo !== undefined &&
      (obj.contractInfo = message.contractInfo
        ? ContractInfoV2.toJSON(message.contractInfo)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryRegisteredContractResponse>
  ): QueryRegisteredContractResponse {
    const message = {
      ...baseQueryRegisteredContractResponse,
    } as QueryRegisteredContractResponse;
    if (object.contractInfo !== undefined && object.contractInfo !== null) {
      message.contractInfo = ContractInfoV2.fromPartial(object.contractInfo);
    } else {
      message.contractInfo = undefined;
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

const baseQueryGetMarketSummaryRequest: object = {
  contractAddr: "",
  priceDenom: "",
  assetDenom: "",
  lookbackInSeconds: 0,
};

export const QueryGetMarketSummaryRequest = {
  encode(
    message: QueryGetMarketSummaryRequest,
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
    if (message.lookbackInSeconds !== 0) {
      writer.uint32(32).uint64(message.lookbackInSeconds);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryGetMarketSummaryRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetMarketSummaryRequest,
    } as QueryGetMarketSummaryRequest;
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
          message.lookbackInSeconds = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryGetMarketSummaryRequest {
    const message = {
      ...baseQueryGetMarketSummaryRequest,
    } as QueryGetMarketSummaryRequest;
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
      object.lookbackInSeconds !== undefined &&
      object.lookbackInSeconds !== null
    ) {
      message.lookbackInSeconds = Number(object.lookbackInSeconds);
    } else {
      message.lookbackInSeconds = 0;
    }
    return message;
  },

  toJSON(message: QueryGetMarketSummaryRequest): unknown {
    const obj: any = {};
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    message.lookbackInSeconds !== undefined &&
      (obj.lookbackInSeconds = message.lookbackInSeconds);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetMarketSummaryRequest>
  ): QueryGetMarketSummaryRequest {
    const message = {
      ...baseQueryGetMarketSummaryRequest,
    } as QueryGetMarketSummaryRequest;
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
      object.lookbackInSeconds !== undefined &&
      object.lookbackInSeconds !== null
    ) {
      message.lookbackInSeconds = object.lookbackInSeconds;
    } else {
      message.lookbackInSeconds = 0;
    }
    return message;
  },
};

const baseQueryGetMarketSummaryResponse: object = {
  totalVolume: "",
  totalVolumeNotional: "",
  highPrice: "",
  lowPrice: "",
  lastPrice: "",
};

export const QueryGetMarketSummaryResponse = {
  encode(
    message: QueryGetMarketSummaryResponse,
    writer: Writer = Writer.create()
  ): Writer {
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

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryGetMarketSummaryResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetMarketSummaryResponse,
    } as QueryGetMarketSummaryResponse;
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

  fromJSON(object: any): QueryGetMarketSummaryResponse {
    const message = {
      ...baseQueryGetMarketSummaryResponse,
    } as QueryGetMarketSummaryResponse;
    if (object.totalVolume !== undefined && object.totalVolume !== null) {
      message.totalVolume = String(object.totalVolume);
    } else {
      message.totalVolume = "";
    }
    if (
      object.totalVolumeNotional !== undefined &&
      object.totalVolumeNotional !== null
    ) {
      message.totalVolumeNotional = String(object.totalVolumeNotional);
    } else {
      message.totalVolumeNotional = "";
    }
    if (object.highPrice !== undefined && object.highPrice !== null) {
      message.highPrice = String(object.highPrice);
    } else {
      message.highPrice = "";
    }
    if (object.lowPrice !== undefined && object.lowPrice !== null) {
      message.lowPrice = String(object.lowPrice);
    } else {
      message.lowPrice = "";
    }
    if (object.lastPrice !== undefined && object.lastPrice !== null) {
      message.lastPrice = String(object.lastPrice);
    } else {
      message.lastPrice = "";
    }
    return message;
  },

  toJSON(message: QueryGetMarketSummaryResponse): unknown {
    const obj: any = {};
    message.totalVolume !== undefined &&
      (obj.totalVolume = message.totalVolume);
    message.totalVolumeNotional !== undefined &&
      (obj.totalVolumeNotional = message.totalVolumeNotional);
    message.highPrice !== undefined && (obj.highPrice = message.highPrice);
    message.lowPrice !== undefined && (obj.lowPrice = message.lowPrice);
    message.lastPrice !== undefined && (obj.lastPrice = message.lastPrice);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetMarketSummaryResponse>
  ): QueryGetMarketSummaryResponse {
    const message = {
      ...baseQueryGetMarketSummaryResponse,
    } as QueryGetMarketSummaryResponse;
    if (object.totalVolume !== undefined && object.totalVolume !== null) {
      message.totalVolume = object.totalVolume;
    } else {
      message.totalVolume = "";
    }
    if (
      object.totalVolumeNotional !== undefined &&
      object.totalVolumeNotional !== null
    ) {
      message.totalVolumeNotional = object.totalVolumeNotional;
    } else {
      message.totalVolumeNotional = "";
    }
    if (object.highPrice !== undefined && object.highPrice !== null) {
      message.highPrice = object.highPrice;
    } else {
      message.highPrice = "";
    }
    if (object.lowPrice !== undefined && object.lowPrice !== null) {
      message.lowPrice = object.lowPrice;
    } else {
      message.lowPrice = "";
    }
    if (object.lastPrice !== undefined && object.lastPrice !== null) {
      message.lastPrice = object.lastPrice;
    } else {
      message.lastPrice = "";
    }
    return message;
  },
};

const baseQueryOrderSimulationRequest: object = { contractAddr: "" };

export const QueryOrderSimulationRequest = {
  encode(
    message: QueryOrderSimulationRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.order !== undefined) {
      Order.encode(message.order, writer.uint32(10).fork()).ldelim();
    }
    if (message.contractAddr !== "") {
      writer.uint32(18).string(message.contractAddr);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryOrderSimulationRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryOrderSimulationRequest,
    } as QueryOrderSimulationRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.order = Order.decode(reader, reader.uint32());
          break;
        case 2:
          message.contractAddr = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryOrderSimulationRequest {
    const message = {
      ...baseQueryOrderSimulationRequest,
    } as QueryOrderSimulationRequest;
    if (object.order !== undefined && object.order !== null) {
      message.order = Order.fromJSON(object.order);
    } else {
      message.order = undefined;
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    return message;
  },

  toJSON(message: QueryOrderSimulationRequest): unknown {
    const obj: any = {};
    message.order !== undefined &&
      (obj.order = message.order ? Order.toJSON(message.order) : undefined);
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryOrderSimulationRequest>
  ): QueryOrderSimulationRequest {
    const message = {
      ...baseQueryOrderSimulationRequest,
    } as QueryOrderSimulationRequest;
    if (object.order !== undefined && object.order !== null) {
      message.order = Order.fromPartial(object.order);
    } else {
      message.order = undefined;
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    return message;
  },
};

const baseQueryOrderSimulationResponse: object = { ExecutedQuantity: "" };

export const QueryOrderSimulationResponse = {
  encode(
    message: QueryOrderSimulationResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.ExecutedQuantity !== "") {
      writer.uint32(10).string(message.ExecutedQuantity);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryOrderSimulationResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryOrderSimulationResponse,
    } as QueryOrderSimulationResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.ExecutedQuantity = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryOrderSimulationResponse {
    const message = {
      ...baseQueryOrderSimulationResponse,
    } as QueryOrderSimulationResponse;
    if (
      object.ExecutedQuantity !== undefined &&
      object.ExecutedQuantity !== null
    ) {
      message.ExecutedQuantity = String(object.ExecutedQuantity);
    } else {
      message.ExecutedQuantity = "";
    }
    return message;
  },

  toJSON(message: QueryOrderSimulationResponse): unknown {
    const obj: any = {};
    message.ExecutedQuantity !== undefined &&
      (obj.ExecutedQuantity = message.ExecutedQuantity);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryOrderSimulationResponse>
  ): QueryOrderSimulationResponse {
    const message = {
      ...baseQueryOrderSimulationResponse,
    } as QueryOrderSimulationResponse;
    if (
      object.ExecutedQuantity !== undefined &&
      object.ExecutedQuantity !== null
    ) {
      message.ExecutedQuantity = object.ExecutedQuantity;
    } else {
      message.ExecutedQuantity = "";
    }
    return message;
  },
};

const baseQueryGetMatchResultRequest: object = { contractAddr: "" };

export const QueryGetMatchResultRequest = {
  encode(
    message: QueryGetMatchResultRequest,
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
  ): QueryGetMatchResultRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetMatchResultRequest,
    } as QueryGetMatchResultRequest;
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

  fromJSON(object: any): QueryGetMatchResultRequest {
    const message = {
      ...baseQueryGetMatchResultRequest,
    } as QueryGetMatchResultRequest;
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    return message;
  },

  toJSON(message: QueryGetMatchResultRequest): unknown {
    const obj: any = {};
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetMatchResultRequest>
  ): QueryGetMatchResultRequest {
    const message = {
      ...baseQueryGetMatchResultRequest,
    } as QueryGetMatchResultRequest;
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    return message;
  },
};

const baseQueryGetMatchResultResponse: object = {};

export const QueryGetMatchResultResponse = {
  encode(
    message: QueryGetMatchResultResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.result !== undefined) {
      MatchResult.encode(message.result, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryGetMatchResultResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetMatchResultResponse,
    } as QueryGetMatchResultResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.result = MatchResult.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryGetMatchResultResponse {
    const message = {
      ...baseQueryGetMatchResultResponse,
    } as QueryGetMatchResultResponse;
    if (object.result !== undefined && object.result !== null) {
      message.result = MatchResult.fromJSON(object.result);
    } else {
      message.result = undefined;
    }
    return message;
  },

  toJSON(message: QueryGetMatchResultResponse): unknown {
    const obj: any = {};
    message.result !== undefined &&
      (obj.result = message.result
        ? MatchResult.toJSON(message.result)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetMatchResultResponse>
  ): QueryGetMatchResultResponse {
    const message = {
      ...baseQueryGetMatchResultResponse,
    } as QueryGetMatchResultResponse;
    if (object.result !== undefined && object.result !== null) {
      message.result = MatchResult.fromPartial(object.result);
    } else {
      message.result = undefined;
    }
    return message;
  },
};

const baseQueryGetOrderCountRequest: object = {
  contractAddr: "",
  priceDenom: "",
  assetDenom: "",
  price: "",
  positionDirection: 0,
};

export const QueryGetOrderCountRequest = {
  encode(
    message: QueryGetOrderCountRequest,
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
    if (message.price !== "") {
      writer.uint32(34).string(message.price);
    }
    if (message.positionDirection !== 0) {
      writer.uint32(40).int32(message.positionDirection);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryGetOrderCountRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetOrderCountRequest,
    } as QueryGetOrderCountRequest;
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
          message.price = reader.string();
          break;
        case 5:
          message.positionDirection = reader.int32() as any;
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryGetOrderCountRequest {
    const message = {
      ...baseQueryGetOrderCountRequest,
    } as QueryGetOrderCountRequest;
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
    if (object.price !== undefined && object.price !== null) {
      message.price = String(object.price);
    } else {
      message.price = "";
    }
    if (
      object.positionDirection !== undefined &&
      object.positionDirection !== null
    ) {
      message.positionDirection = positionDirectionFromJSON(
        object.positionDirection
      );
    } else {
      message.positionDirection = 0;
    }
    return message;
  },

  toJSON(message: QueryGetOrderCountRequest): unknown {
    const obj: any = {};
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
    message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
    message.price !== undefined && (obj.price = message.price);
    message.positionDirection !== undefined &&
      (obj.positionDirection = positionDirectionToJSON(
        message.positionDirection
      ));
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetOrderCountRequest>
  ): QueryGetOrderCountRequest {
    const message = {
      ...baseQueryGetOrderCountRequest,
    } as QueryGetOrderCountRequest;
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
    if (object.price !== undefined && object.price !== null) {
      message.price = object.price;
    } else {
      message.price = "";
    }
    if (
      object.positionDirection !== undefined &&
      object.positionDirection !== null
    ) {
      message.positionDirection = object.positionDirection;
    } else {
      message.positionDirection = 0;
    }
    return message;
  },
};

const baseQueryGetOrderCountResponse: object = { count: 0 };

export const QueryGetOrderCountResponse = {
  encode(
    message: QueryGetOrderCountResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.count !== 0) {
      writer.uint32(8).uint64(message.count);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryGetOrderCountResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryGetOrderCountResponse,
    } as QueryGetOrderCountResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.count = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryGetOrderCountResponse {
    const message = {
      ...baseQueryGetOrderCountResponse,
    } as QueryGetOrderCountResponse;
    if (object.count !== undefined && object.count !== null) {
      message.count = Number(object.count);
    } else {
      message.count = 0;
    }
    return message;
  },

  toJSON(message: QueryGetOrderCountResponse): unknown {
    const obj: any = {};
    message.count !== undefined && (obj.count = message.count);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryGetOrderCountResponse>
  ): QueryGetOrderCountResponse {
    const message = {
      ...baseQueryGetOrderCountResponse,
    } as QueryGetOrderCountResponse;
    if (object.count !== undefined && object.count !== null) {
      message.count = object.count;
    } else {
      message.count = 0;
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
  GetPrice(request: QueryGetPriceRequest): Promise<QueryGetPriceResponse>;
  GetLatestPrice(
    request: QueryGetLatestPriceRequest
  ): Promise<QueryGetLatestPriceResponse>;
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
  /** Returns registered contract information */
  GetRegisteredContract(
    request: QueryRegisteredContractRequest
  ): Promise<QueryRegisteredContractResponse>;
  GetOrders(request: QueryGetOrdersRequest): Promise<QueryGetOrdersResponse>;
  GetOrder(
    request: QueryGetOrderByIDRequest
  ): Promise<QueryGetOrderByIDResponse>;
  GetHistoricalPrices(
    request: QueryGetHistoricalPricesRequest
  ): Promise<QueryGetHistoricalPricesResponse>;
  GetMarketSummary(
    request: QueryGetMarketSummaryRequest
  ): Promise<QueryGetMarketSummaryResponse>;
  GetOrderSimulation(
    request: QueryOrderSimulationRequest
  ): Promise<QueryOrderSimulationResponse>;
  GetMatchResult(
    request: QueryGetMatchResultRequest
  ): Promise<QueryGetMatchResultResponse>;
  GetOrderCount(
    request: QueryGetOrderCountRequest
  ): Promise<QueryGetOrderCountResponse>;
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

  GetPrice(request: QueryGetPriceRequest): Promise<QueryGetPriceResponse> {
    const data = QueryGetPriceRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "GetPrice",
      data
    );
    return promise.then((data) =>
      QueryGetPriceResponse.decode(new Reader(data))
    );
  }

  GetLatestPrice(
    request: QueryGetLatestPriceRequest
  ): Promise<QueryGetLatestPriceResponse> {
    const data = QueryGetLatestPriceRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "GetLatestPrice",
      data
    );
    return promise.then((data) =>
      QueryGetLatestPriceResponse.decode(new Reader(data))
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

  GetRegisteredContract(
    request: QueryRegisteredContractRequest
  ): Promise<QueryRegisteredContractResponse> {
    const data = QueryRegisteredContractRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "GetRegisteredContract",
      data
    );
    return promise.then((data) =>
      QueryRegisteredContractResponse.decode(new Reader(data))
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

  GetOrder(
    request: QueryGetOrderByIDRequest
  ): Promise<QueryGetOrderByIDResponse> {
    const data = QueryGetOrderByIDRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "GetOrder",
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

  GetMarketSummary(
    request: QueryGetMarketSummaryRequest
  ): Promise<QueryGetMarketSummaryResponse> {
    const data = QueryGetMarketSummaryRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "GetMarketSummary",
      data
    );
    return promise.then((data) =>
      QueryGetMarketSummaryResponse.decode(new Reader(data))
    );
  }

  GetOrderSimulation(
    request: QueryOrderSimulationRequest
  ): Promise<QueryOrderSimulationResponse> {
    const data = QueryOrderSimulationRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "GetOrderSimulation",
      data
    );
    return promise.then((data) =>
      QueryOrderSimulationResponse.decode(new Reader(data))
    );
  }

  GetMatchResult(
    request: QueryGetMatchResultRequest
  ): Promise<QueryGetMatchResultResponse> {
    const data = QueryGetMatchResultRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "GetMatchResult",
      data
    );
    return promise.then((data) =>
      QueryGetMatchResultResponse.decode(new Reader(data))
    );
  }

  GetOrderCount(
    request: QueryGetOrderCountRequest
  ): Promise<QueryGetOrderCountResponse> {
    const data = QueryGetOrderCountRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.dex.Query",
      "GetOrderCount",
      data
    );
    return promise.then((data) =>
      QueryGetOrderCountResponse.decode(new Reader(data))
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
