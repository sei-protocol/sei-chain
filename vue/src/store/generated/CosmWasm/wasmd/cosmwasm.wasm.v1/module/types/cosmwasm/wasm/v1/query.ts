/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import {
  ContractInfo,
  ContractCodeHistoryEntry,
  Model,
  AccessConfig,
} from "../../../cosmwasm/wasm/v1/types";
import {
  PageRequest,
  PageResponse,
} from "../../../cosmos/base/query/v1beta1/pagination";

export const protobufPackage = "cosmwasm.wasm.v1";

/**
 * QueryContractInfoRequest is the request type for the Query/ContractInfo RPC
 * method
 */
export interface QueryContractInfoRequest {
  /** address is the address of the contract to query */
  address: string;
}

/**
 * QueryContractInfoResponse is the response type for the Query/ContractInfo RPC
 * method
 */
export interface QueryContractInfoResponse {
  /** address is the address of the contract */
  address: string;
  contractInfo: ContractInfo | undefined;
}

/**
 * QueryContractHistoryRequest is the request type for the Query/ContractHistory
 * RPC method
 */
export interface QueryContractHistoryRequest {
  /** address is the address of the contract to query */
  address: string;
  /** pagination defines an optional pagination for the request. */
  pagination: PageRequest | undefined;
}

/**
 * QueryContractHistoryResponse is the response type for the
 * Query/ContractHistory RPC method
 */
export interface QueryContractHistoryResponse {
  entries: ContractCodeHistoryEntry[];
  /** pagination defines the pagination in the response. */
  pagination: PageResponse | undefined;
}

/**
 * QueryContractsByCodeRequest is the request type for the Query/ContractsByCode
 * RPC method
 */
export interface QueryContractsByCodeRequest {
  /** grpc-gateway_out does not support Go style CodID */
  codeId: number;
  /** pagination defines an optional pagination for the request. */
  pagination: PageRequest | undefined;
}

/**
 * QueryContractsByCodeResponse is the response type for the
 * Query/ContractsByCode RPC method
 */
export interface QueryContractsByCodeResponse {
  /** contracts are a set of contract addresses */
  contracts: string[];
  /** pagination defines the pagination in the response. */
  pagination: PageResponse | undefined;
}

/**
 * QueryAllContractStateRequest is the request type for the
 * Query/AllContractState RPC method
 */
export interface QueryAllContractStateRequest {
  /** address is the address of the contract */
  address: string;
  /** pagination defines an optional pagination for the request. */
  pagination: PageRequest | undefined;
}

/**
 * QueryAllContractStateResponse is the response type for the
 * Query/AllContractState RPC method
 */
export interface QueryAllContractStateResponse {
  models: Model[];
  /** pagination defines the pagination in the response. */
  pagination: PageResponse | undefined;
}

/**
 * QueryRawContractStateRequest is the request type for the
 * Query/RawContractState RPC method
 */
export interface QueryRawContractStateRequest {
  /** address is the address of the contract */
  address: string;
  queryData: Uint8Array;
}

/**
 * QueryRawContractStateResponse is the response type for the
 * Query/RawContractState RPC method
 */
export interface QueryRawContractStateResponse {
  /** Data contains the raw store data */
  data: Uint8Array;
}

/**
 * QuerySmartContractStateRequest is the request type for the
 * Query/SmartContractState RPC method
 */
export interface QuerySmartContractStateRequest {
  /** address is the address of the contract */
  address: string;
  /** QueryData contains the query data passed to the contract */
  queryData: Uint8Array;
}

/**
 * QuerySmartContractStateResponse is the response type for the
 * Query/SmartContractState RPC method
 */
export interface QuerySmartContractStateResponse {
  /** Data contains the json data returned from the smart contract */
  data: Uint8Array;
}

/** QueryCodeRequest is the request type for the Query/Code RPC method */
export interface QueryCodeRequest {
  /** grpc-gateway_out does not support Go style CodID */
  codeId: number;
}

/** CodeInfoResponse contains code meta data from CodeInfo */
export interface CodeInfoResponse {
  /** id for legacy support */
  codeId: number;
  creator: string;
  dataHash: Uint8Array;
  instantiatePermission: AccessConfig | undefined;
}

/** QueryCodeResponse is the response type for the Query/Code RPC method */
export interface QueryCodeResponse {
  codeInfo: CodeInfoResponse | undefined;
  data: Uint8Array;
}

/** QueryCodesRequest is the request type for the Query/Codes RPC method */
export interface QueryCodesRequest {
  /** pagination defines an optional pagination for the request. */
  pagination: PageRequest | undefined;
}

/** QueryCodesResponse is the response type for the Query/Codes RPC method */
export interface QueryCodesResponse {
  codeInfos: CodeInfoResponse[];
  /** pagination defines the pagination in the response. */
  pagination: PageResponse | undefined;
}

/**
 * QueryPinnedCodesRequest is the request type for the Query/PinnedCodes
 * RPC method
 */
export interface QueryPinnedCodesRequest {
  /** pagination defines an optional pagination for the request. */
  pagination: PageRequest | undefined;
}

/**
 * QueryPinnedCodesResponse is the response type for the
 * Query/PinnedCodes RPC method
 */
export interface QueryPinnedCodesResponse {
  codeIds: number[];
  /** pagination defines the pagination in the response. */
  pagination: PageResponse | undefined;
}

const baseQueryContractInfoRequest: object = { address: "" };

export const QueryContractInfoRequest = {
  encode(
    message: QueryContractInfoRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryContractInfoRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryContractInfoRequest,
    } as QueryContractInfoRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryContractInfoRequest {
    const message = {
      ...baseQueryContractInfoRequest,
    } as QueryContractInfoRequest;
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    return message;
  },

  toJSON(message: QueryContractInfoRequest): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryContractInfoRequest>
  ): QueryContractInfoRequest {
    const message = {
      ...baseQueryContractInfoRequest,
    } as QueryContractInfoRequest;
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    return message;
  },
};

const baseQueryContractInfoResponse: object = { address: "" };

export const QueryContractInfoResponse = {
  encode(
    message: QueryContractInfoResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    if (message.contractInfo !== undefined) {
      ContractInfo.encode(
        message.contractInfo,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryContractInfoResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryContractInfoResponse,
    } as QueryContractInfoResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
          break;
        case 2:
          message.contractInfo = ContractInfo.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryContractInfoResponse {
    const message = {
      ...baseQueryContractInfoResponse,
    } as QueryContractInfoResponse;
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (object.contractInfo !== undefined && object.contractInfo !== null) {
      message.contractInfo = ContractInfo.fromJSON(object.contractInfo);
    } else {
      message.contractInfo = undefined;
    }
    return message;
  },

  toJSON(message: QueryContractInfoResponse): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    message.contractInfo !== undefined &&
      (obj.contractInfo = message.contractInfo
        ? ContractInfo.toJSON(message.contractInfo)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryContractInfoResponse>
  ): QueryContractInfoResponse {
    const message = {
      ...baseQueryContractInfoResponse,
    } as QueryContractInfoResponse;
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.contractInfo !== undefined && object.contractInfo !== null) {
      message.contractInfo = ContractInfo.fromPartial(object.contractInfo);
    } else {
      message.contractInfo = undefined;
    }
    return message;
  },
};

const baseQueryContractHistoryRequest: object = { address: "" };

export const QueryContractHistoryRequest = {
  encode(
    message: QueryContractHistoryRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryContractHistoryRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryContractHistoryRequest,
    } as QueryContractHistoryRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
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

  fromJSON(object: any): QueryContractHistoryRequest {
    const message = {
      ...baseQueryContractHistoryRequest,
    } as QueryContractHistoryRequest;
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryContractHistoryRequest): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryContractHistoryRequest>
  ): QueryContractHistoryRequest {
    const message = {
      ...baseQueryContractHistoryRequest,
    } as QueryContractHistoryRequest;
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryContractHistoryResponse: object = {};

export const QueryContractHistoryResponse = {
  encode(
    message: QueryContractHistoryResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.entries) {
      ContractCodeHistoryEntry.encode(v!, writer.uint32(10).fork()).ldelim();
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
  ): QueryContractHistoryResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryContractHistoryResponse,
    } as QueryContractHistoryResponse;
    message.entries = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.entries.push(
            ContractCodeHistoryEntry.decode(reader, reader.uint32())
          );
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

  fromJSON(object: any): QueryContractHistoryResponse {
    const message = {
      ...baseQueryContractHistoryResponse,
    } as QueryContractHistoryResponse;
    message.entries = [];
    if (object.entries !== undefined && object.entries !== null) {
      for (const e of object.entries) {
        message.entries.push(ContractCodeHistoryEntry.fromJSON(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryContractHistoryResponse): unknown {
    const obj: any = {};
    if (message.entries) {
      obj.entries = message.entries.map((e) =>
        e ? ContractCodeHistoryEntry.toJSON(e) : undefined
      );
    } else {
      obj.entries = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryContractHistoryResponse>
  ): QueryContractHistoryResponse {
    const message = {
      ...baseQueryContractHistoryResponse,
    } as QueryContractHistoryResponse;
    message.entries = [];
    if (object.entries !== undefined && object.entries !== null) {
      for (const e of object.entries) {
        message.entries.push(ContractCodeHistoryEntry.fromPartial(e));
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

const baseQueryContractsByCodeRequest: object = { codeId: 0 };

export const QueryContractsByCodeRequest = {
  encode(
    message: QueryContractsByCodeRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.codeId !== 0) {
      writer.uint32(8).uint64(message.codeId);
    }
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryContractsByCodeRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryContractsByCodeRequest,
    } as QueryContractsByCodeRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.codeId = longToNumber(reader.uint64() as Long);
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

  fromJSON(object: any): QueryContractsByCodeRequest {
    const message = {
      ...baseQueryContractsByCodeRequest,
    } as QueryContractsByCodeRequest;
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = Number(object.codeId);
    } else {
      message.codeId = 0;
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryContractsByCodeRequest): unknown {
    const obj: any = {};
    message.codeId !== undefined && (obj.codeId = message.codeId);
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryContractsByCodeRequest>
  ): QueryContractsByCodeRequest {
    const message = {
      ...baseQueryContractsByCodeRequest,
    } as QueryContractsByCodeRequest;
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = object.codeId;
    } else {
      message.codeId = 0;
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryContractsByCodeResponse: object = { contracts: "" };

export const QueryContractsByCodeResponse = {
  encode(
    message: QueryContractsByCodeResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.contracts) {
      writer.uint32(10).string(v!);
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
  ): QueryContractsByCodeResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryContractsByCodeResponse,
    } as QueryContractsByCodeResponse;
    message.contracts = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.contracts.push(reader.string());
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

  fromJSON(object: any): QueryContractsByCodeResponse {
    const message = {
      ...baseQueryContractsByCodeResponse,
    } as QueryContractsByCodeResponse;
    message.contracts = [];
    if (object.contracts !== undefined && object.contracts !== null) {
      for (const e of object.contracts) {
        message.contracts.push(String(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryContractsByCodeResponse): unknown {
    const obj: any = {};
    if (message.contracts) {
      obj.contracts = message.contracts.map((e) => e);
    } else {
      obj.contracts = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryContractsByCodeResponse>
  ): QueryContractsByCodeResponse {
    const message = {
      ...baseQueryContractsByCodeResponse,
    } as QueryContractsByCodeResponse;
    message.contracts = [];
    if (object.contracts !== undefined && object.contracts !== null) {
      for (const e of object.contracts) {
        message.contracts.push(e);
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

const baseQueryAllContractStateRequest: object = { address: "" };

export const QueryAllContractStateRequest = {
  encode(
    message: QueryAllContractStateRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryAllContractStateRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryAllContractStateRequest,
    } as QueryAllContractStateRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
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

  fromJSON(object: any): QueryAllContractStateRequest {
    const message = {
      ...baseQueryAllContractStateRequest,
    } as QueryAllContractStateRequest;
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryAllContractStateRequest): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryAllContractStateRequest>
  ): QueryAllContractStateRequest {
    const message = {
      ...baseQueryAllContractStateRequest,
    } as QueryAllContractStateRequest;
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryAllContractStateResponse: object = {};

export const QueryAllContractStateResponse = {
  encode(
    message: QueryAllContractStateResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.models) {
      Model.encode(v!, writer.uint32(10).fork()).ldelim();
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
  ): QueryAllContractStateResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryAllContractStateResponse,
    } as QueryAllContractStateResponse;
    message.models = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.models.push(Model.decode(reader, reader.uint32()));
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

  fromJSON(object: any): QueryAllContractStateResponse {
    const message = {
      ...baseQueryAllContractStateResponse,
    } as QueryAllContractStateResponse;
    message.models = [];
    if (object.models !== undefined && object.models !== null) {
      for (const e of object.models) {
        message.models.push(Model.fromJSON(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryAllContractStateResponse): unknown {
    const obj: any = {};
    if (message.models) {
      obj.models = message.models.map((e) => (e ? Model.toJSON(e) : undefined));
    } else {
      obj.models = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryAllContractStateResponse>
  ): QueryAllContractStateResponse {
    const message = {
      ...baseQueryAllContractStateResponse,
    } as QueryAllContractStateResponse;
    message.models = [];
    if (object.models !== undefined && object.models !== null) {
      for (const e of object.models) {
        message.models.push(Model.fromPartial(e));
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

const baseQueryRawContractStateRequest: object = { address: "" };

export const QueryRawContractStateRequest = {
  encode(
    message: QueryRawContractStateRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    if (message.queryData.length !== 0) {
      writer.uint32(18).bytes(message.queryData);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryRawContractStateRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryRawContractStateRequest,
    } as QueryRawContractStateRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
          break;
        case 2:
          message.queryData = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryRawContractStateRequest {
    const message = {
      ...baseQueryRawContractStateRequest,
    } as QueryRawContractStateRequest;
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (object.queryData !== undefined && object.queryData !== null) {
      message.queryData = bytesFromBase64(object.queryData);
    }
    return message;
  },

  toJSON(message: QueryRawContractStateRequest): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    message.queryData !== undefined &&
      (obj.queryData = base64FromBytes(
        message.queryData !== undefined ? message.queryData : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryRawContractStateRequest>
  ): QueryRawContractStateRequest {
    const message = {
      ...baseQueryRawContractStateRequest,
    } as QueryRawContractStateRequest;
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.queryData !== undefined && object.queryData !== null) {
      message.queryData = object.queryData;
    } else {
      message.queryData = new Uint8Array();
    }
    return message;
  },
};

const baseQueryRawContractStateResponse: object = {};

export const QueryRawContractStateResponse = {
  encode(
    message: QueryRawContractStateResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.data.length !== 0) {
      writer.uint32(10).bytes(message.data);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryRawContractStateResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryRawContractStateResponse,
    } as QueryRawContractStateResponse;
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

  fromJSON(object: any): QueryRawContractStateResponse {
    const message = {
      ...baseQueryRawContractStateResponse,
    } as QueryRawContractStateResponse;
    if (object.data !== undefined && object.data !== null) {
      message.data = bytesFromBase64(object.data);
    }
    return message;
  },

  toJSON(message: QueryRawContractStateResponse): unknown {
    const obj: any = {};
    message.data !== undefined &&
      (obj.data = base64FromBytes(
        message.data !== undefined ? message.data : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryRawContractStateResponse>
  ): QueryRawContractStateResponse {
    const message = {
      ...baseQueryRawContractStateResponse,
    } as QueryRawContractStateResponse;
    if (object.data !== undefined && object.data !== null) {
      message.data = object.data;
    } else {
      message.data = new Uint8Array();
    }
    return message;
  },
};

const baseQuerySmartContractStateRequest: object = { address: "" };

export const QuerySmartContractStateRequest = {
  encode(
    message: QuerySmartContractStateRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    if (message.queryData.length !== 0) {
      writer.uint32(18).bytes(message.queryData);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QuerySmartContractStateRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQuerySmartContractStateRequest,
    } as QuerySmartContractStateRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
          break;
        case 2:
          message.queryData = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QuerySmartContractStateRequest {
    const message = {
      ...baseQuerySmartContractStateRequest,
    } as QuerySmartContractStateRequest;
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (object.queryData !== undefined && object.queryData !== null) {
      message.queryData = bytesFromBase64(object.queryData);
    }
    return message;
  },

  toJSON(message: QuerySmartContractStateRequest): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    message.queryData !== undefined &&
      (obj.queryData = base64FromBytes(
        message.queryData !== undefined ? message.queryData : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(
    object: DeepPartial<QuerySmartContractStateRequest>
  ): QuerySmartContractStateRequest {
    const message = {
      ...baseQuerySmartContractStateRequest,
    } as QuerySmartContractStateRequest;
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.queryData !== undefined && object.queryData !== null) {
      message.queryData = object.queryData;
    } else {
      message.queryData = new Uint8Array();
    }
    return message;
  },
};

const baseQuerySmartContractStateResponse: object = {};

export const QuerySmartContractStateResponse = {
  encode(
    message: QuerySmartContractStateResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.data.length !== 0) {
      writer.uint32(10).bytes(message.data);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QuerySmartContractStateResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQuerySmartContractStateResponse,
    } as QuerySmartContractStateResponse;
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

  fromJSON(object: any): QuerySmartContractStateResponse {
    const message = {
      ...baseQuerySmartContractStateResponse,
    } as QuerySmartContractStateResponse;
    if (object.data !== undefined && object.data !== null) {
      message.data = bytesFromBase64(object.data);
    }
    return message;
  },

  toJSON(message: QuerySmartContractStateResponse): unknown {
    const obj: any = {};
    message.data !== undefined &&
      (obj.data = base64FromBytes(
        message.data !== undefined ? message.data : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(
    object: DeepPartial<QuerySmartContractStateResponse>
  ): QuerySmartContractStateResponse {
    const message = {
      ...baseQuerySmartContractStateResponse,
    } as QuerySmartContractStateResponse;
    if (object.data !== undefined && object.data !== null) {
      message.data = object.data;
    } else {
      message.data = new Uint8Array();
    }
    return message;
  },
};

const baseQueryCodeRequest: object = { codeId: 0 };

export const QueryCodeRequest = {
  encode(message: QueryCodeRequest, writer: Writer = Writer.create()): Writer {
    if (message.codeId !== 0) {
      writer.uint32(8).uint64(message.codeId);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryCodeRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryCodeRequest } as QueryCodeRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.codeId = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryCodeRequest {
    const message = { ...baseQueryCodeRequest } as QueryCodeRequest;
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = Number(object.codeId);
    } else {
      message.codeId = 0;
    }
    return message;
  },

  toJSON(message: QueryCodeRequest): unknown {
    const obj: any = {};
    message.codeId !== undefined && (obj.codeId = message.codeId);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryCodeRequest>): QueryCodeRequest {
    const message = { ...baseQueryCodeRequest } as QueryCodeRequest;
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = object.codeId;
    } else {
      message.codeId = 0;
    }
    return message;
  },
};

const baseCodeInfoResponse: object = { codeId: 0, creator: "" };

export const CodeInfoResponse = {
  encode(message: CodeInfoResponse, writer: Writer = Writer.create()): Writer {
    if (message.codeId !== 0) {
      writer.uint32(8).uint64(message.codeId);
    }
    if (message.creator !== "") {
      writer.uint32(18).string(message.creator);
    }
    if (message.dataHash.length !== 0) {
      writer.uint32(26).bytes(message.dataHash);
    }
    if (message.instantiatePermission !== undefined) {
      AccessConfig.encode(
        message.instantiatePermission,
        writer.uint32(50).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): CodeInfoResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseCodeInfoResponse } as CodeInfoResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.codeId = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.creator = reader.string();
          break;
        case 3:
          message.dataHash = reader.bytes();
          break;
        case 6:
          message.instantiatePermission = AccessConfig.decode(
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

  fromJSON(object: any): CodeInfoResponse {
    const message = { ...baseCodeInfoResponse } as CodeInfoResponse;
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = Number(object.codeId);
    } else {
      message.codeId = 0;
    }
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = String(object.creator);
    } else {
      message.creator = "";
    }
    if (object.dataHash !== undefined && object.dataHash !== null) {
      message.dataHash = bytesFromBase64(object.dataHash);
    }
    if (
      object.instantiatePermission !== undefined &&
      object.instantiatePermission !== null
    ) {
      message.instantiatePermission = AccessConfig.fromJSON(
        object.instantiatePermission
      );
    } else {
      message.instantiatePermission = undefined;
    }
    return message;
  },

  toJSON(message: CodeInfoResponse): unknown {
    const obj: any = {};
    message.codeId !== undefined && (obj.codeId = message.codeId);
    message.creator !== undefined && (obj.creator = message.creator);
    message.dataHash !== undefined &&
      (obj.dataHash = base64FromBytes(
        message.dataHash !== undefined ? message.dataHash : new Uint8Array()
      ));
    message.instantiatePermission !== undefined &&
      (obj.instantiatePermission = message.instantiatePermission
        ? AccessConfig.toJSON(message.instantiatePermission)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<CodeInfoResponse>): CodeInfoResponse {
    const message = { ...baseCodeInfoResponse } as CodeInfoResponse;
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = object.codeId;
    } else {
      message.codeId = 0;
    }
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = object.creator;
    } else {
      message.creator = "";
    }
    if (object.dataHash !== undefined && object.dataHash !== null) {
      message.dataHash = object.dataHash;
    } else {
      message.dataHash = new Uint8Array();
    }
    if (
      object.instantiatePermission !== undefined &&
      object.instantiatePermission !== null
    ) {
      message.instantiatePermission = AccessConfig.fromPartial(
        object.instantiatePermission
      );
    } else {
      message.instantiatePermission = undefined;
    }
    return message;
  },
};

const baseQueryCodeResponse: object = {};

export const QueryCodeResponse = {
  encode(message: QueryCodeResponse, writer: Writer = Writer.create()): Writer {
    if (message.codeInfo !== undefined) {
      CodeInfoResponse.encode(
        message.codeInfo,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.data.length !== 0) {
      writer.uint32(18).bytes(message.data);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryCodeResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryCodeResponse } as QueryCodeResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.codeInfo = CodeInfoResponse.decode(reader, reader.uint32());
          break;
        case 2:
          message.data = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryCodeResponse {
    const message = { ...baseQueryCodeResponse } as QueryCodeResponse;
    if (object.codeInfo !== undefined && object.codeInfo !== null) {
      message.codeInfo = CodeInfoResponse.fromJSON(object.codeInfo);
    } else {
      message.codeInfo = undefined;
    }
    if (object.data !== undefined && object.data !== null) {
      message.data = bytesFromBase64(object.data);
    }
    return message;
  },

  toJSON(message: QueryCodeResponse): unknown {
    const obj: any = {};
    message.codeInfo !== undefined &&
      (obj.codeInfo = message.codeInfo
        ? CodeInfoResponse.toJSON(message.codeInfo)
        : undefined);
    message.data !== undefined &&
      (obj.data = base64FromBytes(
        message.data !== undefined ? message.data : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<QueryCodeResponse>): QueryCodeResponse {
    const message = { ...baseQueryCodeResponse } as QueryCodeResponse;
    if (object.codeInfo !== undefined && object.codeInfo !== null) {
      message.codeInfo = CodeInfoResponse.fromPartial(object.codeInfo);
    } else {
      message.codeInfo = undefined;
    }
    if (object.data !== undefined && object.data !== null) {
      message.data = object.data;
    } else {
      message.data = new Uint8Array();
    }
    return message;
  },
};

const baseQueryCodesRequest: object = {};

export const QueryCodesRequest = {
  encode(message: QueryCodesRequest, writer: Writer = Writer.create()): Writer {
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryCodesRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryCodesRequest } as QueryCodesRequest;
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

  fromJSON(object: any): QueryCodesRequest {
    const message = { ...baseQueryCodesRequest } as QueryCodesRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryCodesRequest): unknown {
    const obj: any = {};
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryCodesRequest>): QueryCodesRequest {
    const message = { ...baseQueryCodesRequest } as QueryCodesRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryCodesResponse: object = {};

export const QueryCodesResponse = {
  encode(
    message: QueryCodesResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.codeInfos) {
      CodeInfoResponse.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.pagination !== undefined) {
      PageResponse.encode(
        message.pagination,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryCodesResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryCodesResponse } as QueryCodesResponse;
    message.codeInfos = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.codeInfos.push(
            CodeInfoResponse.decode(reader, reader.uint32())
          );
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

  fromJSON(object: any): QueryCodesResponse {
    const message = { ...baseQueryCodesResponse } as QueryCodesResponse;
    message.codeInfos = [];
    if (object.codeInfos !== undefined && object.codeInfos !== null) {
      for (const e of object.codeInfos) {
        message.codeInfos.push(CodeInfoResponse.fromJSON(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryCodesResponse): unknown {
    const obj: any = {};
    if (message.codeInfos) {
      obj.codeInfos = message.codeInfos.map((e) =>
        e ? CodeInfoResponse.toJSON(e) : undefined
      );
    } else {
      obj.codeInfos = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryCodesResponse>): QueryCodesResponse {
    const message = { ...baseQueryCodesResponse } as QueryCodesResponse;
    message.codeInfos = [];
    if (object.codeInfos !== undefined && object.codeInfos !== null) {
      for (const e of object.codeInfos) {
        message.codeInfos.push(CodeInfoResponse.fromPartial(e));
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

const baseQueryPinnedCodesRequest: object = {};

export const QueryPinnedCodesRequest = {
  encode(
    message: QueryPinnedCodesRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryPinnedCodesRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryPinnedCodesRequest,
    } as QueryPinnedCodesRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
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

  fromJSON(object: any): QueryPinnedCodesRequest {
    const message = {
      ...baseQueryPinnedCodesRequest,
    } as QueryPinnedCodesRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryPinnedCodesRequest): unknown {
    const obj: any = {};
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryPinnedCodesRequest>
  ): QueryPinnedCodesRequest {
    const message = {
      ...baseQueryPinnedCodesRequest,
    } as QueryPinnedCodesRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryPinnedCodesResponse: object = { codeIds: 0 };

export const QueryPinnedCodesResponse = {
  encode(
    message: QueryPinnedCodesResponse,
    writer: Writer = Writer.create()
  ): Writer {
    writer.uint32(10).fork();
    for (const v of message.codeIds) {
      writer.uint64(v);
    }
    writer.ldelim();
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
  ): QueryPinnedCodesResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryPinnedCodesResponse,
    } as QueryPinnedCodesResponse;
    message.codeIds = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          if ((tag & 7) === 2) {
            const end2 = reader.uint32() + reader.pos;
            while (reader.pos < end2) {
              message.codeIds.push(longToNumber(reader.uint64() as Long));
            }
          } else {
            message.codeIds.push(longToNumber(reader.uint64() as Long));
          }
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

  fromJSON(object: any): QueryPinnedCodesResponse {
    const message = {
      ...baseQueryPinnedCodesResponse,
    } as QueryPinnedCodesResponse;
    message.codeIds = [];
    if (object.codeIds !== undefined && object.codeIds !== null) {
      for (const e of object.codeIds) {
        message.codeIds.push(Number(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryPinnedCodesResponse): unknown {
    const obj: any = {};
    if (message.codeIds) {
      obj.codeIds = message.codeIds.map((e) => e);
    } else {
      obj.codeIds = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryPinnedCodesResponse>
  ): QueryPinnedCodesResponse {
    const message = {
      ...baseQueryPinnedCodesResponse,
    } as QueryPinnedCodesResponse;
    message.codeIds = [];
    if (object.codeIds !== undefined && object.codeIds !== null) {
      for (const e of object.codeIds) {
        message.codeIds.push(e);
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

/** Query provides defines the gRPC querier service */
export interface Query {
  /** ContractInfo gets the contract meta data */
  ContractInfo(
    request: QueryContractInfoRequest
  ): Promise<QueryContractInfoResponse>;
  /** ContractHistory gets the contract code history */
  ContractHistory(
    request: QueryContractHistoryRequest
  ): Promise<QueryContractHistoryResponse>;
  /** ContractsByCode lists all smart contracts for a code id */
  ContractsByCode(
    request: QueryContractsByCodeRequest
  ): Promise<QueryContractsByCodeResponse>;
  /** AllContractState gets all raw store data for a single contract */
  AllContractState(
    request: QueryAllContractStateRequest
  ): Promise<QueryAllContractStateResponse>;
  /** RawContractState gets single key from the raw store data of a contract */
  RawContractState(
    request: QueryRawContractStateRequest
  ): Promise<QueryRawContractStateResponse>;
  /** SmartContractState get smart query result from the contract */
  SmartContractState(
    request: QuerySmartContractStateRequest
  ): Promise<QuerySmartContractStateResponse>;
  /** Code gets the binary code and metadata for a singe wasm code */
  Code(request: QueryCodeRequest): Promise<QueryCodeResponse>;
  /** Codes gets the metadata for all stored wasm codes */
  Codes(request: QueryCodesRequest): Promise<QueryCodesResponse>;
  /** PinnedCodes gets the pinned code ids */
  PinnedCodes(
    request: QueryPinnedCodesRequest
  ): Promise<QueryPinnedCodesResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  ContractInfo(
    request: QueryContractInfoRequest
  ): Promise<QueryContractInfoResponse> {
    const data = QueryContractInfoRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmwasm.wasm.v1.Query",
      "ContractInfo",
      data
    );
    return promise.then((data) =>
      QueryContractInfoResponse.decode(new Reader(data))
    );
  }

  ContractHistory(
    request: QueryContractHistoryRequest
  ): Promise<QueryContractHistoryResponse> {
    const data = QueryContractHistoryRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmwasm.wasm.v1.Query",
      "ContractHistory",
      data
    );
    return promise.then((data) =>
      QueryContractHistoryResponse.decode(new Reader(data))
    );
  }

  ContractsByCode(
    request: QueryContractsByCodeRequest
  ): Promise<QueryContractsByCodeResponse> {
    const data = QueryContractsByCodeRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmwasm.wasm.v1.Query",
      "ContractsByCode",
      data
    );
    return promise.then((data) =>
      QueryContractsByCodeResponse.decode(new Reader(data))
    );
  }

  AllContractState(
    request: QueryAllContractStateRequest
  ): Promise<QueryAllContractStateResponse> {
    const data = QueryAllContractStateRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmwasm.wasm.v1.Query",
      "AllContractState",
      data
    );
    return promise.then((data) =>
      QueryAllContractStateResponse.decode(new Reader(data))
    );
  }

  RawContractState(
    request: QueryRawContractStateRequest
  ): Promise<QueryRawContractStateResponse> {
    const data = QueryRawContractStateRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmwasm.wasm.v1.Query",
      "RawContractState",
      data
    );
    return promise.then((data) =>
      QueryRawContractStateResponse.decode(new Reader(data))
    );
  }

  SmartContractState(
    request: QuerySmartContractStateRequest
  ): Promise<QuerySmartContractStateResponse> {
    const data = QuerySmartContractStateRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmwasm.wasm.v1.Query",
      "SmartContractState",
      data
    );
    return promise.then((data) =>
      QuerySmartContractStateResponse.decode(new Reader(data))
    );
  }

  Code(request: QueryCodeRequest): Promise<QueryCodeResponse> {
    const data = QueryCodeRequest.encode(request).finish();
    const promise = this.rpc.request("cosmwasm.wasm.v1.Query", "Code", data);
    return promise.then((data) => QueryCodeResponse.decode(new Reader(data)));
  }

  Codes(request: QueryCodesRequest): Promise<QueryCodesResponse> {
    const data = QueryCodesRequest.encode(request).finish();
    const promise = this.rpc.request("cosmwasm.wasm.v1.Query", "Codes", data);
    return promise.then((data) => QueryCodesResponse.decode(new Reader(data)));
  }

  PinnedCodes(
    request: QueryPinnedCodesRequest
  ): Promise<QueryPinnedCodesResponse> {
    const data = QueryPinnedCodesRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmwasm.wasm.v1.Query",
      "PinnedCodes",
      data
    );
    return promise.then((data) =>
      QueryPinnedCodesResponse.decode(new Reader(data))
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
