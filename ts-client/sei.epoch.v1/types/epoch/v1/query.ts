/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Params } from "../../epoch/v1/params";
import { Epoch } from "../../epoch/v1/epoch";

export const protobufPackage = "sei.epoch.v1";

/** QueryParamsRequest is request type for the Query/Params RPC method. */
export interface QueryParamsRequest {}

/** QueryParamsResponse is response type for the Query/Params RPC method. */
export interface QueryParamsResponse {
  /** params holds all the parameters of this module. */
  params: Params | undefined;
}

export interface QueryEpochRequest {}

export interface QueryEpochResponse {
  epoch: Epoch | undefined;
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

const baseQueryEpochRequest: object = {};

export const QueryEpochRequest = {
  encode(_: QueryEpochRequest, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryEpochRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryEpochRequest } as QueryEpochRequest;
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

  fromJSON(_: any): QueryEpochRequest {
    const message = { ...baseQueryEpochRequest } as QueryEpochRequest;
    return message;
  },

  toJSON(_: QueryEpochRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(_: DeepPartial<QueryEpochRequest>): QueryEpochRequest {
    const message = { ...baseQueryEpochRequest } as QueryEpochRequest;
    return message;
  },
};

const baseQueryEpochResponse: object = {};

export const QueryEpochResponse = {
  encode(
    message: QueryEpochResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.epoch !== undefined) {
      Epoch.encode(message.epoch, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryEpochResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryEpochResponse } as QueryEpochResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.epoch = Epoch.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryEpochResponse {
    const message = { ...baseQueryEpochResponse } as QueryEpochResponse;
    if (object.epoch !== undefined && object.epoch !== null) {
      message.epoch = Epoch.fromJSON(object.epoch);
    } else {
      message.epoch = undefined;
    }
    return message;
  },

  toJSON(message: QueryEpochResponse): unknown {
    const obj: any = {};
    message.epoch !== undefined &&
      (obj.epoch = message.epoch ? Epoch.toJSON(message.epoch) : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryEpochResponse>): QueryEpochResponse {
    const message = { ...baseQueryEpochResponse } as QueryEpochResponse;
    if (object.epoch !== undefined && object.epoch !== null) {
      message.epoch = Epoch.fromPartial(object.epoch);
    } else {
      message.epoch = undefined;
    }
    return message;
  },
};

/** Query defines the gRPC querier service. */
export interface Query {
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/epoch/v1/epoch` instead.
   *
   * @deprecated
   */
  deprecated_Epoch(request: QueryEpochRequest): Promise<QueryEpochResponse>;
  /** Query the epoch in the chain */
  Epoch(request: QueryEpochRequest): Promise<QueryEpochResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/epoch/v1/params` instead.
   *
   * @deprecated
   */
  deprecated_Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
  /** Parameters queries the parameters of the module. */
  Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  deprecated_Epoch(request: QueryEpochRequest): Promise<QueryEpochResponse> {
    const data = QueryEpochRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.epoch.v1.Query",
      "deprecated_Epoch",
      data
    );
    return promise.then((data) => QueryEpochResponse.decode(new Reader(data)));
  }

  Epoch(request: QueryEpochRequest): Promise<QueryEpochResponse> {
    const data = QueryEpochRequest.encode(request).finish();
    const promise = this.rpc.request("sei.epoch.v1.Query", "Epoch", data);
    return promise.then((data) => QueryEpochResponse.decode(new Reader(data)));
  }

  deprecated_Params(request: QueryParamsRequest): Promise<QueryParamsResponse> {
    const data = QueryParamsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.epoch.v1.Query",
      "deprecated_Params",
      data
    );
    return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
  }

  Params(request: QueryParamsRequest): Promise<QueryParamsResponse> {
    const data = QueryParamsRequest.encode(request).finish();
    const promise = this.rpc.request("sei.epoch.v1.Query", "Params", data);
    return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
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
