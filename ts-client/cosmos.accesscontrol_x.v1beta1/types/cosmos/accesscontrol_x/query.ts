/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Params } from "../../cosmos/accesscontrol_x/genesis";
import {
  MessageDependencyMapping,
  WasmDependencyMapping,
} from "../../cosmos/accesscontrol/accesscontrol";

export const protobufPackage = "cosmos.accesscontrol_x.v1beta1";

export interface QueryParamsRequest {}

export interface QueryParamsResponse {
  /** params defines the parameters of the module. */
  params: Params | undefined;
}

export interface ResourceDependencyMappingFromMessageKeyRequest {
  messageKey: string;
}

export interface ResourceDependencyMappingFromMessageKeyResponse {
  messageDependencyMapping: MessageDependencyMapping | undefined;
}

export interface WasmDependencyMappingRequest {
  contractAddress: string;
}

export interface WasmDependencyMappingResponse {
  wasmDependencyMapping: WasmDependencyMapping | undefined;
}

export interface ListResourceDependencyMappingRequest {}

export interface ListResourceDependencyMappingResponse {
  messageDependencyMappingList: MessageDependencyMapping[];
}

export interface ListWasmDependencyMappingRequest {}

export interface ListWasmDependencyMappingResponse {
  wasmDependencyMappingList: WasmDependencyMapping[];
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

const baseResourceDependencyMappingFromMessageKeyRequest: object = {
  messageKey: "",
};

export const ResourceDependencyMappingFromMessageKeyRequest = {
  encode(
    message: ResourceDependencyMappingFromMessageKeyRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.messageKey !== "") {
      writer.uint32(10).string(message.messageKey);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ResourceDependencyMappingFromMessageKeyRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseResourceDependencyMappingFromMessageKeyRequest,
    } as ResourceDependencyMappingFromMessageKeyRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.messageKey = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ResourceDependencyMappingFromMessageKeyRequest {
    const message = {
      ...baseResourceDependencyMappingFromMessageKeyRequest,
    } as ResourceDependencyMappingFromMessageKeyRequest;
    if (object.messageKey !== undefined && object.messageKey !== null) {
      message.messageKey = String(object.messageKey);
    } else {
      message.messageKey = "";
    }
    return message;
  },

  toJSON(message: ResourceDependencyMappingFromMessageKeyRequest): unknown {
    const obj: any = {};
    message.messageKey !== undefined && (obj.messageKey = message.messageKey);
    return obj;
  },

  fromPartial(
    object: DeepPartial<ResourceDependencyMappingFromMessageKeyRequest>
  ): ResourceDependencyMappingFromMessageKeyRequest {
    const message = {
      ...baseResourceDependencyMappingFromMessageKeyRequest,
    } as ResourceDependencyMappingFromMessageKeyRequest;
    if (object.messageKey !== undefined && object.messageKey !== null) {
      message.messageKey = object.messageKey;
    } else {
      message.messageKey = "";
    }
    return message;
  },
};

const baseResourceDependencyMappingFromMessageKeyResponse: object = {};

export const ResourceDependencyMappingFromMessageKeyResponse = {
  encode(
    message: ResourceDependencyMappingFromMessageKeyResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.messageDependencyMapping !== undefined) {
      MessageDependencyMapping.encode(
        message.messageDependencyMapping,
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ResourceDependencyMappingFromMessageKeyResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseResourceDependencyMappingFromMessageKeyResponse,
    } as ResourceDependencyMappingFromMessageKeyResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.messageDependencyMapping = MessageDependencyMapping.decode(
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

  fromJSON(object: any): ResourceDependencyMappingFromMessageKeyResponse {
    const message = {
      ...baseResourceDependencyMappingFromMessageKeyResponse,
    } as ResourceDependencyMappingFromMessageKeyResponse;
    if (
      object.messageDependencyMapping !== undefined &&
      object.messageDependencyMapping !== null
    ) {
      message.messageDependencyMapping = MessageDependencyMapping.fromJSON(
        object.messageDependencyMapping
      );
    } else {
      message.messageDependencyMapping = undefined;
    }
    return message;
  },

  toJSON(message: ResourceDependencyMappingFromMessageKeyResponse): unknown {
    const obj: any = {};
    message.messageDependencyMapping !== undefined &&
      (obj.messageDependencyMapping = message.messageDependencyMapping
        ? MessageDependencyMapping.toJSON(message.messageDependencyMapping)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<ResourceDependencyMappingFromMessageKeyResponse>
  ): ResourceDependencyMappingFromMessageKeyResponse {
    const message = {
      ...baseResourceDependencyMappingFromMessageKeyResponse,
    } as ResourceDependencyMappingFromMessageKeyResponse;
    if (
      object.messageDependencyMapping !== undefined &&
      object.messageDependencyMapping !== null
    ) {
      message.messageDependencyMapping = MessageDependencyMapping.fromPartial(
        object.messageDependencyMapping
      );
    } else {
      message.messageDependencyMapping = undefined;
    }
    return message;
  },
};

const baseWasmDependencyMappingRequest: object = { contractAddress: "" };

export const WasmDependencyMappingRequest = {
  encode(
    message: WasmDependencyMappingRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.contractAddress !== "") {
      writer.uint32(10).string(message.contractAddress);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): WasmDependencyMappingRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseWasmDependencyMappingRequest,
    } as WasmDependencyMappingRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.contractAddress = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): WasmDependencyMappingRequest {
    const message = {
      ...baseWasmDependencyMappingRequest,
    } as WasmDependencyMappingRequest;
    if (
      object.contractAddress !== undefined &&
      object.contractAddress !== null
    ) {
      message.contractAddress = String(object.contractAddress);
    } else {
      message.contractAddress = "";
    }
    return message;
  },

  toJSON(message: WasmDependencyMappingRequest): unknown {
    const obj: any = {};
    message.contractAddress !== undefined &&
      (obj.contractAddress = message.contractAddress);
    return obj;
  },

  fromPartial(
    object: DeepPartial<WasmDependencyMappingRequest>
  ): WasmDependencyMappingRequest {
    const message = {
      ...baseWasmDependencyMappingRequest,
    } as WasmDependencyMappingRequest;
    if (
      object.contractAddress !== undefined &&
      object.contractAddress !== null
    ) {
      message.contractAddress = object.contractAddress;
    } else {
      message.contractAddress = "";
    }
    return message;
  },
};

const baseWasmDependencyMappingResponse: object = {};

export const WasmDependencyMappingResponse = {
  encode(
    message: WasmDependencyMappingResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.wasmDependencyMapping !== undefined) {
      WasmDependencyMapping.encode(
        message.wasmDependencyMapping,
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): WasmDependencyMappingResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseWasmDependencyMappingResponse,
    } as WasmDependencyMappingResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.wasmDependencyMapping = WasmDependencyMapping.decode(
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

  fromJSON(object: any): WasmDependencyMappingResponse {
    const message = {
      ...baseWasmDependencyMappingResponse,
    } as WasmDependencyMappingResponse;
    if (
      object.wasmDependencyMapping !== undefined &&
      object.wasmDependencyMapping !== null
    ) {
      message.wasmDependencyMapping = WasmDependencyMapping.fromJSON(
        object.wasmDependencyMapping
      );
    } else {
      message.wasmDependencyMapping = undefined;
    }
    return message;
  },

  toJSON(message: WasmDependencyMappingResponse): unknown {
    const obj: any = {};
    message.wasmDependencyMapping !== undefined &&
      (obj.wasmDependencyMapping = message.wasmDependencyMapping
        ? WasmDependencyMapping.toJSON(message.wasmDependencyMapping)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<WasmDependencyMappingResponse>
  ): WasmDependencyMappingResponse {
    const message = {
      ...baseWasmDependencyMappingResponse,
    } as WasmDependencyMappingResponse;
    if (
      object.wasmDependencyMapping !== undefined &&
      object.wasmDependencyMapping !== null
    ) {
      message.wasmDependencyMapping = WasmDependencyMapping.fromPartial(
        object.wasmDependencyMapping
      );
    } else {
      message.wasmDependencyMapping = undefined;
    }
    return message;
  },
};

const baseListResourceDependencyMappingRequest: object = {};

export const ListResourceDependencyMappingRequest = {
  encode(
    _: ListResourceDependencyMappingRequest,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ListResourceDependencyMappingRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseListResourceDependencyMappingRequest,
    } as ListResourceDependencyMappingRequest;
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

  fromJSON(_: any): ListResourceDependencyMappingRequest {
    const message = {
      ...baseListResourceDependencyMappingRequest,
    } as ListResourceDependencyMappingRequest;
    return message;
  },

  toJSON(_: ListResourceDependencyMappingRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<ListResourceDependencyMappingRequest>
  ): ListResourceDependencyMappingRequest {
    const message = {
      ...baseListResourceDependencyMappingRequest,
    } as ListResourceDependencyMappingRequest;
    return message;
  },
};

const baseListResourceDependencyMappingResponse: object = {};

export const ListResourceDependencyMappingResponse = {
  encode(
    message: ListResourceDependencyMappingResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.messageDependencyMappingList) {
      MessageDependencyMapping.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ListResourceDependencyMappingResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseListResourceDependencyMappingResponse,
    } as ListResourceDependencyMappingResponse;
    message.messageDependencyMappingList = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.messageDependencyMappingList.push(
            MessageDependencyMapping.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ListResourceDependencyMappingResponse {
    const message = {
      ...baseListResourceDependencyMappingResponse,
    } as ListResourceDependencyMappingResponse;
    message.messageDependencyMappingList = [];
    if (
      object.messageDependencyMappingList !== undefined &&
      object.messageDependencyMappingList !== null
    ) {
      for (const e of object.messageDependencyMappingList) {
        message.messageDependencyMappingList.push(
          MessageDependencyMapping.fromJSON(e)
        );
      }
    }
    return message;
  },

  toJSON(message: ListResourceDependencyMappingResponse): unknown {
    const obj: any = {};
    if (message.messageDependencyMappingList) {
      obj.messageDependencyMappingList = message.messageDependencyMappingList.map(
        (e) => (e ? MessageDependencyMapping.toJSON(e) : undefined)
      );
    } else {
      obj.messageDependencyMappingList = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<ListResourceDependencyMappingResponse>
  ): ListResourceDependencyMappingResponse {
    const message = {
      ...baseListResourceDependencyMappingResponse,
    } as ListResourceDependencyMappingResponse;
    message.messageDependencyMappingList = [];
    if (
      object.messageDependencyMappingList !== undefined &&
      object.messageDependencyMappingList !== null
    ) {
      for (const e of object.messageDependencyMappingList) {
        message.messageDependencyMappingList.push(
          MessageDependencyMapping.fromPartial(e)
        );
      }
    }
    return message;
  },
};

const baseListWasmDependencyMappingRequest: object = {};

export const ListWasmDependencyMappingRequest = {
  encode(
    _: ListWasmDependencyMappingRequest,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ListWasmDependencyMappingRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseListWasmDependencyMappingRequest,
    } as ListWasmDependencyMappingRequest;
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

  fromJSON(_: any): ListWasmDependencyMappingRequest {
    const message = {
      ...baseListWasmDependencyMappingRequest,
    } as ListWasmDependencyMappingRequest;
    return message;
  },

  toJSON(_: ListWasmDependencyMappingRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<ListWasmDependencyMappingRequest>
  ): ListWasmDependencyMappingRequest {
    const message = {
      ...baseListWasmDependencyMappingRequest,
    } as ListWasmDependencyMappingRequest;
    return message;
  },
};

const baseListWasmDependencyMappingResponse: object = {};

export const ListWasmDependencyMappingResponse = {
  encode(
    message: ListWasmDependencyMappingResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.wasmDependencyMappingList) {
      WasmDependencyMapping.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ListWasmDependencyMappingResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseListWasmDependencyMappingResponse,
    } as ListWasmDependencyMappingResponse;
    message.wasmDependencyMappingList = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.wasmDependencyMappingList.push(
            WasmDependencyMapping.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ListWasmDependencyMappingResponse {
    const message = {
      ...baseListWasmDependencyMappingResponse,
    } as ListWasmDependencyMappingResponse;
    message.wasmDependencyMappingList = [];
    if (
      object.wasmDependencyMappingList !== undefined &&
      object.wasmDependencyMappingList !== null
    ) {
      for (const e of object.wasmDependencyMappingList) {
        message.wasmDependencyMappingList.push(
          WasmDependencyMapping.fromJSON(e)
        );
      }
    }
    return message;
  },

  toJSON(message: ListWasmDependencyMappingResponse): unknown {
    const obj: any = {};
    if (message.wasmDependencyMappingList) {
      obj.wasmDependencyMappingList = message.wasmDependencyMappingList.map(
        (e) => (e ? WasmDependencyMapping.toJSON(e) : undefined)
      );
    } else {
      obj.wasmDependencyMappingList = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<ListWasmDependencyMappingResponse>
  ): ListWasmDependencyMappingResponse {
    const message = {
      ...baseListWasmDependencyMappingResponse,
    } as ListWasmDependencyMappingResponse;
    message.wasmDependencyMappingList = [];
    if (
      object.wasmDependencyMappingList !== undefined &&
      object.wasmDependencyMappingList !== null
    ) {
      for (const e of object.wasmDependencyMappingList) {
        message.wasmDependencyMappingList.push(
          WasmDependencyMapping.fromPartial(e)
        );
      }
    }
    return message;
  },
};

export interface Query {
  Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
  ResourceDependencyMappingFromMessageKey(
    request: ResourceDependencyMappingFromMessageKeyRequest
  ): Promise<ResourceDependencyMappingFromMessageKeyResponse>;
  ListResourceDependencyMapping(
    request: ListResourceDependencyMappingRequest
  ): Promise<ListResourceDependencyMappingResponse>;
  WasmDependencyMapping(
    request: WasmDependencyMappingRequest
  ): Promise<WasmDependencyMappingResponse>;
  ListWasmDependencyMapping(
    request: ListWasmDependencyMappingRequest
  ): Promise<ListWasmDependencyMappingResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  Params(request: QueryParamsRequest): Promise<QueryParamsResponse> {
    const data = QueryParamsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.accesscontrol_x.v1beta1.Query",
      "Params",
      data
    );
    return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
  }

  ResourceDependencyMappingFromMessageKey(
    request: ResourceDependencyMappingFromMessageKeyRequest
  ): Promise<ResourceDependencyMappingFromMessageKeyResponse> {
    const data = ResourceDependencyMappingFromMessageKeyRequest.encode(
      request
    ).finish();
    const promise = this.rpc.request(
      "cosmos.accesscontrol_x.v1beta1.Query",
      "ResourceDependencyMappingFromMessageKey",
      data
    );
    return promise.then((data) =>
      ResourceDependencyMappingFromMessageKeyResponse.decode(new Reader(data))
    );
  }

  ListResourceDependencyMapping(
    request: ListResourceDependencyMappingRequest
  ): Promise<ListResourceDependencyMappingResponse> {
    const data = ListResourceDependencyMappingRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.accesscontrol_x.v1beta1.Query",
      "ListResourceDependencyMapping",
      data
    );
    return promise.then((data) =>
      ListResourceDependencyMappingResponse.decode(new Reader(data))
    );
  }

  WasmDependencyMapping(
    request: WasmDependencyMappingRequest
  ): Promise<WasmDependencyMappingResponse> {
    const data = WasmDependencyMappingRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.accesscontrol_x.v1beta1.Query",
      "WasmDependencyMapping",
      data
    );
    return promise.then((data) =>
      WasmDependencyMappingResponse.decode(new Reader(data))
    );
  }

  ListWasmDependencyMapping(
    request: ListWasmDependencyMappingRequest
  ): Promise<ListWasmDependencyMappingResponse> {
    const data = ListWasmDependencyMappingRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.accesscontrol_x.v1beta1.Query",
      "ListWasmDependencyMapping",
      data
    );
    return promise.then((data) =>
      ListWasmDependencyMappingResponse.decode(new Reader(data))
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
