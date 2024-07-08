/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Params } from "../../tokenfactory/v1/params";
import { DenomAuthorityMetadata } from "../../tokenfactory/v1/authorityMetadata";
import { Metadata } from "../../cosmos/bank/v1beta1/bank";

export const protobufPackage = "sei.tokenfactory.v1";

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
  authorityMetadata: DenomAuthorityMetadata | undefined;
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

/** QueryDenomMetadataRequest is the request type for the DenomMetadata gRPC method. */
export interface QueryDenomMetadataRequest {
  /** denom is the coin denom to query the metadata for. */
  denom: string;
}

/**
 * QueryDenomMetadataResponse is the response type for the Query/DenomMetadata gRPC
 * method.
 */
export interface QueryDenomMetadataResponse {
  /** metadata describes and provides all the client information for the requested token. */
  metadata: Metadata | undefined;
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
    if (message.authorityMetadata !== undefined) {
      DenomAuthorityMetadata.encode(
        message.authorityMetadata,
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
          message.authorityMetadata = DenomAuthorityMetadata.decode(
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
      object.authorityMetadata !== undefined &&
      object.authorityMetadata !== null
    ) {
      message.authorityMetadata = DenomAuthorityMetadata.fromJSON(
        object.authorityMetadata
      );
    } else {
      message.authorityMetadata = undefined;
    }
    return message;
  },

  toJSON(message: QueryDenomAuthorityMetadataResponse): unknown {
    const obj: any = {};
    message.authorityMetadata !== undefined &&
      (obj.authorityMetadata = message.authorityMetadata
        ? DenomAuthorityMetadata.toJSON(message.authorityMetadata)
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
      object.authorityMetadata !== undefined &&
      object.authorityMetadata !== null
    ) {
      message.authorityMetadata = DenomAuthorityMetadata.fromPartial(
        object.authorityMetadata
      );
    } else {
      message.authorityMetadata = undefined;
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

const baseQueryDenomMetadataRequest: object = { denom: "" };

export const QueryDenomMetadataRequest = {
  encode(
    message: QueryDenomMetadataRequest,
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
  ): QueryDenomMetadataRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDenomMetadataRequest,
    } as QueryDenomMetadataRequest;
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

  fromJSON(object: any): QueryDenomMetadataRequest {
    const message = {
      ...baseQueryDenomMetadataRequest,
    } as QueryDenomMetadataRequest;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = String(object.denom);
    } else {
      message.denom = "";
    }
    return message;
  },

  toJSON(message: QueryDenomMetadataRequest): unknown {
    const obj: any = {};
    message.denom !== undefined && (obj.denom = message.denom);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDenomMetadataRequest>
  ): QueryDenomMetadataRequest {
    const message = {
      ...baseQueryDenomMetadataRequest,
    } as QueryDenomMetadataRequest;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = object.denom;
    } else {
      message.denom = "";
    }
    return message;
  },
};

const baseQueryDenomMetadataResponse: object = {};

export const QueryDenomMetadataResponse = {
  encode(
    message: QueryDenomMetadataResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.metadata !== undefined) {
      Metadata.encode(message.metadata, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryDenomMetadataResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDenomMetadataResponse,
    } as QueryDenomMetadataResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.metadata = Metadata.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDenomMetadataResponse {
    const message = {
      ...baseQueryDenomMetadataResponse,
    } as QueryDenomMetadataResponse;
    if (object.metadata !== undefined && object.metadata !== null) {
      message.metadata = Metadata.fromJSON(object.metadata);
    } else {
      message.metadata = undefined;
    }
    return message;
  },

  toJSON(message: QueryDenomMetadataResponse): unknown {
    const obj: any = {};
    message.metadata !== undefined &&
      (obj.metadata = message.metadata
        ? Metadata.toJSON(message.metadata)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDenomMetadataResponse>
  ): QueryDenomMetadataResponse {
    const message = {
      ...baseQueryDenomMetadataResponse,
    } as QueryDenomMetadataResponse;
    if (object.metadata !== undefined && object.metadata !== null) {
      message.metadata = Metadata.fromPartial(object.metadata);
    } else {
      message.metadata = undefined;
    }
    return message;
  },
};

/** Query defines the gRPC querier service. */
export interface Query {
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/tokenfactory/v1/params` instead.
   *
   * @deprecated
   */
  deprecated_Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
  /**
   * Params defines a gRPC query method that returns the tokenfactory module's
   * parameters.
   */
  Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/tokenfactory/v1/denoms/{denom}/authority_metadata` instead.
   *
   * @deprecated
   */
  deprecated_DenomAuthorityMetadata(
    request: QueryDenomAuthorityMetadataRequest
  ): Promise<QueryDenomAuthorityMetadataResponse>;
  /**
   * DenomAuthorityMetadata defines a gRPC query method for fetching
   * DenomAuthorityMetadata for a particular denom.
   */
  DenomAuthorityMetadata(
    request: QueryDenomAuthorityMetadataRequest
  ): Promise<QueryDenomAuthorityMetadataResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/tokenfactory/v1/denoms/metadata` instead.
   *
   * @deprecated
   */
  deprecated_DenomMetadata(
    request: QueryDenomMetadataRequest
  ): Promise<QueryDenomMetadataResponse>;
  /**
   * DenomsMetadata defines a gRPC query method for fetching
   *  DenomMetadata for a particular denom.
   */
  DenomMetadata(
    request: QueryDenomMetadataRequest
  ): Promise<QueryDenomMetadataResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/tokenfactory/v1/denoms_from_creator/{creator}` instead.
   *
   * @deprecated
   */
  deprecated_DenomsFromCreator(
    request: QueryDenomsFromCreatorRequest
  ): Promise<QueryDenomsFromCreatorResponse>;
  /**
   * DenomsFromCreator defines a gRPC query method for fetching all
   * denominations created by a specific admin/creator.
   */
  DenomsFromCreator(
    request: QueryDenomsFromCreatorRequest
  ): Promise<QueryDenomsFromCreatorResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  deprecated_Params(request: QueryParamsRequest): Promise<QueryParamsResponse> {
    const data = QueryParamsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.tokenfactory.v1.Query",
      "deprecated_Params",
      data
    );
    return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
  }

  Params(request: QueryParamsRequest): Promise<QueryParamsResponse> {
    const data = QueryParamsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.tokenfactory.v1.Query",
      "Params",
      data
    );
    return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
  }

  deprecated_DenomAuthorityMetadata(
    request: QueryDenomAuthorityMetadataRequest
  ): Promise<QueryDenomAuthorityMetadataResponse> {
    const data = QueryDenomAuthorityMetadataRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.tokenfactory.v1.Query",
      "deprecated_DenomAuthorityMetadata",
      data
    );
    return promise.then((data) =>
      QueryDenomAuthorityMetadataResponse.decode(new Reader(data))
    );
  }

  DenomAuthorityMetadata(
    request: QueryDenomAuthorityMetadataRequest
  ): Promise<QueryDenomAuthorityMetadataResponse> {
    const data = QueryDenomAuthorityMetadataRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.tokenfactory.v1.Query",
      "DenomAuthorityMetadata",
      data
    );
    return promise.then((data) =>
      QueryDenomAuthorityMetadataResponse.decode(new Reader(data))
    );
  }

  deprecated_DenomMetadata(
    request: QueryDenomMetadataRequest
  ): Promise<QueryDenomMetadataResponse> {
    const data = QueryDenomMetadataRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.tokenfactory.v1.Query",
      "deprecated_DenomMetadata",
      data
    );
    return promise.then((data) =>
      QueryDenomMetadataResponse.decode(new Reader(data))
    );
  }

  DenomMetadata(
    request: QueryDenomMetadataRequest
  ): Promise<QueryDenomMetadataResponse> {
    const data = QueryDenomMetadataRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.tokenfactory.v1.Query",
      "DenomMetadata",
      data
    );
    return promise.then((data) =>
      QueryDenomMetadataResponse.decode(new Reader(data))
    );
  }

  deprecated_DenomsFromCreator(
    request: QueryDenomsFromCreatorRequest
  ): Promise<QueryDenomsFromCreatorResponse> {
    const data = QueryDenomsFromCreatorRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.tokenfactory.v1.Query",
      "deprecated_DenomsFromCreator",
      data
    );
    return promise.then((data) =>
      QueryDenomsFromCreatorResponse.decode(new Reader(data))
    );
  }

  DenomsFromCreator(
    request: QueryDenomsFromCreatorRequest
  ): Promise<QueryDenomsFromCreatorResponse> {
    const data = QueryDenomsFromCreatorRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.tokenfactory.v1.Query",
      "DenomsFromCreator",
      data
    );
    return promise.then((data) =>
      QueryDenomsFromCreatorResponse.decode(new Reader(data))
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
