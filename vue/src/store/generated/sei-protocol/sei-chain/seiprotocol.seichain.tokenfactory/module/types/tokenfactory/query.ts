/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Params } from "../tokenfactory/params";
import { DenomAuthorityMetadata } from "../tokenfactory/authorityMetadata";

export const protobufPackage = "seiprotocol.seichain.tokenfactory";

/** QueryParamsRequest is the request type for the Query/Params RPC method. */
export interface QueryParamsRequest {}

/** QueryParamsResponse is the response type for the Query/Params RPC method. */
export interface QueryParamsResponse {
  /** params defines the parameters of the module. */
  params: Params | undefined;
}

/**
 * QueryDenomAuthorityMetadataRequest defines the request structure for the
 * DenomAuthorityMetadata gRPC query.
 */
export interface QueryDenomAuthorityMetadataRequest {
  denom: string;
}

/**
 * QueryDenomAuthorityMetadataResponse defines the response structure for the
 * DenomAuthorityMetadata gRPC query.
 */
export interface QueryDenomAuthorityMetadataResponse {
  authority_metadata: DenomAuthorityMetadata | undefined;
}

/**
 * QueryDenomsFromCreatorRequest defines the request structure for the
 * DenomsFromCreator gRPC query.
 */
export interface QueryDenomsFromCreatorRequest {
  creator: string;
}

/**
 * QueryDenomsFromCreatorRequest defines the response structure for the
 * DenomsFromCreator gRPC query.
 */
export interface QueryDenomsFromCreatorResponse {
  denoms: string[];
}

/**
 * QueryDenomCreationFeeWhitelistRequest defines the request structure for the
 * DenomCreationFeeWhitelist gRPC query.
 */
export interface QueryDenomCreationFeeWhitelistRequest {}

/**
 * QueryDenomCreationFeeWhitelistResponse defines the response structure for the
 * DenomsFromCreator gRPC query.
 */
export interface QueryDenomCreationFeeWhitelistResponse {
  creators: string[];
}

/**
 * QueryCreatorInDenomFeeWhitelistRequest defines the request structure for the
 * CreatorInDenomFeeWhitelist gRPC query.
 */
export interface QueryCreatorInDenomFeeWhitelistRequest {
  creator: string;
}

/**
 * QueryCreatorInDenomFeeWhitelistResponse defines the response structure for the
 * CreatorInDenomFeeWhitelist gRPC query.
 */
export interface QueryCreatorInDenomFeeWhitelistResponse {
  whitelisted: boolean;
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

const baseQueryDenomAuthorityMetadataRequest: object = { denom: "" };

export const QueryDenomAuthorityMetadataRequest = {
  encode(
    message: QueryDenomAuthorityMetadataRequest,
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
  ): QueryDenomAuthorityMetadataRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDenomAuthorityMetadataRequest,
    } as QueryDenomAuthorityMetadataRequest;
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

  fromJSON(object: any): QueryDenomAuthorityMetadataRequest {
    const message = {
      ...baseQueryDenomAuthorityMetadataRequest,
    } as QueryDenomAuthorityMetadataRequest;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = String(object.denom);
    } else {
      message.denom = "";
    }
    return message;
  },

  toJSON(message: QueryDenomAuthorityMetadataRequest): unknown {
    const obj: any = {};
    message.denom !== undefined && (obj.denom = message.denom);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDenomAuthorityMetadataRequest>
  ): QueryDenomAuthorityMetadataRequest {
    const message = {
      ...baseQueryDenomAuthorityMetadataRequest,
    } as QueryDenomAuthorityMetadataRequest;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = object.denom;
    } else {
      message.denom = "";
    }
    return message;
  },
};

const baseQueryDenomAuthorityMetadataResponse: object = {};

export const QueryDenomAuthorityMetadataResponse = {
  encode(
    message: QueryDenomAuthorityMetadataResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.authority_metadata !== undefined) {
      DenomAuthorityMetadata.encode(
        message.authority_metadata,
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryDenomAuthorityMetadataResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDenomAuthorityMetadataResponse,
    } as QueryDenomAuthorityMetadataResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.authority_metadata = DenomAuthorityMetadata.decode(
            reader,
            reader.uint32()
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDenomAuthorityMetadataResponse {
    const message = {
      ...baseQueryDenomAuthorityMetadataResponse,
    } as QueryDenomAuthorityMetadataResponse;
    if (
      object.authority_metadata !== undefined &&
      object.authority_metadata !== null
    ) {
      message.authority_metadata = DenomAuthorityMetadata.fromJSON(
        object.authority_metadata
      );
    } else {
      message.authority_metadata = undefined;
    }
    return message;
  },

  toJSON(message: QueryDenomAuthorityMetadataResponse): unknown {
    const obj: any = {};
    message.authority_metadata !== undefined &&
      (obj.authority_metadata = message.authority_metadata
        ? DenomAuthorityMetadata.toJSON(message.authority_metadata)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDenomAuthorityMetadataResponse>
  ): QueryDenomAuthorityMetadataResponse {
    const message = {
      ...baseQueryDenomAuthorityMetadataResponse,
    } as QueryDenomAuthorityMetadataResponse;
    if (
      object.authority_metadata !== undefined &&
      object.authority_metadata !== null
    ) {
      message.authority_metadata = DenomAuthorityMetadata.fromPartial(
        object.authority_metadata
      );
    } else {
      message.authority_metadata = undefined;
    }
    return message;
  },
};

const baseQueryDenomsFromCreatorRequest: object = { creator: "" };

export const QueryDenomsFromCreatorRequest = {
  encode(
    message: QueryDenomsFromCreatorRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.creator !== "") {
      writer.uint32(10).string(message.creator);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryDenomsFromCreatorRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDenomsFromCreatorRequest,
    } as QueryDenomsFromCreatorRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.creator = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDenomsFromCreatorRequest {
    const message = {
      ...baseQueryDenomsFromCreatorRequest,
    } as QueryDenomsFromCreatorRequest;
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = String(object.creator);
    } else {
      message.creator = "";
    }
    return message;
  },

  toJSON(message: QueryDenomsFromCreatorRequest): unknown {
    const obj: any = {};
    message.creator !== undefined && (obj.creator = message.creator);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDenomsFromCreatorRequest>
  ): QueryDenomsFromCreatorRequest {
    const message = {
      ...baseQueryDenomsFromCreatorRequest,
    } as QueryDenomsFromCreatorRequest;
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = object.creator;
    } else {
      message.creator = "";
    }
    return message;
  },
};

const baseQueryDenomsFromCreatorResponse: object = { denoms: "" };

export const QueryDenomsFromCreatorResponse = {
  encode(
    message: QueryDenomsFromCreatorResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.denoms) {
      writer.uint32(10).string(v!);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryDenomsFromCreatorResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDenomsFromCreatorResponse,
    } as QueryDenomsFromCreatorResponse;
    message.denoms = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.denoms.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDenomsFromCreatorResponse {
    const message = {
      ...baseQueryDenomsFromCreatorResponse,
    } as QueryDenomsFromCreatorResponse;
    message.denoms = [];
    if (object.denoms !== undefined && object.denoms !== null) {
      for (const e of object.denoms) {
        message.denoms.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: QueryDenomsFromCreatorResponse): unknown {
    const obj: any = {};
    if (message.denoms) {
      obj.denoms = message.denoms.map((e) => e);
    } else {
      obj.denoms = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDenomsFromCreatorResponse>
  ): QueryDenomsFromCreatorResponse {
    const message = {
      ...baseQueryDenomsFromCreatorResponse,
    } as QueryDenomsFromCreatorResponse;
    message.denoms = [];
    if (object.denoms !== undefined && object.denoms !== null) {
      for (const e of object.denoms) {
        message.denoms.push(e);
      }
    }
    return message;
  },
};

const baseQueryDenomCreationFeeWhitelistRequest: object = {};

export const QueryDenomCreationFeeWhitelistRequest = {
  encode(
    _: QueryDenomCreationFeeWhitelistRequest,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryDenomCreationFeeWhitelistRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDenomCreationFeeWhitelistRequest,
    } as QueryDenomCreationFeeWhitelistRequest;
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

  fromJSON(_: any): QueryDenomCreationFeeWhitelistRequest {
    const message = {
      ...baseQueryDenomCreationFeeWhitelistRequest,
    } as QueryDenomCreationFeeWhitelistRequest;
    return message;
  },

  toJSON(_: QueryDenomCreationFeeWhitelistRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<QueryDenomCreationFeeWhitelistRequest>
  ): QueryDenomCreationFeeWhitelistRequest {
    const message = {
      ...baseQueryDenomCreationFeeWhitelistRequest,
    } as QueryDenomCreationFeeWhitelistRequest;
    return message;
  },
};

const baseQueryDenomCreationFeeWhitelistResponse: object = { creators: "" };

export const QueryDenomCreationFeeWhitelistResponse = {
  encode(
    message: QueryDenomCreationFeeWhitelistResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.creators) {
      writer.uint32(10).string(v!);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryDenomCreationFeeWhitelistResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDenomCreationFeeWhitelistResponse,
    } as QueryDenomCreationFeeWhitelistResponse;
    message.creators = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.creators.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDenomCreationFeeWhitelistResponse {
    const message = {
      ...baseQueryDenomCreationFeeWhitelistResponse,
    } as QueryDenomCreationFeeWhitelistResponse;
    message.creators = [];
    if (object.creators !== undefined && object.creators !== null) {
      for (const e of object.creators) {
        message.creators.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: QueryDenomCreationFeeWhitelistResponse): unknown {
    const obj: any = {};
    if (message.creators) {
      obj.creators = message.creators.map((e) => e);
    } else {
      obj.creators = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDenomCreationFeeWhitelistResponse>
  ): QueryDenomCreationFeeWhitelistResponse {
    const message = {
      ...baseQueryDenomCreationFeeWhitelistResponse,
    } as QueryDenomCreationFeeWhitelistResponse;
    message.creators = [];
    if (object.creators !== undefined && object.creators !== null) {
      for (const e of object.creators) {
        message.creators.push(e);
      }
    }
    return message;
  },
};

const baseQueryCreatorInDenomFeeWhitelistRequest: object = { creator: "" };

export const QueryCreatorInDenomFeeWhitelistRequest = {
  encode(
    message: QueryCreatorInDenomFeeWhitelistRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.creator !== "") {
      writer.uint32(10).string(message.creator);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryCreatorInDenomFeeWhitelistRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryCreatorInDenomFeeWhitelistRequest,
    } as QueryCreatorInDenomFeeWhitelistRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.creator = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryCreatorInDenomFeeWhitelistRequest {
    const message = {
      ...baseQueryCreatorInDenomFeeWhitelistRequest,
    } as QueryCreatorInDenomFeeWhitelistRequest;
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = String(object.creator);
    } else {
      message.creator = "";
    }
    return message;
  },

  toJSON(message: QueryCreatorInDenomFeeWhitelistRequest): unknown {
    const obj: any = {};
    message.creator !== undefined && (obj.creator = message.creator);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryCreatorInDenomFeeWhitelistRequest>
  ): QueryCreatorInDenomFeeWhitelistRequest {
    const message = {
      ...baseQueryCreatorInDenomFeeWhitelistRequest,
    } as QueryCreatorInDenomFeeWhitelistRequest;
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = object.creator;
    } else {
      message.creator = "";
    }
    return message;
  },
};

const baseQueryCreatorInDenomFeeWhitelistResponse: object = {
  whitelisted: false,
};

export const QueryCreatorInDenomFeeWhitelistResponse = {
  encode(
    message: QueryCreatorInDenomFeeWhitelistResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.whitelisted === true) {
      writer.uint32(8).bool(message.whitelisted);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryCreatorInDenomFeeWhitelistResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryCreatorInDenomFeeWhitelistResponse,
    } as QueryCreatorInDenomFeeWhitelistResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.whitelisted = reader.bool();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryCreatorInDenomFeeWhitelistResponse {
    const message = {
      ...baseQueryCreatorInDenomFeeWhitelistResponse,
    } as QueryCreatorInDenomFeeWhitelistResponse;
    if (object.whitelisted !== undefined && object.whitelisted !== null) {
      message.whitelisted = Boolean(object.whitelisted);
    } else {
      message.whitelisted = false;
    }
    return message;
  },

  toJSON(message: QueryCreatorInDenomFeeWhitelistResponse): unknown {
    const obj: any = {};
    message.whitelisted !== undefined &&
      (obj.whitelisted = message.whitelisted);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryCreatorInDenomFeeWhitelistResponse>
  ): QueryCreatorInDenomFeeWhitelistResponse {
    const message = {
      ...baseQueryCreatorInDenomFeeWhitelistResponse,
    } as QueryCreatorInDenomFeeWhitelistResponse;
    if (object.whitelisted !== undefined && object.whitelisted !== null) {
      message.whitelisted = object.whitelisted;
    } else {
      message.whitelisted = false;
    }
    return message;
  },
};

/** Query defines the gRPC querier service. */
export interface Query {
  /**
   * Params defines a gRPC query method that returns the tokenfactory module's
   * parameters.
   */
  Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
  /**
   * DenomAuthorityMetadata defines a gRPC query method for fetching
   * DenomAuthorityMetadata for a particular denom.
   */
  DenomAuthorityMetadata(
    request: QueryDenomAuthorityMetadataRequest
  ): Promise<QueryDenomAuthorityMetadataResponse>;
  /**
   * DenomsFromCreator defines a gRPC query method for fetching all
   * denominations created by a specific admin/creator.
   */
  DenomsFromCreator(
    request: QueryDenomsFromCreatorRequest
  ): Promise<QueryDenomsFromCreatorResponse>;
  /**
   * DenomCreationFeeWhitelist defines a gRPC query method for fetching all
   * creators who are whitelisted from paying the denom creation fee.
   */
  DenomCreationFeeWhitelist(
    request: QueryDenomCreationFeeWhitelistRequest
  ): Promise<QueryDenomCreationFeeWhitelistResponse>;
  /**
   * CreatorInDenomFeeWhitelist defines a gRPC query method for fetching
   * whether a creator is whitelisted from denom creation fees.
   */
  CreatorInDenomFeeWhitelist(
    request: QueryCreatorInDenomFeeWhitelistRequest
  ): Promise<QueryCreatorInDenomFeeWhitelistResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  Params(request: QueryParamsRequest): Promise<QueryParamsResponse> {
    const data = QueryParamsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.tokenfactory.Query",
      "Params",
      data
    );
    return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
  }

  DenomAuthorityMetadata(
    request: QueryDenomAuthorityMetadataRequest
  ): Promise<QueryDenomAuthorityMetadataResponse> {
    const data = QueryDenomAuthorityMetadataRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.tokenfactory.Query",
      "DenomAuthorityMetadata",
      data
    );
    return promise.then((data) =>
      QueryDenomAuthorityMetadataResponse.decode(new Reader(data))
    );
  }

  DenomsFromCreator(
    request: QueryDenomsFromCreatorRequest
  ): Promise<QueryDenomsFromCreatorResponse> {
    const data = QueryDenomsFromCreatorRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.tokenfactory.Query",
      "DenomsFromCreator",
      data
    );
    return promise.then((data) =>
      QueryDenomsFromCreatorResponse.decode(new Reader(data))
    );
  }

  DenomCreationFeeWhitelist(
    request: QueryDenomCreationFeeWhitelistRequest
  ): Promise<QueryDenomCreationFeeWhitelistResponse> {
    const data = QueryDenomCreationFeeWhitelistRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.tokenfactory.Query",
      "DenomCreationFeeWhitelist",
      data
    );
    return promise.then((data) =>
      QueryDenomCreationFeeWhitelistResponse.decode(new Reader(data))
    );
  }

  CreatorInDenomFeeWhitelist(
    request: QueryCreatorInDenomFeeWhitelistRequest
  ): Promise<QueryCreatorInDenomFeeWhitelistResponse> {
    const data = QueryCreatorInDenomFeeWhitelistRequest.encode(
      request
    ).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.tokenfactory.Query",
      "CreatorInDenomFeeWhitelist",
      data
    );
    return promise.then((data) =>
      QueryCreatorInDenomFeeWhitelistResponse.decode(new Reader(data))
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
