/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Grant } from "../../../cosmos/feegrant/v1beta1/feegrant";
import {
  PageRequest,
  PageResponse,
} from "../../../cosmos/base/query/v1beta1/pagination";

export const protobufPackage = "cosmos.feegrant.v1beta1";

/** Since: cosmos-sdk 0.43 */

/** QueryAllowanceRequest is the request type for the Query/Allowance RPC method. */
export interface QueryAllowanceRequest {
  /** granter is the address of the user granting an allowance of their funds. */
  granter: string;
  /** grantee is the address of the user being granted an allowance of another user's funds. */
  grantee: string;
}

/** QueryAllowanceResponse is the response type for the Query/Allowance RPC method. */
export interface QueryAllowanceResponse {
  /** allowance is a allowance granted for grantee by granter. */
  allowance: Grant | undefined;
}

/** QueryAllowancesRequest is the request type for the Query/Allowances RPC method. */
export interface QueryAllowancesRequest {
  grantee: string;
  /** pagination defines an pagination for the request. */
  pagination: PageRequest | undefined;
}

/** QueryAllowancesResponse is the response type for the Query/Allowances RPC method. */
export interface QueryAllowancesResponse {
  /** allowances are allowance's granted for grantee by granter. */
  allowances: Grant[];
  /** pagination defines an pagination for the response. */
  pagination: PageResponse | undefined;
}

const baseQueryAllowanceRequest: object = { granter: "", grantee: "" };

export const QueryAllowanceRequest = {
  encode(
    message: QueryAllowanceRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.granter !== "") {
      writer.uint32(10).string(message.granter);
    }
    if (message.grantee !== "") {
      writer.uint32(18).string(message.grantee);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryAllowanceRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryAllowanceRequest } as QueryAllowanceRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.granter = reader.string();
          break;
        case 2:
          message.grantee = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryAllowanceRequest {
    const message = { ...baseQueryAllowanceRequest } as QueryAllowanceRequest;
    if (object.granter !== undefined && object.granter !== null) {
      message.granter = String(object.granter);
    } else {
      message.granter = "";
    }
    if (object.grantee !== undefined && object.grantee !== null) {
      message.grantee = String(object.grantee);
    } else {
      message.grantee = "";
    }
    return message;
  },

  toJSON(message: QueryAllowanceRequest): unknown {
    const obj: any = {};
    message.granter !== undefined && (obj.granter = message.granter);
    message.grantee !== undefined && (obj.grantee = message.grantee);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryAllowanceRequest>
  ): QueryAllowanceRequest {
    const message = { ...baseQueryAllowanceRequest } as QueryAllowanceRequest;
    if (object.granter !== undefined && object.granter !== null) {
      message.granter = object.granter;
    } else {
      message.granter = "";
    }
    if (object.grantee !== undefined && object.grantee !== null) {
      message.grantee = object.grantee;
    } else {
      message.grantee = "";
    }
    return message;
  },
};

const baseQueryAllowanceResponse: object = {};

export const QueryAllowanceResponse = {
  encode(
    message: QueryAllowanceResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.allowance !== undefined) {
      Grant.encode(message.allowance, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryAllowanceResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryAllowanceResponse } as QueryAllowanceResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.allowance = Grant.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryAllowanceResponse {
    const message = { ...baseQueryAllowanceResponse } as QueryAllowanceResponse;
    if (object.allowance !== undefined && object.allowance !== null) {
      message.allowance = Grant.fromJSON(object.allowance);
    } else {
      message.allowance = undefined;
    }
    return message;
  },

  toJSON(message: QueryAllowanceResponse): unknown {
    const obj: any = {};
    message.allowance !== undefined &&
      (obj.allowance = message.allowance
        ? Grant.toJSON(message.allowance)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryAllowanceResponse>
  ): QueryAllowanceResponse {
    const message = { ...baseQueryAllowanceResponse } as QueryAllowanceResponse;
    if (object.allowance !== undefined && object.allowance !== null) {
      message.allowance = Grant.fromPartial(object.allowance);
    } else {
      message.allowance = undefined;
    }
    return message;
  },
};

const baseQueryAllowancesRequest: object = { grantee: "" };

export const QueryAllowancesRequest = {
  encode(
    message: QueryAllowancesRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.grantee !== "") {
      writer.uint32(10).string(message.grantee);
    }
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryAllowancesRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryAllowancesRequest } as QueryAllowancesRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.grantee = reader.string();
          break;
        case 2:
          message.pagination = PageRequest.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryAllowancesRequest {
    const message = { ...baseQueryAllowancesRequest } as QueryAllowancesRequest;
    if (object.grantee !== undefined && object.grantee !== null) {
      message.grantee = String(object.grantee);
    } else {
      message.grantee = "";
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryAllowancesRequest): unknown {
    const obj: any = {};
    message.grantee !== undefined && (obj.grantee = message.grantee);
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryAllowancesRequest>
  ): QueryAllowancesRequest {
    const message = { ...baseQueryAllowancesRequest } as QueryAllowancesRequest;
    if (object.grantee !== undefined && object.grantee !== null) {
      message.grantee = object.grantee;
    } else {
      message.grantee = "";
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryAllowancesResponse: object = {};

export const QueryAllowancesResponse = {
  encode(
    message: QueryAllowancesResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.allowances) {
      Grant.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.pagination !== undefined) {
      PageResponse.encode(
        message.pagination,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryAllowancesResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryAllowancesResponse,
    } as QueryAllowancesResponse;
    message.allowances = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.allowances.push(Grant.decode(reader, reader.uint32()));
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

  fromJSON(object: any): QueryAllowancesResponse {
    const message = {
      ...baseQueryAllowancesResponse,
    } as QueryAllowancesResponse;
    message.allowances = [];
    if (object.allowances !== undefined && object.allowances !== null) {
      for (const e of object.allowances) {
        message.allowances.push(Grant.fromJSON(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryAllowancesResponse): unknown {
    const obj: any = {};
    if (message.allowances) {
      obj.allowances = message.allowances.map((e) =>
        e ? Grant.toJSON(e) : undefined
      );
    } else {
      obj.allowances = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryAllowancesResponse>
  ): QueryAllowancesResponse {
    const message = {
      ...baseQueryAllowancesResponse,
    } as QueryAllowancesResponse;
    message.allowances = [];
    if (object.allowances !== undefined && object.allowances !== null) {
      for (const e of object.allowances) {
        message.allowances.push(Grant.fromPartial(e));
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

/** Query defines the gRPC querier service. */
export interface Query {
  /** Allowance returns fee granted to the grantee by the granter. */
  Allowance(request: QueryAllowanceRequest): Promise<QueryAllowanceResponse>;
  /** Allowances returns all the grants for address. */
  Allowances(request: QueryAllowancesRequest): Promise<QueryAllowancesResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  Allowance(request: QueryAllowanceRequest): Promise<QueryAllowanceResponse> {
    const data = QueryAllowanceRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.feegrant.v1beta1.Query",
      "Allowance",
      data
    );
    return promise.then((data) =>
      QueryAllowanceResponse.decode(new Reader(data))
    );
  }

  Allowances(
    request: QueryAllowancesRequest
  ): Promise<QueryAllowancesResponse> {
    const data = QueryAllowancesRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.feegrant.v1beta1.Query",
      "Allowances",
      data
    );
    return promise.then((data) =>
      QueryAllowancesResponse.decode(new Reader(data))
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
