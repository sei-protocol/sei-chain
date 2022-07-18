/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Params } from "../../mint/v1beta1/mint";

export const protobufPackage = "seiprotocol.seichain.mint";

/** QueryParamsRequest is the request type for the Query/Params RPC method. */
export interface QueryParamsRequest {}

/** QueryParamsResponse is the response type for the Query/Params RPC method. */
export interface QueryParamsResponse {
  /** params defines the parameters of the module. */
  params: Params | undefined;
}

/** QueryInflationRequest is the request type for the Query/Inflation RPC method. */
export interface QueryInflationRequest {}

/**
 * QueryInflationResponse is the response type for the Query/Inflation RPC
 * method.
 */
export interface QueryInflationResponse {
  /** inflation is the current minting inflation value. */
  inflation: Uint8Array;
}

/**
 * QueryAnnualProvisionsRequest is the request type for the
 * Query/AnnualProvisions RPC method.
 */
export interface QueryAnnualProvisionsRequest {}

/**
 * QueryAnnualProvisionsResponse is the response type for the
 * Query/AnnualProvisions RPC method.
 */
export interface QueryAnnualProvisionsResponse {
  /** annual_provisions is the current minting annual provisions value. */
  annual_provisions: Uint8Array;
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

const baseQueryInflationRequest: object = {};

export const QueryInflationRequest = {
  encode(_: QueryInflationRequest, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryInflationRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryInflationRequest } as QueryInflationRequest;
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

  fromJSON(_: any): QueryInflationRequest {
    const message = { ...baseQueryInflationRequest } as QueryInflationRequest;
    return message;
  },

  toJSON(_: QueryInflationRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(_: DeepPartial<QueryInflationRequest>): QueryInflationRequest {
    const message = { ...baseQueryInflationRequest } as QueryInflationRequest;
    return message;
  },
};

const baseQueryInflationResponse: object = {};

export const QueryInflationResponse = {
  encode(
    message: QueryInflationResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.inflation.length !== 0) {
      writer.uint32(10).bytes(message.inflation);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryInflationResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryInflationResponse } as QueryInflationResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.inflation = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryInflationResponse {
    const message = { ...baseQueryInflationResponse } as QueryInflationResponse;
    if (object.inflation !== undefined && object.inflation !== null) {
      message.inflation = bytesFromBase64(object.inflation);
    }
    return message;
  },

  toJSON(message: QueryInflationResponse): unknown {
    const obj: any = {};
    message.inflation !== undefined &&
      (obj.inflation = base64FromBytes(
        message.inflation !== undefined ? message.inflation : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryInflationResponse>
  ): QueryInflationResponse {
    const message = { ...baseQueryInflationResponse } as QueryInflationResponse;
    if (object.inflation !== undefined && object.inflation !== null) {
      message.inflation = object.inflation;
    } else {
      message.inflation = new Uint8Array();
    }
    return message;
  },
};

const baseQueryAnnualProvisionsRequest: object = {};

export const QueryAnnualProvisionsRequest = {
  encode(
    _: QueryAnnualProvisionsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryAnnualProvisionsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryAnnualProvisionsRequest,
    } as QueryAnnualProvisionsRequest;
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

  fromJSON(_: any): QueryAnnualProvisionsRequest {
    const message = {
      ...baseQueryAnnualProvisionsRequest,
    } as QueryAnnualProvisionsRequest;
    return message;
  },

  toJSON(_: QueryAnnualProvisionsRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<QueryAnnualProvisionsRequest>
  ): QueryAnnualProvisionsRequest {
    const message = {
      ...baseQueryAnnualProvisionsRequest,
    } as QueryAnnualProvisionsRequest;
    return message;
  },
};

const baseQueryAnnualProvisionsResponse: object = {};

export const QueryAnnualProvisionsResponse = {
  encode(
    message: QueryAnnualProvisionsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.annual_provisions.length !== 0) {
      writer.uint32(10).bytes(message.annual_provisions);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryAnnualProvisionsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryAnnualProvisionsResponse,
    } as QueryAnnualProvisionsResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.annual_provisions = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryAnnualProvisionsResponse {
    const message = {
      ...baseQueryAnnualProvisionsResponse,
    } as QueryAnnualProvisionsResponse;
    if (
      object.annual_provisions !== undefined &&
      object.annual_provisions !== null
    ) {
      message.annual_provisions = bytesFromBase64(object.annual_provisions);
    }
    return message;
  },

  toJSON(message: QueryAnnualProvisionsResponse): unknown {
    const obj: any = {};
    message.annual_provisions !== undefined &&
      (obj.annual_provisions = base64FromBytes(
        message.annual_provisions !== undefined
          ? message.annual_provisions
          : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryAnnualProvisionsResponse>
  ): QueryAnnualProvisionsResponse {
    const message = {
      ...baseQueryAnnualProvisionsResponse,
    } as QueryAnnualProvisionsResponse;
    if (
      object.annual_provisions !== undefined &&
      object.annual_provisions !== null
    ) {
      message.annual_provisions = object.annual_provisions;
    } else {
      message.annual_provisions = new Uint8Array();
    }
    return message;
  },
};

/** Query provides defines the gRPC querier service. */
export interface Query {
  /** Params returns the total set of minting parameters. */
  Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
  /** Inflation returns the current minting inflation value. */
  Inflation(request: QueryInflationRequest): Promise<QueryInflationResponse>;
  /** AnnualProvisions current minting annual provisions value. */
  AnnualProvisions(
    request: QueryAnnualProvisionsRequest
  ): Promise<QueryAnnualProvisionsResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  Params(request: QueryParamsRequest): Promise<QueryParamsResponse> {
    const data = QueryParamsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.mint.Query",
      "Params",
      data
    );
    return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
  }

  Inflation(request: QueryInflationRequest): Promise<QueryInflationResponse> {
    const data = QueryInflationRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.mint.Query",
      "Inflation",
      data
    );
    return promise.then((data) =>
      QueryInflationResponse.decode(new Reader(data))
    );
  }

  AnnualProvisions(
    request: QueryAnnualProvisionsRequest
  ): Promise<QueryAnnualProvisionsResponse> {
    const data = QueryAnnualProvisionsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "seiprotocol.seichain.mint.Query",
      "AnnualProvisions",
      data
    );
    return promise.then((data) =>
      QueryAnnualProvisionsResponse.decode(new Reader(data))
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

const atob: (b64: string) => string =
  globalThis.atob ||
  ((b64) => globalThis.Buffer.from(b64, "base64").toString("binary"));
function bytesFromBase64(b64: string): Uint8Array {
  const bin = atob(b64);
  const arr = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; ++i) {
    arr[i] = bin.charCodeAt(i);
  }
  return arr;
}

const btoa: (bin: string) => string =
  globalThis.btoa ||
  ((bin) => globalThis.Buffer.from(bin, "binary").toString("base64"));
function base64FromBytes(arr: Uint8Array): string {
  const bin: string[] = [];
  for (let i = 0; i < arr.byteLength; ++i) {
    bin.push(String.fromCharCode(arr[i]));
  }
  return btoa(bin.join(""));
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
