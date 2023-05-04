/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import { Any } from "../../../../google/protobuf/any";
import {
  Height,
  IdentifiedClientState,
  ConsensusStateWithHeight,
  Params,
} from "../../../../ibc/core/client/v1/client";
import {
  PageRequest,
  PageResponse,
} from "../../../../cosmos/base/query/v1beta1/pagination";

export const protobufPackage = "ibc.core.client.v1";

/**
 * QueryClientStateRequest is the request type for the Query/ClientState RPC
 * method
 */
export interface QueryClientStateRequest {
  /** client state unique identifier */
  client_id: string;
}

/**
 * QueryClientStateResponse is the response type for the Query/ClientState RPC
 * method. Besides the client state, it includes a proof and the height from
 * which the proof was retrieved.
 */
export interface QueryClientStateResponse {
  /** client state associated with the request identifier */
  client_state: Any | undefined;
  /** merkle proof of existence */
  proof: Uint8Array;
  /** height at which the proof was retrieved */
  proof_height: Height | undefined;
}

/**
 * QueryClientStatesRequest is the request type for the Query/ClientStates RPC
 * method
 */
export interface QueryClientStatesRequest {
  /** pagination request */
  pagination: PageRequest | undefined;
}

/**
 * QueryClientStatesResponse is the response type for the Query/ClientStates RPC
 * method.
 */
export interface QueryClientStatesResponse {
  /** list of stored ClientStates of the chain. */
  client_states: IdentifiedClientState[];
  /** pagination response */
  pagination: PageResponse | undefined;
}

/**
 * QueryConsensusStateRequest is the request type for the Query/ConsensusState
 * RPC method. Besides the consensus state, it includes a proof and the height
 * from which the proof was retrieved.
 */
export interface QueryConsensusStateRequest {
  /** client identifier */
  client_id: string;
  /** consensus state revision number */
  revision_number: number;
  /** consensus state revision height */
  revision_height: number;
  /**
   * latest_height overrrides the height field and queries the latest stored
   * ConsensusState
   */
  latest_height: boolean;
}

/**
 * QueryConsensusStateResponse is the response type for the Query/ConsensusState
 * RPC method
 */
export interface QueryConsensusStateResponse {
  /** consensus state associated with the client identifier at the given height */
  consensus_state: Any | undefined;
  /** merkle proof of existence */
  proof: Uint8Array;
  /** height at which the proof was retrieved */
  proof_height: Height | undefined;
}

/**
 * QueryConsensusStatesRequest is the request type for the Query/ConsensusStates
 * RPC method.
 */
export interface QueryConsensusStatesRequest {
  /** client identifier */
  client_id: string;
  /** pagination request */
  pagination: PageRequest | undefined;
}

/**
 * QueryConsensusStatesResponse is the response type for the
 * Query/ConsensusStates RPC method
 */
export interface QueryConsensusStatesResponse {
  /** consensus states associated with the identifier */
  consensus_states: ConsensusStateWithHeight[];
  /** pagination response */
  pagination: PageResponse | undefined;
}

/**
 * QueryClientStatusRequest is the request type for the Query/ClientStatus RPC
 * method
 */
export interface QueryClientStatusRequest {
  /** client unique identifier */
  client_id: string;
}

/**
 * QueryClientStatusResponse is the response type for the Query/ClientStatus RPC
 * method. It returns the current status of the IBC client.
 */
export interface QueryClientStatusResponse {
  status: string;
}

/**
 * QueryClientParamsRequest is the request type for the Query/ClientParams RPC
 * method.
 */
export interface QueryClientParamsRequest {}

/**
 * QueryClientParamsResponse is the response type for the Query/ClientParams RPC
 * method.
 */
export interface QueryClientParamsResponse {
  /** params defines the parameters of the module. */
  params: Params | undefined;
}

/**
 * QueryUpgradedClientStateRequest is the request type for the
 * Query/UpgradedClientState RPC method
 */
export interface QueryUpgradedClientStateRequest {}

/**
 * QueryUpgradedClientStateResponse is the response type for the
 * Query/UpgradedClientState RPC method.
 */
export interface QueryUpgradedClientStateResponse {
  /** client state associated with the request identifier */
  upgraded_client_state: Any | undefined;
}

/**
 * QueryUpgradedConsensusStateRequest is the request type for the
 * Query/UpgradedConsensusState RPC method
 */
export interface QueryUpgradedConsensusStateRequest {}

/**
 * QueryUpgradedConsensusStateResponse is the response type for the
 * Query/UpgradedConsensusState RPC method.
 */
export interface QueryUpgradedConsensusStateResponse {
  /** Consensus state associated with the request identifier */
  upgraded_consensus_state: Any | undefined;
}

const baseQueryClientStateRequest: object = { client_id: "" };

export const QueryClientStateRequest = {
  encode(
    message: QueryClientStateRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.client_id !== "") {
      writer.uint32(10).string(message.client_id);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryClientStateRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryClientStateRequest,
    } as QueryClientStateRequest;
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

  fromJSON(object: any): QueryClientStateRequest {
    const message = {
      ...baseQueryClientStateRequest,
    } as QueryClientStateRequest;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
    }
    return message;
  },

  toJSON(message: QueryClientStateRequest): unknown {
    const obj: any = {};
    message.client_id !== undefined && (obj.client_id = message.client_id);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryClientStateRequest>
  ): QueryClientStateRequest {
    const message = {
      ...baseQueryClientStateRequest,
    } as QueryClientStateRequest;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
    }
    return message;
  },
};

const baseQueryClientStateResponse: object = {};

export const QueryClientStateResponse = {
  encode(
    message: QueryClientStateResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.client_state !== undefined) {
      Any.encode(message.client_state, writer.uint32(10).fork()).ldelim();
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
  ): QueryClientStateResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryClientStateResponse,
    } as QueryClientStateResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_state = Any.decode(reader, reader.uint32());
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

  fromJSON(object: any): QueryClientStateResponse {
    const message = {
      ...baseQueryClientStateResponse,
    } as QueryClientStateResponse;
    if (object.client_state !== undefined && object.client_state !== null) {
      message.client_state = Any.fromJSON(object.client_state);
    } else {
      message.client_state = undefined;
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

  toJSON(message: QueryClientStateResponse): unknown {
    const obj: any = {};
    message.client_state !== undefined &&
      (obj.client_state = message.client_state
        ? Any.toJSON(message.client_state)
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
    object: DeepPartial<QueryClientStateResponse>
  ): QueryClientStateResponse {
    const message = {
      ...baseQueryClientStateResponse,
    } as QueryClientStateResponse;
    if (object.client_state !== undefined && object.client_state !== null) {
      message.client_state = Any.fromPartial(object.client_state);
    } else {
      message.client_state = undefined;
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

const baseQueryClientStatesRequest: object = {};

export const QueryClientStatesRequest = {
  encode(
    message: QueryClientStatesRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryClientStatesRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryClientStatesRequest,
    } as QueryClientStatesRequest;
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

  fromJSON(object: any): QueryClientStatesRequest {
    const message = {
      ...baseQueryClientStatesRequest,
    } as QueryClientStatesRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryClientStatesRequest): unknown {
    const obj: any = {};
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryClientStatesRequest>
  ): QueryClientStatesRequest {
    const message = {
      ...baseQueryClientStatesRequest,
    } as QueryClientStatesRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryClientStatesResponse: object = {};

export const QueryClientStatesResponse = {
  encode(
    message: QueryClientStatesResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.client_states) {
      IdentifiedClientState.encode(v!, writer.uint32(10).fork()).ldelim();
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
  ): QueryClientStatesResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryClientStatesResponse,
    } as QueryClientStatesResponse;
    message.client_states = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_states.push(
            IdentifiedClientState.decode(reader, reader.uint32())
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

  fromJSON(object: any): QueryClientStatesResponse {
    const message = {
      ...baseQueryClientStatesResponse,
    } as QueryClientStatesResponse;
    message.client_states = [];
    if (object.client_states !== undefined && object.client_states !== null) {
      for (const e of object.client_states) {
        message.client_states.push(IdentifiedClientState.fromJSON(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryClientStatesResponse): unknown {
    const obj: any = {};
    if (message.client_states) {
      obj.client_states = message.client_states.map((e) =>
        e ? IdentifiedClientState.toJSON(e) : undefined
      );
    } else {
      obj.client_states = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryClientStatesResponse>
  ): QueryClientStatesResponse {
    const message = {
      ...baseQueryClientStatesResponse,
    } as QueryClientStatesResponse;
    message.client_states = [];
    if (object.client_states !== undefined && object.client_states !== null) {
      for (const e of object.client_states) {
        message.client_states.push(IdentifiedClientState.fromPartial(e));
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

const baseQueryConsensusStateRequest: object = {
  client_id: "",
  revision_number: 0,
  revision_height: 0,
  latest_height: false,
};

export const QueryConsensusStateRequest = {
  encode(
    message: QueryConsensusStateRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.client_id !== "") {
      writer.uint32(10).string(message.client_id);
    }
    if (message.revision_number !== 0) {
      writer.uint32(16).uint64(message.revision_number);
    }
    if (message.revision_height !== 0) {
      writer.uint32(24).uint64(message.revision_height);
    }
    if (message.latest_height === true) {
      writer.uint32(32).bool(message.latest_height);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryConsensusStateRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryConsensusStateRequest,
    } as QueryConsensusStateRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_id = reader.string();
          break;
        case 2:
          message.revision_number = longToNumber(reader.uint64() as Long);
          break;
        case 3:
          message.revision_height = longToNumber(reader.uint64() as Long);
          break;
        case 4:
          message.latest_height = reader.bool();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryConsensusStateRequest {
    const message = {
      ...baseQueryConsensusStateRequest,
    } as QueryConsensusStateRequest;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
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
    if (object.latest_height !== undefined && object.latest_height !== null) {
      message.latest_height = Boolean(object.latest_height);
    } else {
      message.latest_height = false;
    }
    return message;
  },

  toJSON(message: QueryConsensusStateRequest): unknown {
    const obj: any = {};
    message.client_id !== undefined && (obj.client_id = message.client_id);
    message.revision_number !== undefined &&
      (obj.revision_number = message.revision_number);
    message.revision_height !== undefined &&
      (obj.revision_height = message.revision_height);
    message.latest_height !== undefined &&
      (obj.latest_height = message.latest_height);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryConsensusStateRequest>
  ): QueryConsensusStateRequest {
    const message = {
      ...baseQueryConsensusStateRequest,
    } as QueryConsensusStateRequest;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
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
    if (object.latest_height !== undefined && object.latest_height !== null) {
      message.latest_height = object.latest_height;
    } else {
      message.latest_height = false;
    }
    return message;
  },
};

const baseQueryConsensusStateResponse: object = {};

export const QueryConsensusStateResponse = {
  encode(
    message: QueryConsensusStateResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.consensus_state !== undefined) {
      Any.encode(message.consensus_state, writer.uint32(10).fork()).ldelim();
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
  ): QueryConsensusStateResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryConsensusStateResponse,
    } as QueryConsensusStateResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.consensus_state = Any.decode(reader, reader.uint32());
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

  fromJSON(object: any): QueryConsensusStateResponse {
    const message = {
      ...baseQueryConsensusStateResponse,
    } as QueryConsensusStateResponse;
    if (
      object.consensus_state !== undefined &&
      object.consensus_state !== null
    ) {
      message.consensus_state = Any.fromJSON(object.consensus_state);
    } else {
      message.consensus_state = undefined;
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

  toJSON(message: QueryConsensusStateResponse): unknown {
    const obj: any = {};
    message.consensus_state !== undefined &&
      (obj.consensus_state = message.consensus_state
        ? Any.toJSON(message.consensus_state)
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
    object: DeepPartial<QueryConsensusStateResponse>
  ): QueryConsensusStateResponse {
    const message = {
      ...baseQueryConsensusStateResponse,
    } as QueryConsensusStateResponse;
    if (
      object.consensus_state !== undefined &&
      object.consensus_state !== null
    ) {
      message.consensus_state = Any.fromPartial(object.consensus_state);
    } else {
      message.consensus_state = undefined;
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

const baseQueryConsensusStatesRequest: object = { client_id: "" };

export const QueryConsensusStatesRequest = {
  encode(
    message: QueryConsensusStatesRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.client_id !== "") {
      writer.uint32(10).string(message.client_id);
    }
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryConsensusStatesRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryConsensusStatesRequest,
    } as QueryConsensusStatesRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_id = reader.string();
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

  fromJSON(object: any): QueryConsensusStatesRequest {
    const message = {
      ...baseQueryConsensusStatesRequest,
    } as QueryConsensusStatesRequest;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryConsensusStatesRequest): unknown {
    const obj: any = {};
    message.client_id !== undefined && (obj.client_id = message.client_id);
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryConsensusStatesRequest>
  ): QueryConsensusStatesRequest {
    const message = {
      ...baseQueryConsensusStatesRequest,
    } as QueryConsensusStatesRequest;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryConsensusStatesResponse: object = {};

export const QueryConsensusStatesResponse = {
  encode(
    message: QueryConsensusStatesResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.consensus_states) {
      ConsensusStateWithHeight.encode(v!, writer.uint32(10).fork()).ldelim();
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
  ): QueryConsensusStatesResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryConsensusStatesResponse,
    } as QueryConsensusStatesResponse;
    message.consensus_states = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.consensus_states.push(
            ConsensusStateWithHeight.decode(reader, reader.uint32())
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

  fromJSON(object: any): QueryConsensusStatesResponse {
    const message = {
      ...baseQueryConsensusStatesResponse,
    } as QueryConsensusStatesResponse;
    message.consensus_states = [];
    if (
      object.consensus_states !== undefined &&
      object.consensus_states !== null
    ) {
      for (const e of object.consensus_states) {
        message.consensus_states.push(ConsensusStateWithHeight.fromJSON(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryConsensusStatesResponse): unknown {
    const obj: any = {};
    if (message.consensus_states) {
      obj.consensus_states = message.consensus_states.map((e) =>
        e ? ConsensusStateWithHeight.toJSON(e) : undefined
      );
    } else {
      obj.consensus_states = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryConsensusStatesResponse>
  ): QueryConsensusStatesResponse {
    const message = {
      ...baseQueryConsensusStatesResponse,
    } as QueryConsensusStatesResponse;
    message.consensus_states = [];
    if (
      object.consensus_states !== undefined &&
      object.consensus_states !== null
    ) {
      for (const e of object.consensus_states) {
        message.consensus_states.push(ConsensusStateWithHeight.fromPartial(e));
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

const baseQueryClientStatusRequest: object = { client_id: "" };

export const QueryClientStatusRequest = {
  encode(
    message: QueryClientStatusRequest,
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
  ): QueryClientStatusRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryClientStatusRequest,
    } as QueryClientStatusRequest;
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

  fromJSON(object: any): QueryClientStatusRequest {
    const message = {
      ...baseQueryClientStatusRequest,
    } as QueryClientStatusRequest;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
    }
    return message;
  },

  toJSON(message: QueryClientStatusRequest): unknown {
    const obj: any = {};
    message.client_id !== undefined && (obj.client_id = message.client_id);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryClientStatusRequest>
  ): QueryClientStatusRequest {
    const message = {
      ...baseQueryClientStatusRequest,
    } as QueryClientStatusRequest;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
    }
    return message;
  },
};

const baseQueryClientStatusResponse: object = { status: "" };

export const QueryClientStatusResponse = {
  encode(
    message: QueryClientStatusResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.status !== "") {
      writer.uint32(10).string(message.status);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryClientStatusResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryClientStatusResponse,
    } as QueryClientStatusResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.status = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryClientStatusResponse {
    const message = {
      ...baseQueryClientStatusResponse,
    } as QueryClientStatusResponse;
    if (object.status !== undefined && object.status !== null) {
      message.status = String(object.status);
    } else {
      message.status = "";
    }
    return message;
  },

  toJSON(message: QueryClientStatusResponse): unknown {
    const obj: any = {};
    message.status !== undefined && (obj.status = message.status);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryClientStatusResponse>
  ): QueryClientStatusResponse {
    const message = {
      ...baseQueryClientStatusResponse,
    } as QueryClientStatusResponse;
    if (object.status !== undefined && object.status !== null) {
      message.status = object.status;
    } else {
      message.status = "";
    }
    return message;
  },
};

const baseQueryClientParamsRequest: object = {};

export const QueryClientParamsRequest = {
  encode(
    _: QueryClientParamsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryClientParamsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryClientParamsRequest,
    } as QueryClientParamsRequest;
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

  fromJSON(_: any): QueryClientParamsRequest {
    const message = {
      ...baseQueryClientParamsRequest,
    } as QueryClientParamsRequest;
    return message;
  },

  toJSON(_: QueryClientParamsRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<QueryClientParamsRequest>
  ): QueryClientParamsRequest {
    const message = {
      ...baseQueryClientParamsRequest,
    } as QueryClientParamsRequest;
    return message;
  },
};

const baseQueryClientParamsResponse: object = {};

export const QueryClientParamsResponse = {
  encode(
    message: QueryClientParamsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.params !== undefined) {
      Params.encode(message.params, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryClientParamsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryClientParamsResponse,
    } as QueryClientParamsResponse;
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

  fromJSON(object: any): QueryClientParamsResponse {
    const message = {
      ...baseQueryClientParamsResponse,
    } as QueryClientParamsResponse;
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromJSON(object.params);
    } else {
      message.params = undefined;
    }
    return message;
  },

  toJSON(message: QueryClientParamsResponse): unknown {
    const obj: any = {};
    message.params !== undefined &&
      (obj.params = message.params ? Params.toJSON(message.params) : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryClientParamsResponse>
  ): QueryClientParamsResponse {
    const message = {
      ...baseQueryClientParamsResponse,
    } as QueryClientParamsResponse;
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromPartial(object.params);
    } else {
      message.params = undefined;
    }
    return message;
  },
};

const baseQueryUpgradedClientStateRequest: object = {};

export const QueryUpgradedClientStateRequest = {
  encode(
    _: QueryUpgradedClientStateRequest,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryUpgradedClientStateRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryUpgradedClientStateRequest,
    } as QueryUpgradedClientStateRequest;
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

  fromJSON(_: any): QueryUpgradedClientStateRequest {
    const message = {
      ...baseQueryUpgradedClientStateRequest,
    } as QueryUpgradedClientStateRequest;
    return message;
  },

  toJSON(_: QueryUpgradedClientStateRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<QueryUpgradedClientStateRequest>
  ): QueryUpgradedClientStateRequest {
    const message = {
      ...baseQueryUpgradedClientStateRequest,
    } as QueryUpgradedClientStateRequest;
    return message;
  },
};

const baseQueryUpgradedClientStateResponse: object = {};

export const QueryUpgradedClientStateResponse = {
  encode(
    message: QueryUpgradedClientStateResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.upgraded_client_state !== undefined) {
      Any.encode(
        message.upgraded_client_state,
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryUpgradedClientStateResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryUpgradedClientStateResponse,
    } as QueryUpgradedClientStateResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.upgraded_client_state = Any.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryUpgradedClientStateResponse {
    const message = {
      ...baseQueryUpgradedClientStateResponse,
    } as QueryUpgradedClientStateResponse;
    if (
      object.upgraded_client_state !== undefined &&
      object.upgraded_client_state !== null
    ) {
      message.upgraded_client_state = Any.fromJSON(
        object.upgraded_client_state
      );
    } else {
      message.upgraded_client_state = undefined;
    }
    return message;
  },

  toJSON(message: QueryUpgradedClientStateResponse): unknown {
    const obj: any = {};
    message.upgraded_client_state !== undefined &&
      (obj.upgraded_client_state = message.upgraded_client_state
        ? Any.toJSON(message.upgraded_client_state)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryUpgradedClientStateResponse>
  ): QueryUpgradedClientStateResponse {
    const message = {
      ...baseQueryUpgradedClientStateResponse,
    } as QueryUpgradedClientStateResponse;
    if (
      object.upgraded_client_state !== undefined &&
      object.upgraded_client_state !== null
    ) {
      message.upgraded_client_state = Any.fromPartial(
        object.upgraded_client_state
      );
    } else {
      message.upgraded_client_state = undefined;
    }
    return message;
  },
};

const baseQueryUpgradedConsensusStateRequest: object = {};

export const QueryUpgradedConsensusStateRequest = {
  encode(
    _: QueryUpgradedConsensusStateRequest,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryUpgradedConsensusStateRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryUpgradedConsensusStateRequest,
    } as QueryUpgradedConsensusStateRequest;
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

  fromJSON(_: any): QueryUpgradedConsensusStateRequest {
    const message = {
      ...baseQueryUpgradedConsensusStateRequest,
    } as QueryUpgradedConsensusStateRequest;
    return message;
  },

  toJSON(_: QueryUpgradedConsensusStateRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<QueryUpgradedConsensusStateRequest>
  ): QueryUpgradedConsensusStateRequest {
    const message = {
      ...baseQueryUpgradedConsensusStateRequest,
    } as QueryUpgradedConsensusStateRequest;
    return message;
  },
};

const baseQueryUpgradedConsensusStateResponse: object = {};

export const QueryUpgradedConsensusStateResponse = {
  encode(
    message: QueryUpgradedConsensusStateResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.upgraded_consensus_state !== undefined) {
      Any.encode(
        message.upgraded_consensus_state,
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryUpgradedConsensusStateResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryUpgradedConsensusStateResponse,
    } as QueryUpgradedConsensusStateResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.upgraded_consensus_state = Any.decode(
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

  fromJSON(object: any): QueryUpgradedConsensusStateResponse {
    const message = {
      ...baseQueryUpgradedConsensusStateResponse,
    } as QueryUpgradedConsensusStateResponse;
    if (
      object.upgraded_consensus_state !== undefined &&
      object.upgraded_consensus_state !== null
    ) {
      message.upgraded_consensus_state = Any.fromJSON(
        object.upgraded_consensus_state
      );
    } else {
      message.upgraded_consensus_state = undefined;
    }
    return message;
  },

  toJSON(message: QueryUpgradedConsensusStateResponse): unknown {
    const obj: any = {};
    message.upgraded_consensus_state !== undefined &&
      (obj.upgraded_consensus_state = message.upgraded_consensus_state
        ? Any.toJSON(message.upgraded_consensus_state)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryUpgradedConsensusStateResponse>
  ): QueryUpgradedConsensusStateResponse {
    const message = {
      ...baseQueryUpgradedConsensusStateResponse,
    } as QueryUpgradedConsensusStateResponse;
    if (
      object.upgraded_consensus_state !== undefined &&
      object.upgraded_consensus_state !== null
    ) {
      message.upgraded_consensus_state = Any.fromPartial(
        object.upgraded_consensus_state
      );
    } else {
      message.upgraded_consensus_state = undefined;
    }
    return message;
  },
};

/** Query provides defines the gRPC querier service */
export interface Query {
  /** ClientState queries an IBC light client. */
  ClientState(
    request: QueryClientStateRequest
  ): Promise<QueryClientStateResponse>;
  /** ClientStates queries all the IBC light clients of a chain. */
  ClientStates(
    request: QueryClientStatesRequest
  ): Promise<QueryClientStatesResponse>;
  /**
   * ConsensusState queries a consensus state associated with a client state at
   * a given height.
   */
  ConsensusState(
    request: QueryConsensusStateRequest
  ): Promise<QueryConsensusStateResponse>;
  /**
   * ConsensusStates queries all the consensus state associated with a given
   * client.
   */
  ConsensusStates(
    request: QueryConsensusStatesRequest
  ): Promise<QueryConsensusStatesResponse>;
  /** Status queries the status of an IBC client. */
  ClientStatus(
    request: QueryClientStatusRequest
  ): Promise<QueryClientStatusResponse>;
  /** ClientParams queries all parameters of the ibc client. */
  ClientParams(
    request: QueryClientParamsRequest
  ): Promise<QueryClientParamsResponse>;
  /** UpgradedClientState queries an Upgraded IBC light client. */
  UpgradedClientState(
    request: QueryUpgradedClientStateRequest
  ): Promise<QueryUpgradedClientStateResponse>;
  /** UpgradedConsensusState queries an Upgraded IBC consensus state. */
  UpgradedConsensusState(
    request: QueryUpgradedConsensusStateRequest
  ): Promise<QueryUpgradedConsensusStateResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  ClientState(
    request: QueryClientStateRequest
  ): Promise<QueryClientStateResponse> {
    const data = QueryClientStateRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.client.v1.Query",
      "ClientState",
      data
    );
    return promise.then((data) =>
      QueryClientStateResponse.decode(new Reader(data))
    );
  }

  ClientStates(
    request: QueryClientStatesRequest
  ): Promise<QueryClientStatesResponse> {
    const data = QueryClientStatesRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.client.v1.Query",
      "ClientStates",
      data
    );
    return promise.then((data) =>
      QueryClientStatesResponse.decode(new Reader(data))
    );
  }

  ConsensusState(
    request: QueryConsensusStateRequest
  ): Promise<QueryConsensusStateResponse> {
    const data = QueryConsensusStateRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.client.v1.Query",
      "ConsensusState",
      data
    );
    return promise.then((data) =>
      QueryConsensusStateResponse.decode(new Reader(data))
    );
  }

  ConsensusStates(
    request: QueryConsensusStatesRequest
  ): Promise<QueryConsensusStatesResponse> {
    const data = QueryConsensusStatesRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.client.v1.Query",
      "ConsensusStates",
      data
    );
    return promise.then((data) =>
      QueryConsensusStatesResponse.decode(new Reader(data))
    );
  }

  ClientStatus(
    request: QueryClientStatusRequest
  ): Promise<QueryClientStatusResponse> {
    const data = QueryClientStatusRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.client.v1.Query",
      "ClientStatus",
      data
    );
    return promise.then((data) =>
      QueryClientStatusResponse.decode(new Reader(data))
    );
  }

  ClientParams(
    request: QueryClientParamsRequest
  ): Promise<QueryClientParamsResponse> {
    const data = QueryClientParamsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.client.v1.Query",
      "ClientParams",
      data
    );
    return promise.then((data) =>
      QueryClientParamsResponse.decode(new Reader(data))
    );
  }

  UpgradedClientState(
    request: QueryUpgradedClientStateRequest
  ): Promise<QueryUpgradedClientStateResponse> {
    const data = QueryUpgradedClientStateRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.client.v1.Query",
      "UpgradedClientState",
      data
    );
    return promise.then((data) =>
      QueryUpgradedClientStateResponse.decode(new Reader(data))
    );
  }

  UpgradedConsensusState(
    request: QueryUpgradedConsensusStateRequest
  ): Promise<QueryUpgradedConsensusStateResponse> {
    const data = QueryUpgradedConsensusStateRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.client.v1.Query",
      "UpgradedConsensusState",
      data
    );
    return promise.then((data) =>
      QueryUpgradedConsensusStateResponse.decode(new Reader(data))
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
