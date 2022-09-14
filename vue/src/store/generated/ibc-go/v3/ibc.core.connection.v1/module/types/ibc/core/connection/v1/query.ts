/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import {
  ConnectionEnd,
  IdentifiedConnection,
} from "../../../../ibc/core/connection/v1/connection";
import {
  Height,
  IdentifiedClientState,
} from "../../../../ibc/core/client/v1/client";
import {
  PageRequest,
  PageResponse,
} from "../../../../cosmos/base/query/v1beta1/pagination";
import { Any } from "../../../../google/protobuf/any";

export const protobufPackage = "ibc.core.connection.v1";

/**
 * QueryConnectionRequest is the request type for the Query/Connection RPC
 * method
 */
export interface QueryConnectionRequest {
  /** connection unique identifier */
  connection_id: string;
}

/**
 * QueryConnectionResponse is the response type for the Query/Connection RPC
 * method. Besides the connection end, it includes a proof and the height from
 * which the proof was retrieved.
 */
export interface QueryConnectionResponse {
  /** connection associated with the request identifier */
  connection: ConnectionEnd | undefined;
  /** merkle proof of existence */
  proof: Uint8Array;
  /** height at which the proof was retrieved */
  proof_height: Height | undefined;
}

/**
 * QueryConnectionsRequest is the request type for the Query/Connections RPC
 * method
 */
export interface QueryConnectionsRequest {
  pagination: PageRequest | undefined;
}

/**
 * QueryConnectionsResponse is the response type for the Query/Connections RPC
 * method.
 */
export interface QueryConnectionsResponse {
  /** list of stored connections of the chain. */
  connections: IdentifiedConnection[];
  /** pagination response */
  pagination: PageResponse | undefined;
  /** query block height */
  height: Height | undefined;
}

/**
 * QueryClientConnectionsRequest is the request type for the
 * Query/ClientConnections RPC method
 */
export interface QueryClientConnectionsRequest {
  /** client identifier associated with a connection */
  client_id: string;
}

/**
 * QueryClientConnectionsResponse is the response type for the
 * Query/ClientConnections RPC method
 */
export interface QueryClientConnectionsResponse {
  /** slice of all the connection paths associated with a client. */
  connection_paths: string[];
  /** merkle proof of existence */
  proof: Uint8Array;
  /** height at which the proof was generated */
  proof_height: Height | undefined;
}

/**
 * QueryConnectionClientStateRequest is the request type for the
 * Query/ConnectionClientState RPC method
 */
export interface QueryConnectionClientStateRequest {
  /** connection identifier */
  connection_id: string;
}

/**
 * QueryConnectionClientStateResponse is the response type for the
 * Query/ConnectionClientState RPC method
 */
export interface QueryConnectionClientStateResponse {
  /** client state associated with the channel */
  identified_client_state: IdentifiedClientState | undefined;
  /** merkle proof of existence */
  proof: Uint8Array;
  /** height at which the proof was retrieved */
  proof_height: Height | undefined;
}

/**
 * QueryConnectionConsensusStateRequest is the request type for the
 * Query/ConnectionConsensusState RPC method
 */
export interface QueryConnectionConsensusStateRequest {
  /** connection identifier */
  connection_id: string;
  revision_number: number;
  revision_height: number;
}

/**
 * QueryConnectionConsensusStateResponse is the response type for the
 * Query/ConnectionConsensusState RPC method
 */
export interface QueryConnectionConsensusStateResponse {
  /** consensus state associated with the channel */
  consensus_state: Any | undefined;
  /** client ID associated with the consensus state */
  client_id: string;
  /** merkle proof of existence */
  proof: Uint8Array;
  /** height at which the proof was retrieved */
  proof_height: Height | undefined;
}

const baseQueryConnectionRequest: object = { connection_id: "" };

export const QueryConnectionRequest = {
  encode(
    message: QueryConnectionRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.connection_id !== "") {
      writer.uint32(10).string(message.connection_id);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryConnectionRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryConnectionRequest } as QueryConnectionRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.connection_id = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryConnectionRequest {
    const message = { ...baseQueryConnectionRequest } as QueryConnectionRequest;
    if (object.connection_id !== undefined && object.connection_id !== null) {
      message.connection_id = String(object.connection_id);
    } else {
      message.connection_id = "";
    }
    return message;
  },

  toJSON(message: QueryConnectionRequest): unknown {
    const obj: any = {};
    message.connection_id !== undefined &&
      (obj.connection_id = message.connection_id);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryConnectionRequest>
  ): QueryConnectionRequest {
    const message = { ...baseQueryConnectionRequest } as QueryConnectionRequest;
    if (object.connection_id !== undefined && object.connection_id !== null) {
      message.connection_id = object.connection_id;
    } else {
      message.connection_id = "";
    }
    return message;
  },
};

const baseQueryConnectionResponse: object = {};

export const QueryConnectionResponse = {
  encode(
    message: QueryConnectionResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.connection !== undefined) {
      ConnectionEnd.encode(
        message.connection,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.proof.length !== 0) {
      writer.uint32(18).bytes(message.proof);
    }
    if (message.proof_height !== undefined) {
      Height.encode(message.proof_height, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryConnectionResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryConnectionResponse,
    } as QueryConnectionResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.connection = ConnectionEnd.decode(reader, reader.uint32());
          break;
        case 2:
          message.proof = reader.bytes();
          break;
        case 3:
          message.proof_height = Height.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryConnectionResponse {
    const message = {
      ...baseQueryConnectionResponse,
    } as QueryConnectionResponse;
    if (object.connection !== undefined && object.connection !== null) {
      message.connection = ConnectionEnd.fromJSON(object.connection);
    } else {
      message.connection = undefined;
    }
    if (object.proof !== undefined && object.proof !== null) {
      message.proof = bytesFromBase64(object.proof);
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromJSON(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    return message;
  },

  toJSON(message: QueryConnectionResponse): unknown {
    const obj: any = {};
    message.connection !== undefined &&
      (obj.connection = message.connection
        ? ConnectionEnd.toJSON(message.connection)
        : undefined);
    message.proof !== undefined &&
      (obj.proof = base64FromBytes(
        message.proof !== undefined ? message.proof : new Uint8Array()
      ));
    message.proof_height !== undefined &&
      (obj.proof_height = message.proof_height
        ? Height.toJSON(message.proof_height)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryConnectionResponse>
  ): QueryConnectionResponse {
    const message = {
      ...baseQueryConnectionResponse,
    } as QueryConnectionResponse;
    if (object.connection !== undefined && object.connection !== null) {
      message.connection = ConnectionEnd.fromPartial(object.connection);
    } else {
      message.connection = undefined;
    }
    if (object.proof !== undefined && object.proof !== null) {
      message.proof = object.proof;
    } else {
      message.proof = new Uint8Array();
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromPartial(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    return message;
  },
};

const baseQueryConnectionsRequest: object = {};

export const QueryConnectionsRequest = {
  encode(
    message: QueryConnectionsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryConnectionsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryConnectionsRequest,
    } as QueryConnectionsRequest;
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

  fromJSON(object: any): QueryConnectionsRequest {
    const message = {
      ...baseQueryConnectionsRequest,
    } as QueryConnectionsRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryConnectionsRequest): unknown {
    const obj: any = {};
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryConnectionsRequest>
  ): QueryConnectionsRequest {
    const message = {
      ...baseQueryConnectionsRequest,
    } as QueryConnectionsRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryConnectionsResponse: object = {};

export const QueryConnectionsResponse = {
  encode(
    message: QueryConnectionsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.connections) {
      IdentifiedConnection.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.pagination !== undefined) {
      PageResponse.encode(
        message.pagination,
        writer.uint32(18).fork()
      ).ldelim();
    }
    if (message.height !== undefined) {
      Height.encode(message.height, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryConnectionsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryConnectionsResponse,
    } as QueryConnectionsResponse;
    message.connections = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.connections.push(
            IdentifiedConnection.decode(reader, reader.uint32())
          );
          break;
        case 2:
          message.pagination = PageResponse.decode(reader, reader.uint32());
          break;
        case 3:
          message.height = Height.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryConnectionsResponse {
    const message = {
      ...baseQueryConnectionsResponse,
    } as QueryConnectionsResponse;
    message.connections = [];
    if (object.connections !== undefined && object.connections !== null) {
      for (const e of object.connections) {
        message.connections.push(IdentifiedConnection.fromJSON(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = Height.fromJSON(object.height);
    } else {
      message.height = undefined;
    }
    return message;
  },

  toJSON(message: QueryConnectionsResponse): unknown {
    const obj: any = {};
    if (message.connections) {
      obj.connections = message.connections.map((e) =>
        e ? IdentifiedConnection.toJSON(e) : undefined
      );
    } else {
      obj.connections = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    message.height !== undefined &&
      (obj.height = message.height ? Height.toJSON(message.height) : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryConnectionsResponse>
  ): QueryConnectionsResponse {
    const message = {
      ...baseQueryConnectionsResponse,
    } as QueryConnectionsResponse;
    message.connections = [];
    if (object.connections !== undefined && object.connections !== null) {
      for (const e of object.connections) {
        message.connections.push(IdentifiedConnection.fromPartial(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = Height.fromPartial(object.height);
    } else {
      message.height = undefined;
    }
    return message;
  },
};

const baseQueryClientConnectionsRequest: object = { client_id: "" };

export const QueryClientConnectionsRequest = {
  encode(
    message: QueryClientConnectionsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.client_id !== "") {
      writer.uint32(10).string(message.client_id);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryClientConnectionsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryClientConnectionsRequest,
    } as QueryClientConnectionsRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_id = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryClientConnectionsRequest {
    const message = {
      ...baseQueryClientConnectionsRequest,
    } as QueryClientConnectionsRequest;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
    }
    return message;
  },

  toJSON(message: QueryClientConnectionsRequest): unknown {
    const obj: any = {};
    message.client_id !== undefined && (obj.client_id = message.client_id);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryClientConnectionsRequest>
  ): QueryClientConnectionsRequest {
    const message = {
      ...baseQueryClientConnectionsRequest,
    } as QueryClientConnectionsRequest;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
    }
    return message;
  },
};

const baseQueryClientConnectionsResponse: object = { connection_paths: "" };

export const QueryClientConnectionsResponse = {
  encode(
    message: QueryClientConnectionsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.connection_paths) {
      writer.uint32(10).string(v!);
    }
    if (message.proof.length !== 0) {
      writer.uint32(18).bytes(message.proof);
    }
    if (message.proof_height !== undefined) {
      Height.encode(message.proof_height, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryClientConnectionsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryClientConnectionsResponse,
    } as QueryClientConnectionsResponse;
    message.connection_paths = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.connection_paths.push(reader.string());
          break;
        case 2:
          message.proof = reader.bytes();
          break;
        case 3:
          message.proof_height = Height.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryClientConnectionsResponse {
    const message = {
      ...baseQueryClientConnectionsResponse,
    } as QueryClientConnectionsResponse;
    message.connection_paths = [];
    if (
      object.connection_paths !== undefined &&
      object.connection_paths !== null
    ) {
      for (const e of object.connection_paths) {
        message.connection_paths.push(String(e));
      }
    }
    if (object.proof !== undefined && object.proof !== null) {
      message.proof = bytesFromBase64(object.proof);
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromJSON(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    return message;
  },

  toJSON(message: QueryClientConnectionsResponse): unknown {
    const obj: any = {};
    if (message.connection_paths) {
      obj.connection_paths = message.connection_paths.map((e) => e);
    } else {
      obj.connection_paths = [];
    }
    message.proof !== undefined &&
      (obj.proof = base64FromBytes(
        message.proof !== undefined ? message.proof : new Uint8Array()
      ));
    message.proof_height !== undefined &&
      (obj.proof_height = message.proof_height
        ? Height.toJSON(message.proof_height)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryClientConnectionsResponse>
  ): QueryClientConnectionsResponse {
    const message = {
      ...baseQueryClientConnectionsResponse,
    } as QueryClientConnectionsResponse;
    message.connection_paths = [];
    if (
      object.connection_paths !== undefined &&
      object.connection_paths !== null
    ) {
      for (const e of object.connection_paths) {
        message.connection_paths.push(e);
      }
    }
    if (object.proof !== undefined && object.proof !== null) {
      message.proof = object.proof;
    } else {
      message.proof = new Uint8Array();
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromPartial(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    return message;
  },
};

const baseQueryConnectionClientStateRequest: object = { connection_id: "" };

export const QueryConnectionClientStateRequest = {
  encode(
    message: QueryConnectionClientStateRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.connection_id !== "") {
      writer.uint32(10).string(message.connection_id);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryConnectionClientStateRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryConnectionClientStateRequest,
    } as QueryConnectionClientStateRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.connection_id = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryConnectionClientStateRequest {
    const message = {
      ...baseQueryConnectionClientStateRequest,
    } as QueryConnectionClientStateRequest;
    if (object.connection_id !== undefined && object.connection_id !== null) {
      message.connection_id = String(object.connection_id);
    } else {
      message.connection_id = "";
    }
    return message;
  },

  toJSON(message: QueryConnectionClientStateRequest): unknown {
    const obj: any = {};
    message.connection_id !== undefined &&
      (obj.connection_id = message.connection_id);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryConnectionClientStateRequest>
  ): QueryConnectionClientStateRequest {
    const message = {
      ...baseQueryConnectionClientStateRequest,
    } as QueryConnectionClientStateRequest;
    if (object.connection_id !== undefined && object.connection_id !== null) {
      message.connection_id = object.connection_id;
    } else {
      message.connection_id = "";
    }
    return message;
  },
};

const baseQueryConnectionClientStateResponse: object = {};

export const QueryConnectionClientStateResponse = {
  encode(
    message: QueryConnectionClientStateResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.identified_client_state !== undefined) {
      IdentifiedClientState.encode(
        message.identified_client_state,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.proof.length !== 0) {
      writer.uint32(18).bytes(message.proof);
    }
    if (message.proof_height !== undefined) {
      Height.encode(message.proof_height, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryConnectionClientStateResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryConnectionClientStateResponse,
    } as QueryConnectionClientStateResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.identified_client_state = IdentifiedClientState.decode(
            reader,
            reader.uint32()
          );
          break;
        case 2:
          message.proof = reader.bytes();
          break;
        case 3:
          message.proof_height = Height.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryConnectionClientStateResponse {
    const message = {
      ...baseQueryConnectionClientStateResponse,
    } as QueryConnectionClientStateResponse;
    if (
      object.identified_client_state !== undefined &&
      object.identified_client_state !== null
    ) {
      message.identified_client_state = IdentifiedClientState.fromJSON(
        object.identified_client_state
      );
    } else {
      message.identified_client_state = undefined;
    }
    if (object.proof !== undefined && object.proof !== null) {
      message.proof = bytesFromBase64(object.proof);
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromJSON(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    return message;
  },

  toJSON(message: QueryConnectionClientStateResponse): unknown {
    const obj: any = {};
    message.identified_client_state !== undefined &&
      (obj.identified_client_state = message.identified_client_state
        ? IdentifiedClientState.toJSON(message.identified_client_state)
        : undefined);
    message.proof !== undefined &&
      (obj.proof = base64FromBytes(
        message.proof !== undefined ? message.proof : new Uint8Array()
      ));
    message.proof_height !== undefined &&
      (obj.proof_height = message.proof_height
        ? Height.toJSON(message.proof_height)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryConnectionClientStateResponse>
  ): QueryConnectionClientStateResponse {
    const message = {
      ...baseQueryConnectionClientStateResponse,
    } as QueryConnectionClientStateResponse;
    if (
      object.identified_client_state !== undefined &&
      object.identified_client_state !== null
    ) {
      message.identified_client_state = IdentifiedClientState.fromPartial(
        object.identified_client_state
      );
    } else {
      message.identified_client_state = undefined;
    }
    if (object.proof !== undefined && object.proof !== null) {
      message.proof = object.proof;
    } else {
      message.proof = new Uint8Array();
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromPartial(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    return message;
  },
};

const baseQueryConnectionConsensusStateRequest: object = {
  connection_id: "",
  revision_number: 0,
  revision_height: 0,
};

export const QueryConnectionConsensusStateRequest = {
  encode(
    message: QueryConnectionConsensusStateRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.connection_id !== "") {
      writer.uint32(10).string(message.connection_id);
    }
    if (message.revision_number !== 0) {
      writer.uint32(16).uint64(message.revision_number);
    }
    if (message.revision_height !== 0) {
      writer.uint32(24).uint64(message.revision_height);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryConnectionConsensusStateRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryConnectionConsensusStateRequest,
    } as QueryConnectionConsensusStateRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.connection_id = reader.string();
          break;
        case 2:
          message.revision_number = longToNumber(reader.uint64() as Long);
          break;
        case 3:
          message.revision_height = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryConnectionConsensusStateRequest {
    const message = {
      ...baseQueryConnectionConsensusStateRequest,
    } as QueryConnectionConsensusStateRequest;
    if (object.connection_id !== undefined && object.connection_id !== null) {
      message.connection_id = String(object.connection_id);
    } else {
      message.connection_id = "";
    }
    if (
      object.revision_number !== undefined &&
      object.revision_number !== null
    ) {
      message.revision_number = Number(object.revision_number);
    } else {
      message.revision_number = 0;
    }
    if (
      object.revision_height !== undefined &&
      object.revision_height !== null
    ) {
      message.revision_height = Number(object.revision_height);
    } else {
      message.revision_height = 0;
    }
    return message;
  },

  toJSON(message: QueryConnectionConsensusStateRequest): unknown {
    const obj: any = {};
    message.connection_id !== undefined &&
      (obj.connection_id = message.connection_id);
    message.revision_number !== undefined &&
      (obj.revision_number = message.revision_number);
    message.revision_height !== undefined &&
      (obj.revision_height = message.revision_height);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryConnectionConsensusStateRequest>
  ): QueryConnectionConsensusStateRequest {
    const message = {
      ...baseQueryConnectionConsensusStateRequest,
    } as QueryConnectionConsensusStateRequest;
    if (object.connection_id !== undefined && object.connection_id !== null) {
      message.connection_id = object.connection_id;
    } else {
      message.connection_id = "";
    }
    if (
      object.revision_number !== undefined &&
      object.revision_number !== null
    ) {
      message.revision_number = object.revision_number;
    } else {
      message.revision_number = 0;
    }
    if (
      object.revision_height !== undefined &&
      object.revision_height !== null
    ) {
      message.revision_height = object.revision_height;
    } else {
      message.revision_height = 0;
    }
    return message;
  },
};

const baseQueryConnectionConsensusStateResponse: object = { client_id: "" };

export const QueryConnectionConsensusStateResponse = {
  encode(
    message: QueryConnectionConsensusStateResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.consensus_state !== undefined) {
      Any.encode(message.consensus_state, writer.uint32(10).fork()).ldelim();
    }
    if (message.client_id !== "") {
      writer.uint32(18).string(message.client_id);
    }
    if (message.proof.length !== 0) {
      writer.uint32(26).bytes(message.proof);
    }
    if (message.proof_height !== undefined) {
      Height.encode(message.proof_height, writer.uint32(34).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryConnectionConsensusStateResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryConnectionConsensusStateResponse,
    } as QueryConnectionConsensusStateResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.consensus_state = Any.decode(reader, reader.uint32());
          break;
        case 2:
          message.client_id = reader.string();
          break;
        case 3:
          message.proof = reader.bytes();
          break;
        case 4:
          message.proof_height = Height.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryConnectionConsensusStateResponse {
    const message = {
      ...baseQueryConnectionConsensusStateResponse,
    } as QueryConnectionConsensusStateResponse;
    if (
      object.consensus_state !== undefined &&
      object.consensus_state !== null
    ) {
      message.consensus_state = Any.fromJSON(object.consensus_state);
    } else {
      message.consensus_state = undefined;
    }
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
    }
    if (object.proof !== undefined && object.proof !== null) {
      message.proof = bytesFromBase64(object.proof);
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromJSON(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    return message;
  },

  toJSON(message: QueryConnectionConsensusStateResponse): unknown {
    const obj: any = {};
    message.consensus_state !== undefined &&
      (obj.consensus_state = message.consensus_state
        ? Any.toJSON(message.consensus_state)
        : undefined);
    message.client_id !== undefined && (obj.client_id = message.client_id);
    message.proof !== undefined &&
      (obj.proof = base64FromBytes(
        message.proof !== undefined ? message.proof : new Uint8Array()
      ));
    message.proof_height !== undefined &&
      (obj.proof_height = message.proof_height
        ? Height.toJSON(message.proof_height)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryConnectionConsensusStateResponse>
  ): QueryConnectionConsensusStateResponse {
    const message = {
      ...baseQueryConnectionConsensusStateResponse,
    } as QueryConnectionConsensusStateResponse;
    if (
      object.consensus_state !== undefined &&
      object.consensus_state !== null
    ) {
      message.consensus_state = Any.fromPartial(object.consensus_state);
    } else {
      message.consensus_state = undefined;
    }
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
    }
    if (object.proof !== undefined && object.proof !== null) {
      message.proof = object.proof;
    } else {
      message.proof = new Uint8Array();
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromPartial(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    return message;
  },
};

/** Query provides defines the gRPC querier service */
export interface Query {
  /** Connection queries an IBC connection end. */
  Connection(request: QueryConnectionRequest): Promise<QueryConnectionResponse>;
  /** Connections queries all the IBC connections of a chain. */
  Connections(
    request: QueryConnectionsRequest
  ): Promise<QueryConnectionsResponse>;
  /**
   * ClientConnections queries the connection paths associated with a client
   * state.
   */
  ClientConnections(
    request: QueryClientConnectionsRequest
  ): Promise<QueryClientConnectionsResponse>;
  /**
   * ConnectionClientState queries the client state associated with the
   * connection.
   */
  ConnectionClientState(
    request: QueryConnectionClientStateRequest
  ): Promise<QueryConnectionClientStateResponse>;
  /**
   * ConnectionConsensusState queries the consensus state associated with the
   * connection.
   */
  ConnectionConsensusState(
    request: QueryConnectionConsensusStateRequest
  ): Promise<QueryConnectionConsensusStateResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  Connection(
    request: QueryConnectionRequest
  ): Promise<QueryConnectionResponse> {
    const data = QueryConnectionRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.connection.v1.Query",
      "Connection",
      data
    );
    return promise.then((data) =>
      QueryConnectionResponse.decode(new Reader(data))
    );
  }

  Connections(
    request: QueryConnectionsRequest
  ): Promise<QueryConnectionsResponse> {
    const data = QueryConnectionsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.connection.v1.Query",
      "Connections",
      data
    );
    return promise.then((data) =>
      QueryConnectionsResponse.decode(new Reader(data))
    );
  }

  ClientConnections(
    request: QueryClientConnectionsRequest
  ): Promise<QueryClientConnectionsResponse> {
    const data = QueryClientConnectionsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.connection.v1.Query",
      "ClientConnections",
      data
    );
    return promise.then((data) =>
      QueryClientConnectionsResponse.decode(new Reader(data))
    );
  }

  ConnectionClientState(
    request: QueryConnectionClientStateRequest
  ): Promise<QueryConnectionClientStateResponse> {
    const data = QueryConnectionClientStateRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.connection.v1.Query",
      "ConnectionClientState",
      data
    );
    return promise.then((data) =>
      QueryConnectionClientStateResponse.decode(new Reader(data))
    );
  }

  ConnectionConsensusState(
    request: QueryConnectionConsensusStateRequest
  ): Promise<QueryConnectionConsensusStateResponse> {
    const data = QueryConnectionConsensusStateRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.connection.v1.Query",
      "ConnectionConsensusState",
      data
    );
    return promise.then((data) =>
      QueryConnectionConsensusStateResponse.decode(new Reader(data))
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
