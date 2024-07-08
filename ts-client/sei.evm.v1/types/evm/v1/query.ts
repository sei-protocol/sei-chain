/* eslint-disable */
import {
  PointerType,
  pointerTypeFromJSON,
  pointerTypeToJSON,
} from "../../evm/v1/enums";
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";

export const protobufPackage = "sei.evm.v1";

export interface QuerySeiAddressByEVMAddressRequest {
  evmAddress: string;
}

export interface QuerySeiAddressByEVMAddressResponse {
  seiAddress: string;
  associated: boolean;
}

export interface QueryEVMAddressBySeiAddressRequest {
  seiAddress: string;
}

export interface QueryEVMAddressBySeiAddressResponse {
  evmAddress: string;
  associated: boolean;
}

export interface QueryStaticCallRequest {
  data: Uint8Array;
  to: string;
}

export interface QueryStaticCallResponse {
  data: Uint8Array;
}

export interface QueryPointerRequest {
  pointerType: PointerType;
  pointee: string;
}

export interface QueryPointerResponse {
  pointer: string;
  version: number;
  exists: boolean;
}

export interface QueryPointerVersionRequest {
  pointerType: PointerType;
}

export interface QueryPointerVersionResponse {
  version: number;
  cwCodeId: number;
}

const baseQuerySeiAddressByEVMAddressRequest: object = { evmAddress: "" };

export const QuerySeiAddressByEVMAddressRequest = {
  encode(
    message: QuerySeiAddressByEVMAddressRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.evmAddress !== "") {
      writer.uint32(10).string(message.evmAddress);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QuerySeiAddressByEVMAddressRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQuerySeiAddressByEVMAddressRequest,
    } as QuerySeiAddressByEVMAddressRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.evmAddress = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QuerySeiAddressByEVMAddressRequest {
    const message = {
      ...baseQuerySeiAddressByEVMAddressRequest,
    } as QuerySeiAddressByEVMAddressRequest;
    if (object.evmAddress !== undefined && object.evmAddress !== null) {
      message.evmAddress = String(object.evmAddress);
    } else {
      message.evmAddress = "";
    }
    return message;
  },

  toJSON(message: QuerySeiAddressByEVMAddressRequest): unknown {
    const obj: any = {};
    message.evmAddress !== undefined && (obj.evmAddress = message.evmAddress);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QuerySeiAddressByEVMAddressRequest>
  ): QuerySeiAddressByEVMAddressRequest {
    const message = {
      ...baseQuerySeiAddressByEVMAddressRequest,
    } as QuerySeiAddressByEVMAddressRequest;
    if (object.evmAddress !== undefined && object.evmAddress !== null) {
      message.evmAddress = object.evmAddress;
    } else {
      message.evmAddress = "";
    }
    return message;
  },
};

const baseQuerySeiAddressByEVMAddressResponse: object = {
  seiAddress: "",
  associated: false,
};

export const QuerySeiAddressByEVMAddressResponse = {
  encode(
    message: QuerySeiAddressByEVMAddressResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.seiAddress !== "") {
      writer.uint32(10).string(message.seiAddress);
    }
    if (message.associated === true) {
      writer.uint32(16).bool(message.associated);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QuerySeiAddressByEVMAddressResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQuerySeiAddressByEVMAddressResponse,
    } as QuerySeiAddressByEVMAddressResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.seiAddress = reader.string();
          break;
        case 2:
          message.associated = reader.bool();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QuerySeiAddressByEVMAddressResponse {
    const message = {
      ...baseQuerySeiAddressByEVMAddressResponse,
    } as QuerySeiAddressByEVMAddressResponse;
    if (object.seiAddress !== undefined && object.seiAddress !== null) {
      message.seiAddress = String(object.seiAddress);
    } else {
      message.seiAddress = "";
    }
    if (object.associated !== undefined && object.associated !== null) {
      message.associated = Boolean(object.associated);
    } else {
      message.associated = false;
    }
    return message;
  },

  toJSON(message: QuerySeiAddressByEVMAddressResponse): unknown {
    const obj: any = {};
    message.seiAddress !== undefined && (obj.seiAddress = message.seiAddress);
    message.associated !== undefined && (obj.associated = message.associated);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QuerySeiAddressByEVMAddressResponse>
  ): QuerySeiAddressByEVMAddressResponse {
    const message = {
      ...baseQuerySeiAddressByEVMAddressResponse,
    } as QuerySeiAddressByEVMAddressResponse;
    if (object.seiAddress !== undefined && object.seiAddress !== null) {
      message.seiAddress = object.seiAddress;
    } else {
      message.seiAddress = "";
    }
    if (object.associated !== undefined && object.associated !== null) {
      message.associated = object.associated;
    } else {
      message.associated = false;
    }
    return message;
  },
};

const baseQueryEVMAddressBySeiAddressRequest: object = { seiAddress: "" };

export const QueryEVMAddressBySeiAddressRequest = {
  encode(
    message: QueryEVMAddressBySeiAddressRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.seiAddress !== "") {
      writer.uint32(10).string(message.seiAddress);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryEVMAddressBySeiAddressRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryEVMAddressBySeiAddressRequest,
    } as QueryEVMAddressBySeiAddressRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.seiAddress = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryEVMAddressBySeiAddressRequest {
    const message = {
      ...baseQueryEVMAddressBySeiAddressRequest,
    } as QueryEVMAddressBySeiAddressRequest;
    if (object.seiAddress !== undefined && object.seiAddress !== null) {
      message.seiAddress = String(object.seiAddress);
    } else {
      message.seiAddress = "";
    }
    return message;
  },

  toJSON(message: QueryEVMAddressBySeiAddressRequest): unknown {
    const obj: any = {};
    message.seiAddress !== undefined && (obj.seiAddress = message.seiAddress);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryEVMAddressBySeiAddressRequest>
  ): QueryEVMAddressBySeiAddressRequest {
    const message = {
      ...baseQueryEVMAddressBySeiAddressRequest,
    } as QueryEVMAddressBySeiAddressRequest;
    if (object.seiAddress !== undefined && object.seiAddress !== null) {
      message.seiAddress = object.seiAddress;
    } else {
      message.seiAddress = "";
    }
    return message;
  },
};

const baseQueryEVMAddressBySeiAddressResponse: object = {
  evmAddress: "",
  associated: false,
};

export const QueryEVMAddressBySeiAddressResponse = {
  encode(
    message: QueryEVMAddressBySeiAddressResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.evmAddress !== "") {
      writer.uint32(10).string(message.evmAddress);
    }
    if (message.associated === true) {
      writer.uint32(16).bool(message.associated);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryEVMAddressBySeiAddressResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryEVMAddressBySeiAddressResponse,
    } as QueryEVMAddressBySeiAddressResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.evmAddress = reader.string();
          break;
        case 2:
          message.associated = reader.bool();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryEVMAddressBySeiAddressResponse {
    const message = {
      ...baseQueryEVMAddressBySeiAddressResponse,
    } as QueryEVMAddressBySeiAddressResponse;
    if (object.evmAddress !== undefined && object.evmAddress !== null) {
      message.evmAddress = String(object.evmAddress);
    } else {
      message.evmAddress = "";
    }
    if (object.associated !== undefined && object.associated !== null) {
      message.associated = Boolean(object.associated);
    } else {
      message.associated = false;
    }
    return message;
  },

  toJSON(message: QueryEVMAddressBySeiAddressResponse): unknown {
    const obj: any = {};
    message.evmAddress !== undefined && (obj.evmAddress = message.evmAddress);
    message.associated !== undefined && (obj.associated = message.associated);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryEVMAddressBySeiAddressResponse>
  ): QueryEVMAddressBySeiAddressResponse {
    const message = {
      ...baseQueryEVMAddressBySeiAddressResponse,
    } as QueryEVMAddressBySeiAddressResponse;
    if (object.evmAddress !== undefined && object.evmAddress !== null) {
      message.evmAddress = object.evmAddress;
    } else {
      message.evmAddress = "";
    }
    if (object.associated !== undefined && object.associated !== null) {
      message.associated = object.associated;
    } else {
      message.associated = false;
    }
    return message;
  },
};

const baseQueryStaticCallRequest: object = { to: "" };

export const QueryStaticCallRequest = {
  encode(
    message: QueryStaticCallRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.data.length !== 0) {
      writer.uint32(10).bytes(message.data);
    }
    if (message.to !== "") {
      writer.uint32(18).string(message.to);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryStaticCallRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryStaticCallRequest } as QueryStaticCallRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.data = reader.bytes();
          break;
        case 2:
          message.to = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryStaticCallRequest {
    const message = { ...baseQueryStaticCallRequest } as QueryStaticCallRequest;
    if (object.data !== undefined && object.data !== null) {
      message.data = bytesFromBase64(object.data);
    }
    if (object.to !== undefined && object.to !== null) {
      message.to = String(object.to);
    } else {
      message.to = "";
    }
    return message;
  },

  toJSON(message: QueryStaticCallRequest): unknown {
    const obj: any = {};
    message.data !== undefined &&
      (obj.data = base64FromBytes(
        message.data !== undefined ? message.data : new Uint8Array()
      ));
    message.to !== undefined && (obj.to = message.to);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryStaticCallRequest>
  ): QueryStaticCallRequest {
    const message = { ...baseQueryStaticCallRequest } as QueryStaticCallRequest;
    if (object.data !== undefined && object.data !== null) {
      message.data = object.data;
    } else {
      message.data = new Uint8Array();
    }
    if (object.to !== undefined && object.to !== null) {
      message.to = object.to;
    } else {
      message.to = "";
    }
    return message;
  },
};

const baseQueryStaticCallResponse: object = {};

export const QueryStaticCallResponse = {
  encode(
    message: QueryStaticCallResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.data.length !== 0) {
      writer.uint32(10).bytes(message.data);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryStaticCallResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryStaticCallResponse,
    } as QueryStaticCallResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.data = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryStaticCallResponse {
    const message = {
      ...baseQueryStaticCallResponse,
    } as QueryStaticCallResponse;
    if (object.data !== undefined && object.data !== null) {
      message.data = bytesFromBase64(object.data);
    }
    return message;
  },

  toJSON(message: QueryStaticCallResponse): unknown {
    const obj: any = {};
    message.data !== undefined &&
      (obj.data = base64FromBytes(
        message.data !== undefined ? message.data : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryStaticCallResponse>
  ): QueryStaticCallResponse {
    const message = {
      ...baseQueryStaticCallResponse,
    } as QueryStaticCallResponse;
    if (object.data !== undefined && object.data !== null) {
      message.data = object.data;
    } else {
      message.data = new Uint8Array();
    }
    return message;
  },
};

const baseQueryPointerRequest: object = { pointerType: 0, pointee: "" };

export const QueryPointerRequest = {
  encode(
    message: QueryPointerRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.pointerType !== 0) {
      writer.uint32(8).int32(message.pointerType);
    }
    if (message.pointee !== "") {
      writer.uint32(18).string(message.pointee);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryPointerRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryPointerRequest } as QueryPointerRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.pointerType = reader.int32() as any;
          break;
        case 2:
          message.pointee = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryPointerRequest {
    const message = { ...baseQueryPointerRequest } as QueryPointerRequest;
    if (object.pointerType !== undefined && object.pointerType !== null) {
      message.pointerType = pointerTypeFromJSON(object.pointerType);
    } else {
      message.pointerType = 0;
    }
    if (object.pointee !== undefined && object.pointee !== null) {
      message.pointee = String(object.pointee);
    } else {
      message.pointee = "";
    }
    return message;
  },

  toJSON(message: QueryPointerRequest): unknown {
    const obj: any = {};
    message.pointerType !== undefined &&
      (obj.pointerType = pointerTypeToJSON(message.pointerType));
    message.pointee !== undefined && (obj.pointee = message.pointee);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryPointerRequest>): QueryPointerRequest {
    const message = { ...baseQueryPointerRequest } as QueryPointerRequest;
    if (object.pointerType !== undefined && object.pointerType !== null) {
      message.pointerType = object.pointerType;
    } else {
      message.pointerType = 0;
    }
    if (object.pointee !== undefined && object.pointee !== null) {
      message.pointee = object.pointee;
    } else {
      message.pointee = "";
    }
    return message;
  },
};

const baseQueryPointerResponse: object = {
  pointer: "",
  version: 0,
  exists: false,
};

export const QueryPointerResponse = {
  encode(
    message: QueryPointerResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.pointer !== "") {
      writer.uint32(10).string(message.pointer);
    }
    if (message.version !== 0) {
      writer.uint32(16).uint32(message.version);
    }
    if (message.exists === true) {
      writer.uint32(24).bool(message.exists);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryPointerResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryPointerResponse } as QueryPointerResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.pointer = reader.string();
          break;
        case 2:
          message.version = reader.uint32();
          break;
        case 3:
          message.exists = reader.bool();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryPointerResponse {
    const message = { ...baseQueryPointerResponse } as QueryPointerResponse;
    if (object.pointer !== undefined && object.pointer !== null) {
      message.pointer = String(object.pointer);
    } else {
      message.pointer = "";
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = Number(object.version);
    } else {
      message.version = 0;
    }
    if (object.exists !== undefined && object.exists !== null) {
      message.exists = Boolean(object.exists);
    } else {
      message.exists = false;
    }
    return message;
  },

  toJSON(message: QueryPointerResponse): unknown {
    const obj: any = {};
    message.pointer !== undefined && (obj.pointer = message.pointer);
    message.version !== undefined && (obj.version = message.version);
    message.exists !== undefined && (obj.exists = message.exists);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryPointerResponse>): QueryPointerResponse {
    const message = { ...baseQueryPointerResponse } as QueryPointerResponse;
    if (object.pointer !== undefined && object.pointer !== null) {
      message.pointer = object.pointer;
    } else {
      message.pointer = "";
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = object.version;
    } else {
      message.version = 0;
    }
    if (object.exists !== undefined && object.exists !== null) {
      message.exists = object.exists;
    } else {
      message.exists = false;
    }
    return message;
  },
};

const baseQueryPointerVersionRequest: object = { pointerType: 0 };

export const QueryPointerVersionRequest = {
  encode(
    message: QueryPointerVersionRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.pointerType !== 0) {
      writer.uint32(8).int32(message.pointerType);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryPointerVersionRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryPointerVersionRequest,
    } as QueryPointerVersionRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.pointerType = reader.int32() as any;
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryPointerVersionRequest {
    const message = {
      ...baseQueryPointerVersionRequest,
    } as QueryPointerVersionRequest;
    if (object.pointerType !== undefined && object.pointerType !== null) {
      message.pointerType = pointerTypeFromJSON(object.pointerType);
    } else {
      message.pointerType = 0;
    }
    return message;
  },

  toJSON(message: QueryPointerVersionRequest): unknown {
    const obj: any = {};
    message.pointerType !== undefined &&
      (obj.pointerType = pointerTypeToJSON(message.pointerType));
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryPointerVersionRequest>
  ): QueryPointerVersionRequest {
    const message = {
      ...baseQueryPointerVersionRequest,
    } as QueryPointerVersionRequest;
    if (object.pointerType !== undefined && object.pointerType !== null) {
      message.pointerType = object.pointerType;
    } else {
      message.pointerType = 0;
    }
    return message;
  },
};

const baseQueryPointerVersionResponse: object = { version: 0, cwCodeId: 0 };

export const QueryPointerVersionResponse = {
  encode(
    message: QueryPointerVersionResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.version !== 0) {
      writer.uint32(8).uint32(message.version);
    }
    if (message.cwCodeId !== 0) {
      writer.uint32(16).uint64(message.cwCodeId);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryPointerVersionResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryPointerVersionResponse,
    } as QueryPointerVersionResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.version = reader.uint32();
          break;
        case 2:
          message.cwCodeId = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryPointerVersionResponse {
    const message = {
      ...baseQueryPointerVersionResponse,
    } as QueryPointerVersionResponse;
    if (object.version !== undefined && object.version !== null) {
      message.version = Number(object.version);
    } else {
      message.version = 0;
    }
    if (object.cwCodeId !== undefined && object.cwCodeId !== null) {
      message.cwCodeId = Number(object.cwCodeId);
    } else {
      message.cwCodeId = 0;
    }
    return message;
  },

  toJSON(message: QueryPointerVersionResponse): unknown {
    const obj: any = {};
    message.version !== undefined && (obj.version = message.version);
    message.cwCodeId !== undefined && (obj.cwCodeId = message.cwCodeId);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryPointerVersionResponse>
  ): QueryPointerVersionResponse {
    const message = {
      ...baseQueryPointerVersionResponse,
    } as QueryPointerVersionResponse;
    if (object.version !== undefined && object.version !== null) {
      message.version = object.version;
    } else {
      message.version = 0;
    }
    if (object.cwCodeId !== undefined && object.cwCodeId !== null) {
      message.cwCodeId = object.cwCodeId;
    } else {
      message.cwCodeId = 0;
    }
    return message;
  },
};

/** Query defines the gRPC querier service. */
export interface Query {
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/evm/v1/sei_address` instead.
   *
   * @deprecated
   */
  deprecated_SeiAddressByEVMAddress(
    request: QuerySeiAddressByEVMAddressRequest
  ): Promise<QuerySeiAddressByEVMAddressResponse>;
  /** SeiAddressByEVMAddress returns the sei cosmos address associated with an EVM address. */
  SeiAddressByEVMAddress(
    request: QuerySeiAddressByEVMAddressRequest
  ): Promise<QuerySeiAddressByEVMAddressResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/evm/v1/evm_address` instead.
   *
   * @deprecated
   */
  deprecated_EVMAddressBySeiAddress(
    request: QueryEVMAddressBySeiAddressRequest
  ): Promise<QueryEVMAddressBySeiAddressResponse>;
  /** EVMAddressBySeiAddress returns the EVM address associated with the given sei cosmos address. */
  EVMAddressBySeiAddress(
    request: QueryEVMAddressBySeiAddressRequest
  ): Promise<QueryEVMAddressBySeiAddressResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/evm/v1/static_call` instead.
   *
   * @deprecated
   */
  deprecated_StaticCall(
    request: QueryStaticCallRequest
  ): Promise<QueryStaticCallResponse>;
  StaticCall(request: QueryStaticCallRequest): Promise<QueryStaticCallResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/evm/v1/pointer` instead.
   *
   * @deprecated
   */
  deprecated_Pointer(
    request: QueryPointerRequest
  ): Promise<QueryPointerResponse>;
  Pointer(request: QueryPointerRequest): Promise<QueryPointerResponse>;
  /**
   * This endpoint is deprecated and will be removed in the future. Please use the `/sei/evm/v1/pointer_version` instead.
   *
   * @deprecated
   */
  deprecated_PointerVersion(
    request: QueryPointerVersionRequest
  ): Promise<QueryPointerVersionResponse>;
  PointerVersion(
    request: QueryPointerVersionRequest
  ): Promise<QueryPointerVersionResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  deprecated_SeiAddressByEVMAddress(
    request: QuerySeiAddressByEVMAddressRequest
  ): Promise<QuerySeiAddressByEVMAddressResponse> {
    const data = QuerySeiAddressByEVMAddressRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.evm.v1.Query",
      "deprecated_SeiAddressByEVMAddress",
      data
    );
    return promise.then((data) =>
      QuerySeiAddressByEVMAddressResponse.decode(new Reader(data))
    );
  }

  SeiAddressByEVMAddress(
    request: QuerySeiAddressByEVMAddressRequest
  ): Promise<QuerySeiAddressByEVMAddressResponse> {
    const data = QuerySeiAddressByEVMAddressRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.evm.v1.Query",
      "SeiAddressByEVMAddress",
      data
    );
    return promise.then((data) =>
      QuerySeiAddressByEVMAddressResponse.decode(new Reader(data))
    );
  }

  deprecated_EVMAddressBySeiAddress(
    request: QueryEVMAddressBySeiAddressRequest
  ): Promise<QueryEVMAddressBySeiAddressResponse> {
    const data = QueryEVMAddressBySeiAddressRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.evm.v1.Query",
      "deprecated_EVMAddressBySeiAddress",
      data
    );
    return promise.then((data) =>
      QueryEVMAddressBySeiAddressResponse.decode(new Reader(data))
    );
  }

  EVMAddressBySeiAddress(
    request: QueryEVMAddressBySeiAddressRequest
  ): Promise<QueryEVMAddressBySeiAddressResponse> {
    const data = QueryEVMAddressBySeiAddressRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.evm.v1.Query",
      "EVMAddressBySeiAddress",
      data
    );
    return promise.then((data) =>
      QueryEVMAddressBySeiAddressResponse.decode(new Reader(data))
    );
  }

  deprecated_StaticCall(
    request: QueryStaticCallRequest
  ): Promise<QueryStaticCallResponse> {
    const data = QueryStaticCallRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.evm.v1.Query",
      "deprecated_StaticCall",
      data
    );
    return promise.then((data) =>
      QueryStaticCallResponse.decode(new Reader(data))
    );
  }

  StaticCall(
    request: QueryStaticCallRequest
  ): Promise<QueryStaticCallResponse> {
    const data = QueryStaticCallRequest.encode(request).finish();
    const promise = this.rpc.request("sei.evm.v1.Query", "StaticCall", data);
    return promise.then((data) =>
      QueryStaticCallResponse.decode(new Reader(data))
    );
  }

  deprecated_Pointer(
    request: QueryPointerRequest
  ): Promise<QueryPointerResponse> {
    const data = QueryPointerRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.evm.v1.Query",
      "deprecated_Pointer",
      data
    );
    return promise.then((data) =>
      QueryPointerResponse.decode(new Reader(data))
    );
  }

  Pointer(request: QueryPointerRequest): Promise<QueryPointerResponse> {
    const data = QueryPointerRequest.encode(request).finish();
    const promise = this.rpc.request("sei.evm.v1.Query", "Pointer", data);
    return promise.then((data) =>
      QueryPointerResponse.decode(new Reader(data))
    );
  }

  deprecated_PointerVersion(
    request: QueryPointerVersionRequest
  ): Promise<QueryPointerVersionResponse> {
    const data = QueryPointerVersionRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.evm.v1.Query",
      "deprecated_PointerVersion",
      data
    );
    return promise.then((data) =>
      QueryPointerVersionResponse.decode(new Reader(data))
    );
  }

  PointerVersion(
    request: QueryPointerVersionRequest
  ): Promise<QueryPointerVersionResponse> {
    const data = QueryPointerVersionRequest.encode(request).finish();
    const promise = this.rpc.request(
      "sei.evm.v1.Query",
      "PointerVersion",
      data
    );
    return promise.then((data) =>
      QueryPointerVersionResponse.decode(new Reader(data))
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
