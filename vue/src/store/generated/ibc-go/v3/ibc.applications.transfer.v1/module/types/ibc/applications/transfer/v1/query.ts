/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import {
  DenomTrace,
  Params,
} from "../../../../ibc/applications/transfer/v1/transfer";
import {
  PageRequest,
  PageResponse,
} from "../../../../cosmos/base/query/v1beta1/pagination";

export const protobufPackage = "ibc.applications.transfer.v1";

/**
 * QueryDenomTraceRequest is the request type for the Query/DenomTrace RPC
 * method
 */
export interface QueryDenomTraceRequest {
  /** hash (in hex format) of the denomination trace information. */
  hash: string;
}

/**
 * QueryDenomTraceResponse is the response type for the Query/DenomTrace RPC
 * method.
 */
export interface QueryDenomTraceResponse {
  /** denom_trace returns the requested denomination trace information. */
  denom_trace: DenomTrace | undefined;
}

/**
 * QueryConnectionsRequest is the request type for the Query/DenomTraces RPC
 * method
 */
export interface QueryDenomTracesRequest {
  /** pagination defines an optional pagination for the request. */
  pagination: PageRequest | undefined;
}

/**
 * QueryConnectionsResponse is the response type for the Query/DenomTraces RPC
 * method.
 */
export interface QueryDenomTracesResponse {
  /** denom_traces returns all denominations trace information. */
  denom_traces: DenomTrace[];
  /** pagination defines the pagination in the response. */
  pagination: PageResponse | undefined;
}

/** QueryParamsRequest is the request type for the Query/Params RPC method. */
export interface QueryParamsRequest {}

/** QueryParamsResponse is the response type for the Query/Params RPC method. */
export interface QueryParamsResponse {
  /** params defines the parameters of the module. */
  params: Params | undefined;
}

/**
 * QueryDenomHashRequest is the request type for the Query/DenomHash RPC
 * method
 */
export interface QueryDenomHashRequest {
  /** The denomination trace ([port_id]/[channel_id])+/[denom] */
  trace: string;
}

/**
 * QueryDenomHashResponse is the response type for the Query/DenomHash RPC
 * method.
 */
export interface QueryDenomHashResponse {
  /** hash (in hex format) of the denomination trace information. */
  hash: string;
}

const baseQueryDenomTraceRequest: object = { hash: "" };

export const QueryDenomTraceRequest = {
  encode(
    message: QueryDenomTraceRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.hash !== "") {
      writer.uint32(10).string(message.hash);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryDenomTraceRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryDenomTraceRequest } as QueryDenomTraceRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.hash = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDenomTraceRequest {
    const message = { ...baseQueryDenomTraceRequest } as QueryDenomTraceRequest;
    if (object.hash !== undefined && object.hash !== null) {
      message.hash = String(object.hash);
    } else {
      message.hash = "";
    }
    return message;
  },

  toJSON(message: QueryDenomTraceRequest): unknown {
    const obj: any = {};
    message.hash !== undefined && (obj.hash = message.hash);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDenomTraceRequest>
  ): QueryDenomTraceRequest {
    const message = { ...baseQueryDenomTraceRequest } as QueryDenomTraceRequest;
    if (object.hash !== undefined && object.hash !== null) {
      message.hash = object.hash;
    } else {
      message.hash = "";
    }
    return message;
  },
};

const baseQueryDenomTraceResponse: object = {};

export const QueryDenomTraceResponse = {
  encode(
    message: QueryDenomTraceResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.denom_trace !== undefined) {
      DenomTrace.encode(message.denom_trace, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryDenomTraceResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDenomTraceResponse,
    } as QueryDenomTraceResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.denom_trace = DenomTrace.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDenomTraceResponse {
    const message = {
      ...baseQueryDenomTraceResponse,
    } as QueryDenomTraceResponse;
    if (object.denom_trace !== undefined && object.denom_trace !== null) {
      message.denom_trace = DenomTrace.fromJSON(object.denom_trace);
    } else {
      message.denom_trace = undefined;
    }
    return message;
  },

  toJSON(message: QueryDenomTraceResponse): unknown {
    const obj: any = {};
    message.denom_trace !== undefined &&
      (obj.denom_trace = message.denom_trace
        ? DenomTrace.toJSON(message.denom_trace)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDenomTraceResponse>
  ): QueryDenomTraceResponse {
    const message = {
      ...baseQueryDenomTraceResponse,
    } as QueryDenomTraceResponse;
    if (object.denom_trace !== undefined && object.denom_trace !== null) {
      message.denom_trace = DenomTrace.fromPartial(object.denom_trace);
    } else {
      message.denom_trace = undefined;
    }
    return message;
  },
};

const baseQueryDenomTracesRequest: object = {};

export const QueryDenomTracesRequest = {
  encode(
    message: QueryDenomTracesRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryDenomTracesRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDenomTracesRequest,
    } as QueryDenomTracesRequest;
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

  fromJSON(object: any): QueryDenomTracesRequest {
    const message = {
      ...baseQueryDenomTracesRequest,
    } as QueryDenomTracesRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryDenomTracesRequest): unknown {
    const obj: any = {};
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDenomTracesRequest>
  ): QueryDenomTracesRequest {
    const message = {
      ...baseQueryDenomTracesRequest,
    } as QueryDenomTracesRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryDenomTracesResponse: object = {};

export const QueryDenomTracesResponse = {
  encode(
    message: QueryDenomTracesResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.denom_traces) {
      DenomTrace.encode(v!, writer.uint32(10).fork()).ldelim();
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
  ): QueryDenomTracesResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDenomTracesResponse,
    } as QueryDenomTracesResponse;
    message.denom_traces = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.denom_traces.push(DenomTrace.decode(reader, reader.uint32()));
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

  fromJSON(object: any): QueryDenomTracesResponse {
    const message = {
      ...baseQueryDenomTracesResponse,
    } as QueryDenomTracesResponse;
    message.denom_traces = [];
    if (object.denom_traces !== undefined && object.denom_traces !== null) {
      for (const e of object.denom_traces) {
        message.denom_traces.push(DenomTrace.fromJSON(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryDenomTracesResponse): unknown {
    const obj: any = {};
    if (message.denom_traces) {
      obj.denom_traces = message.denom_traces.map((e) =>
        e ? DenomTrace.toJSON(e) : undefined
      );
    } else {
      obj.denom_traces = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDenomTracesResponse>
  ): QueryDenomTracesResponse {
    const message = {
      ...baseQueryDenomTracesResponse,
    } as QueryDenomTracesResponse;
    message.denom_traces = [];
    if (object.denom_traces !== undefined && object.denom_traces !== null) {
      for (const e of object.denom_traces) {
        message.denom_traces.push(DenomTrace.fromPartial(e));
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

const baseQueryDenomHashRequest: object = { trace: "" };

export const QueryDenomHashRequest = {
  encode(
    message: QueryDenomHashRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.trace !== "") {
      writer.uint32(10).string(message.trace);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryDenomHashRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryDenomHashRequest } as QueryDenomHashRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.trace = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDenomHashRequest {
    const message = { ...baseQueryDenomHashRequest } as QueryDenomHashRequest;
    if (object.trace !== undefined && object.trace !== null) {
      message.trace = String(object.trace);
    } else {
      message.trace = "";
    }
    return message;
  },

  toJSON(message: QueryDenomHashRequest): unknown {
    const obj: any = {};
    message.trace !== undefined && (obj.trace = message.trace);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDenomHashRequest>
  ): QueryDenomHashRequest {
    const message = { ...baseQueryDenomHashRequest } as QueryDenomHashRequest;
    if (object.trace !== undefined && object.trace !== null) {
      message.trace = object.trace;
    } else {
      message.trace = "";
    }
    return message;
  },
};

const baseQueryDenomHashResponse: object = { hash: "" };

export const QueryDenomHashResponse = {
  encode(
    message: QueryDenomHashResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.hash !== "") {
      writer.uint32(10).string(message.hash);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryDenomHashResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryDenomHashResponse } as QueryDenomHashResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.hash = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDenomHashResponse {
    const message = { ...baseQueryDenomHashResponse } as QueryDenomHashResponse;
    if (object.hash !== undefined && object.hash !== null) {
      message.hash = String(object.hash);
    } else {
      message.hash = "";
    }
    return message;
  },

  toJSON(message: QueryDenomHashResponse): unknown {
    const obj: any = {};
    message.hash !== undefined && (obj.hash = message.hash);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDenomHashResponse>
  ): QueryDenomHashResponse {
    const message = { ...baseQueryDenomHashResponse } as QueryDenomHashResponse;
    if (object.hash !== undefined && object.hash !== null) {
      message.hash = object.hash;
    } else {
      message.hash = "";
    }
    return message;
  },
};

/** Query provides defines the gRPC querier service. */
export interface Query {
  /** DenomTrace queries a denomination trace information. */
  DenomTrace(request: QueryDenomTraceRequest): Promise<QueryDenomTraceResponse>;
  /** DenomTraces queries all denomination traces. */
  DenomTraces(
    request: QueryDenomTracesRequest
  ): Promise<QueryDenomTracesResponse>;
  /** Params queries all parameters of the ibc-transfer module. */
  Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
  /** DenomHash queries a denomination hash information. */
  DenomHash(request: QueryDenomHashRequest): Promise<QueryDenomHashResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  DenomTrace(
    request: QueryDenomTraceRequest
  ): Promise<QueryDenomTraceResponse> {
    const data = QueryDenomTraceRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.applications.transfer.v1.Query",
      "DenomTrace",
      data
    );
    return promise.then((data) =>
      QueryDenomTraceResponse.decode(new Reader(data))
    );
  }

  DenomTraces(
    request: QueryDenomTracesRequest
  ): Promise<QueryDenomTracesResponse> {
    const data = QueryDenomTracesRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.applications.transfer.v1.Query",
      "DenomTraces",
      data
    );
    return promise.then((data) =>
      QueryDenomTracesResponse.decode(new Reader(data))
    );
  }

  Params(request: QueryParamsRequest): Promise<QueryParamsResponse> {
    const data = QueryParamsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.applications.transfer.v1.Query",
      "Params",
      data
    );
    return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
  }

  DenomHash(request: QueryDenomHashRequest): Promise<QueryDenomHashResponse> {
    const data = QueryDenomHashRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.applications.transfer.v1.Query",
      "DenomHash",
      data
    );
    return promise.then((data) =>
      QueryDenomHashResponse.decode(new Reader(data))
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
