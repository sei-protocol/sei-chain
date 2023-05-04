/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import {
  Channel,
  IdentifiedChannel,
  PacketState,
} from "../../../../ibc/core/channel/v1/channel";
import {
  Height,
  IdentifiedClientState,
} from "../../../../ibc/core/client/v1/client";
import {
  PageRequest,
  PageResponse,
} from "../../../../cosmos/base/query/v1beta1/pagination";
import { Any } from "../../../../google/protobuf/any";

export const protobufPackage = "ibc.core.channel.v1";

/** QueryChannelRequest is the request type for the Query/Channel RPC method */
export interface QueryChannelRequest {
  /** port unique identifier */
  port_id: string;
  /** channel unique identifier */
  channel_id: string;
}

/**
 * QueryChannelResponse is the response type for the Query/Channel RPC method.
 * Besides the Channel end, it includes a proof and the height from which the
 * proof was retrieved.
 */
export interface QueryChannelResponse {
  /** channel associated with the request identifiers */
  channel: Channel | undefined;
  /** merkle proof of existence */
  proof: Uint8Array;
  /** height at which the proof was retrieved */
  proof_height: Height | undefined;
}

/** QueryChannelsRequest is the request type for the Query/Channels RPC method */
export interface QueryChannelsRequest {
  /** pagination request */
  pagination: PageRequest | undefined;
}

/** QueryChannelsResponse is the response type for the Query/Channels RPC method. */
export interface QueryChannelsResponse {
  /** list of stored channels of the chain. */
  channels: IdentifiedChannel[];
  /** pagination response */
  pagination: PageResponse | undefined;
  /** query block height */
  height: Height | undefined;
}

/**
 * QueryConnectionChannelsRequest is the request type for the
 * Query/QueryConnectionChannels RPC method
 */
export interface QueryConnectionChannelsRequest {
  /** connection unique identifier */
  connection: string;
  /** pagination request */
  pagination: PageRequest | undefined;
}

/**
 * QueryConnectionChannelsResponse is the Response type for the
 * Query/QueryConnectionChannels RPC method
 */
export interface QueryConnectionChannelsResponse {
  /** list of channels associated with a connection. */
  channels: IdentifiedChannel[];
  /** pagination response */
  pagination: PageResponse | undefined;
  /** query block height */
  height: Height | undefined;
}

/**
 * QueryChannelClientStateRequest is the request type for the Query/ClientState
 * RPC method
 */
export interface QueryChannelClientStateRequest {
  /** port unique identifier */
  port_id: string;
  /** channel unique identifier */
  channel_id: string;
}

/**
 * QueryChannelClientStateResponse is the Response type for the
 * Query/QueryChannelClientState RPC method
 */
export interface QueryChannelClientStateResponse {
  /** client state associated with the channel */
  identified_client_state: IdentifiedClientState | undefined;
  /** merkle proof of existence */
  proof: Uint8Array;
  /** height at which the proof was retrieved */
  proof_height: Height | undefined;
}

/**
 * QueryChannelConsensusStateRequest is the request type for the
 * Query/ConsensusState RPC method
 */
export interface QueryChannelConsensusStateRequest {
  /** port unique identifier */
  port_id: string;
  /** channel unique identifier */
  channel_id: string;
  /** revision number of the consensus state */
  revision_number: number;
  /** revision height of the consensus state */
  revision_height: number;
}

/**
 * QueryChannelClientStateResponse is the Response type for the
 * Query/QueryChannelClientState RPC method
 */
export interface QueryChannelConsensusStateResponse {
  /** consensus state associated with the channel */
  consensus_state: Any | undefined;
  /** client ID associated with the consensus state */
  client_id: string;
  /** merkle proof of existence */
  proof: Uint8Array;
  /** height at which the proof was retrieved */
  proof_height: Height | undefined;
}

/**
 * QueryPacketCommitmentRequest is the request type for the
 * Query/PacketCommitment RPC method
 */
export interface QueryPacketCommitmentRequest {
  /** port unique identifier */
  port_id: string;
  /** channel unique identifier */
  channel_id: string;
  /** packet sequence */
  sequence: number;
}

/**
 * QueryPacketCommitmentResponse defines the client query response for a packet
 * which also includes a proof and the height from which the proof was
 * retrieved
 */
export interface QueryPacketCommitmentResponse {
  /** packet associated with the request fields */
  commitment: Uint8Array;
  /** merkle proof of existence */
  proof: Uint8Array;
  /** height at which the proof was retrieved */
  proof_height: Height | undefined;
}

/**
 * QueryPacketCommitmentsRequest is the request type for the
 * Query/QueryPacketCommitments RPC method
 */
export interface QueryPacketCommitmentsRequest {
  /** port unique identifier */
  port_id: string;
  /** channel unique identifier */
  channel_id: string;
  /** pagination request */
  pagination: PageRequest | undefined;
}

/**
 * QueryPacketCommitmentsResponse is the request type for the
 * Query/QueryPacketCommitments RPC method
 */
export interface QueryPacketCommitmentsResponse {
  commitments: PacketState[];
  /** pagination response */
  pagination: PageResponse | undefined;
  /** query block height */
  height: Height | undefined;
}

/**
 * QueryPacketReceiptRequest is the request type for the
 * Query/PacketReceipt RPC method
 */
export interface QueryPacketReceiptRequest {
  /** port unique identifier */
  port_id: string;
  /** channel unique identifier */
  channel_id: string;
  /** packet sequence */
  sequence: number;
}

/**
 * QueryPacketReceiptResponse defines the client query response for a packet
 * receipt which also includes a proof, and the height from which the proof was
 * retrieved
 */
export interface QueryPacketReceiptResponse {
  /** success flag for if receipt exists */
  received: boolean;
  /** merkle proof of existence */
  proof: Uint8Array;
  /** height at which the proof was retrieved */
  proof_height: Height | undefined;
}

/**
 * QueryPacketAcknowledgementRequest is the request type for the
 * Query/PacketAcknowledgement RPC method
 */
export interface QueryPacketAcknowledgementRequest {
  /** port unique identifier */
  port_id: string;
  /** channel unique identifier */
  channel_id: string;
  /** packet sequence */
  sequence: number;
}

/**
 * QueryPacketAcknowledgementResponse defines the client query response for a
 * packet which also includes a proof and the height from which the
 * proof was retrieved
 */
export interface QueryPacketAcknowledgementResponse {
  /** packet associated with the request fields */
  acknowledgement: Uint8Array;
  /** merkle proof of existence */
  proof: Uint8Array;
  /** height at which the proof was retrieved */
  proof_height: Height | undefined;
}

/**
 * QueryPacketAcknowledgementsRequest is the request type for the
 * Query/QueryPacketCommitments RPC method
 */
export interface QueryPacketAcknowledgementsRequest {
  /** port unique identifier */
  port_id: string;
  /** channel unique identifier */
  channel_id: string;
  /** pagination request */
  pagination: PageRequest | undefined;
  /** list of packet sequences */
  packet_commitment_sequences: number[];
}

/**
 * QueryPacketAcknowledgemetsResponse is the request type for the
 * Query/QueryPacketAcknowledgements RPC method
 */
export interface QueryPacketAcknowledgementsResponse {
  acknowledgements: PacketState[];
  /** pagination response */
  pagination: PageResponse | undefined;
  /** query block height */
  height: Height | undefined;
}

/**
 * QueryUnreceivedPacketsRequest is the request type for the
 * Query/UnreceivedPackets RPC method
 */
export interface QueryUnreceivedPacketsRequest {
  /** port unique identifier */
  port_id: string;
  /** channel unique identifier */
  channel_id: string;
  /** list of packet sequences */
  packet_commitment_sequences: number[];
}

/**
 * QueryUnreceivedPacketsResponse is the response type for the
 * Query/UnreceivedPacketCommitments RPC method
 */
export interface QueryUnreceivedPacketsResponse {
  /** list of unreceived packet sequences */
  sequences: number[];
  /** query block height */
  height: Height | undefined;
}

/**
 * QueryUnreceivedAcks is the request type for the
 * Query/UnreceivedAcks RPC method
 */
export interface QueryUnreceivedAcksRequest {
  /** port unique identifier */
  port_id: string;
  /** channel unique identifier */
  channel_id: string;
  /** list of acknowledgement sequences */
  packet_ack_sequences: number[];
}

/**
 * QueryUnreceivedAcksResponse is the response type for the
 * Query/UnreceivedAcks RPC method
 */
export interface QueryUnreceivedAcksResponse {
  /** list of unreceived acknowledgement sequences */
  sequences: number[];
  /** query block height */
  height: Height | undefined;
}

/**
 * QueryNextSequenceReceiveRequest is the request type for the
 * Query/QueryNextSequenceReceiveRequest RPC method
 */
export interface QueryNextSequenceReceiveRequest {
  /** port unique identifier */
  port_id: string;
  /** channel unique identifier */
  channel_id: string;
}

/**
 * QuerySequenceResponse is the request type for the
 * Query/QueryNextSequenceReceiveResponse RPC method
 */
export interface QueryNextSequenceReceiveResponse {
  /** next sequence receive number */
  next_sequence_receive: number;
  /** merkle proof of existence */
  proof: Uint8Array;
  /** height at which the proof was retrieved */
  proof_height: Height | undefined;
}

const baseQueryChannelRequest: object = { port_id: "", channel_id: "" };

export const QueryChannelRequest = {
  encode(
    message: QueryChannelRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.channel_id !== "") {
      writer.uint32(18).string(message.channel_id);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryChannelRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryChannelRequest } as QueryChannelRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.port_id = reader.string();
          break;
        case 2:
          message.channel_id = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryChannelRequest {
    const message = { ...baseQueryChannelRequest } as QueryChannelRequest;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = String(object.port_id);
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = String(object.channel_id);
    } else {
      message.channel_id = "";
    }
    return message;
  },

  toJSON(message: QueryChannelRequest): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryChannelRequest>): QueryChannelRequest {
    const message = { ...baseQueryChannelRequest } as QueryChannelRequest;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = object.port_id;
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = object.channel_id;
    } else {
      message.channel_id = "";
    }
    return message;
  },
};

const baseQueryChannelResponse: object = {};

export const QueryChannelResponse = {
  encode(
    message: QueryChannelResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.channel !== undefined) {
      Channel.encode(message.channel, writer.uint32(10).fork()).ldelim();
    }
    if (message.proof.length !== 0) {
      writer.uint32(18).bytes(message.proof);
    }
    if (message.proof_height !== undefined) {
      Height.encode(message.proof_height, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryChannelResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryChannelResponse } as QueryChannelResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.channel = Channel.decode(reader, reader.uint32());
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

  fromJSON(object: any): QueryChannelResponse {
    const message = { ...baseQueryChannelResponse } as QueryChannelResponse;
    if (object.channel !== undefined && object.channel !== null) {
      message.channel = Channel.fromJSON(object.channel);
    } else {
      message.channel = undefined;
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

  toJSON(message: QueryChannelResponse): unknown {
    const obj: any = {};
    message.channel !== undefined &&
      (obj.channel = message.channel
        ? Channel.toJSON(message.channel)
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

  fromPartial(object: DeepPartial<QueryChannelResponse>): QueryChannelResponse {
    const message = { ...baseQueryChannelResponse } as QueryChannelResponse;
    if (object.channel !== undefined && object.channel !== null) {
      message.channel = Channel.fromPartial(object.channel);
    } else {
      message.channel = undefined;
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

const baseQueryChannelsRequest: object = {};

export const QueryChannelsRequest = {
  encode(
    message: QueryChannelsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryChannelsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryChannelsRequest } as QueryChannelsRequest;
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

  fromJSON(object: any): QueryChannelsRequest {
    const message = { ...baseQueryChannelsRequest } as QueryChannelsRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryChannelsRequest): unknown {
    const obj: any = {};
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryChannelsRequest>): QueryChannelsRequest {
    const message = { ...baseQueryChannelsRequest } as QueryChannelsRequest;
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryChannelsResponse: object = {};

export const QueryChannelsResponse = {
  encode(
    message: QueryChannelsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.channels) {
      IdentifiedChannel.encode(v!, writer.uint32(10).fork()).ldelim();
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

  decode(input: Reader | Uint8Array, length?: number): QueryChannelsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryChannelsResponse } as QueryChannelsResponse;
    message.channels = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.channels.push(
            IdentifiedChannel.decode(reader, reader.uint32())
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

  fromJSON(object: any): QueryChannelsResponse {
    const message = { ...baseQueryChannelsResponse } as QueryChannelsResponse;
    message.channels = [];
    if (object.channels !== undefined && object.channels !== null) {
      for (const e of object.channels) {
        message.channels.push(IdentifiedChannel.fromJSON(e));
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

  toJSON(message: QueryChannelsResponse): unknown {
    const obj: any = {};
    if (message.channels) {
      obj.channels = message.channels.map((e) =>
        e ? IdentifiedChannel.toJSON(e) : undefined
      );
    } else {
      obj.channels = [];
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
    object: DeepPartial<QueryChannelsResponse>
  ): QueryChannelsResponse {
    const message = { ...baseQueryChannelsResponse } as QueryChannelsResponse;
    message.channels = [];
    if (object.channels !== undefined && object.channels !== null) {
      for (const e of object.channels) {
        message.channels.push(IdentifiedChannel.fromPartial(e));
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

const baseQueryConnectionChannelsRequest: object = { connection: "" };

export const QueryConnectionChannelsRequest = {
  encode(
    message: QueryConnectionChannelsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.connection !== "") {
      writer.uint32(10).string(message.connection);
    }
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryConnectionChannelsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryConnectionChannelsRequest,
    } as QueryConnectionChannelsRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.connection = reader.string();
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

  fromJSON(object: any): QueryConnectionChannelsRequest {
    const message = {
      ...baseQueryConnectionChannelsRequest,
    } as QueryConnectionChannelsRequest;
    if (object.connection !== undefined && object.connection !== null) {
      message.connection = String(object.connection);
    } else {
      message.connection = "";
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryConnectionChannelsRequest): unknown {
    const obj: any = {};
    message.connection !== undefined && (obj.connection = message.connection);
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryConnectionChannelsRequest>
  ): QueryConnectionChannelsRequest {
    const message = {
      ...baseQueryConnectionChannelsRequest,
    } as QueryConnectionChannelsRequest;
    if (object.connection !== undefined && object.connection !== null) {
      message.connection = object.connection;
    } else {
      message.connection = "";
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryConnectionChannelsResponse: object = {};

export const QueryConnectionChannelsResponse = {
  encode(
    message: QueryConnectionChannelsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.channels) {
      IdentifiedChannel.encode(v!, writer.uint32(10).fork()).ldelim();
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
  ): QueryConnectionChannelsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryConnectionChannelsResponse,
    } as QueryConnectionChannelsResponse;
    message.channels = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.channels.push(
            IdentifiedChannel.decode(reader, reader.uint32())
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

  fromJSON(object: any): QueryConnectionChannelsResponse {
    const message = {
      ...baseQueryConnectionChannelsResponse,
    } as QueryConnectionChannelsResponse;
    message.channels = [];
    if (object.channels !== undefined && object.channels !== null) {
      for (const e of object.channels) {
        message.channels.push(IdentifiedChannel.fromJSON(e));
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

  toJSON(message: QueryConnectionChannelsResponse): unknown {
    const obj: any = {};
    if (message.channels) {
      obj.channels = message.channels.map((e) =>
        e ? IdentifiedChannel.toJSON(e) : undefined
      );
    } else {
      obj.channels = [];
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
    object: DeepPartial<QueryConnectionChannelsResponse>
  ): QueryConnectionChannelsResponse {
    const message = {
      ...baseQueryConnectionChannelsResponse,
    } as QueryConnectionChannelsResponse;
    message.channels = [];
    if (object.channels !== undefined && object.channels !== null) {
      for (const e of object.channels) {
        message.channels.push(IdentifiedChannel.fromPartial(e));
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

const baseQueryChannelClientStateRequest: object = {
  port_id: "",
  channel_id: "",
};

export const QueryChannelClientStateRequest = {
  encode(
    message: QueryChannelClientStateRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.channel_id !== "") {
      writer.uint32(18).string(message.channel_id);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryChannelClientStateRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryChannelClientStateRequest,
    } as QueryChannelClientStateRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.port_id = reader.string();
          break;
        case 2:
          message.channel_id = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryChannelClientStateRequest {
    const message = {
      ...baseQueryChannelClientStateRequest,
    } as QueryChannelClientStateRequest;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = String(object.port_id);
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = String(object.channel_id);
    } else {
      message.channel_id = "";
    }
    return message;
  },

  toJSON(message: QueryChannelClientStateRequest): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryChannelClientStateRequest>
  ): QueryChannelClientStateRequest {
    const message = {
      ...baseQueryChannelClientStateRequest,
    } as QueryChannelClientStateRequest;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = object.port_id;
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = object.channel_id;
    } else {
      message.channel_id = "";
    }
    return message;
  },
};

const baseQueryChannelClientStateResponse: object = {};

export const QueryChannelClientStateResponse = {
  encode(
    message: QueryChannelClientStateResponse,
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
  ): QueryChannelClientStateResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryChannelClientStateResponse,
    } as QueryChannelClientStateResponse;
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

  fromJSON(object: any): QueryChannelClientStateResponse {
    const message = {
      ...baseQueryChannelClientStateResponse,
    } as QueryChannelClientStateResponse;
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

  toJSON(message: QueryChannelClientStateResponse): unknown {
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
    object: DeepPartial<QueryChannelClientStateResponse>
  ): QueryChannelClientStateResponse {
    const message = {
      ...baseQueryChannelClientStateResponse,
    } as QueryChannelClientStateResponse;
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

const baseQueryChannelConsensusStateRequest: object = {
  port_id: "",
  channel_id: "",
  revision_number: 0,
  revision_height: 0,
};

export const QueryChannelConsensusStateRequest = {
  encode(
    message: QueryChannelConsensusStateRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.channel_id !== "") {
      writer.uint32(18).string(message.channel_id);
    }
    if (message.revision_number !== 0) {
      writer.uint32(24).uint64(message.revision_number);
    }
    if (message.revision_height !== 0) {
      writer.uint32(32).uint64(message.revision_height);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryChannelConsensusStateRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryChannelConsensusStateRequest,
    } as QueryChannelConsensusStateRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.port_id = reader.string();
          break;
        case 2:
          message.channel_id = reader.string();
          break;
        case 3:
          message.revision_number = longToNumber(reader.uint64() as Long);
          break;
        case 4:
          message.revision_height = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryChannelConsensusStateRequest {
    const message = {
      ...baseQueryChannelConsensusStateRequest,
    } as QueryChannelConsensusStateRequest;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = String(object.port_id);
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = String(object.channel_id);
    } else {
      message.channel_id = "";
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

  toJSON(message: QueryChannelConsensusStateRequest): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    message.revision_number !== undefined &&
      (obj.revision_number = message.revision_number);
    message.revision_height !== undefined &&
      (obj.revision_height = message.revision_height);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryChannelConsensusStateRequest>
  ): QueryChannelConsensusStateRequest {
    const message = {
      ...baseQueryChannelConsensusStateRequest,
    } as QueryChannelConsensusStateRequest;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = object.port_id;
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = object.channel_id;
    } else {
      message.channel_id = "";
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

const baseQueryChannelConsensusStateResponse: object = { client_id: "" };

export const QueryChannelConsensusStateResponse = {
  encode(
    message: QueryChannelConsensusStateResponse,
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
  ): QueryChannelConsensusStateResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryChannelConsensusStateResponse,
    } as QueryChannelConsensusStateResponse;
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

  fromJSON(object: any): QueryChannelConsensusStateResponse {
    const message = {
      ...baseQueryChannelConsensusStateResponse,
    } as QueryChannelConsensusStateResponse;
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

  toJSON(message: QueryChannelConsensusStateResponse): unknown {
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
    object: DeepPartial<QueryChannelConsensusStateResponse>
  ): QueryChannelConsensusStateResponse {
    const message = {
      ...baseQueryChannelConsensusStateResponse,
    } as QueryChannelConsensusStateResponse;
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

const baseQueryPacketCommitmentRequest: object = {
  port_id: "",
  channel_id: "",
  sequence: 0,
};

export const QueryPacketCommitmentRequest = {
  encode(
    message: QueryPacketCommitmentRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.channel_id !== "") {
      writer.uint32(18).string(message.channel_id);
    }
    if (message.sequence !== 0) {
      writer.uint32(24).uint64(message.sequence);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryPacketCommitmentRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryPacketCommitmentRequest,
    } as QueryPacketCommitmentRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.port_id = reader.string();
          break;
        case 2:
          message.channel_id = reader.string();
          break;
        case 3:
          message.sequence = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryPacketCommitmentRequest {
    const message = {
      ...baseQueryPacketCommitmentRequest,
    } as QueryPacketCommitmentRequest;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = String(object.port_id);
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = String(object.channel_id);
    } else {
      message.channel_id = "";
    }
    if (object.sequence !== undefined && object.sequence !== null) {
      message.sequence = Number(object.sequence);
    } else {
      message.sequence = 0;
    }
    return message;
  },

  toJSON(message: QueryPacketCommitmentRequest): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    message.sequence !== undefined && (obj.sequence = message.sequence);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryPacketCommitmentRequest>
  ): QueryPacketCommitmentRequest {
    const message = {
      ...baseQueryPacketCommitmentRequest,
    } as QueryPacketCommitmentRequest;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = object.port_id;
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = object.channel_id;
    } else {
      message.channel_id = "";
    }
    if (object.sequence !== undefined && object.sequence !== null) {
      message.sequence = object.sequence;
    } else {
      message.sequence = 0;
    }
    return message;
  },
};

const baseQueryPacketCommitmentResponse: object = {};

export const QueryPacketCommitmentResponse = {
  encode(
    message: QueryPacketCommitmentResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.commitment.length !== 0) {
      writer.uint32(10).bytes(message.commitment);
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
  ): QueryPacketCommitmentResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryPacketCommitmentResponse,
    } as QueryPacketCommitmentResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.commitment = reader.bytes();
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

  fromJSON(object: any): QueryPacketCommitmentResponse {
    const message = {
      ...baseQueryPacketCommitmentResponse,
    } as QueryPacketCommitmentResponse;
    if (object.commitment !== undefined && object.commitment !== null) {
      message.commitment = bytesFromBase64(object.commitment);
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

  toJSON(message: QueryPacketCommitmentResponse): unknown {
    const obj: any = {};
    message.commitment !== undefined &&
      (obj.commitment = base64FromBytes(
        message.commitment !== undefined ? message.commitment : new Uint8Array()
      ));
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
    object: DeepPartial<QueryPacketCommitmentResponse>
  ): QueryPacketCommitmentResponse {
    const message = {
      ...baseQueryPacketCommitmentResponse,
    } as QueryPacketCommitmentResponse;
    if (object.commitment !== undefined && object.commitment !== null) {
      message.commitment = object.commitment;
    } else {
      message.commitment = new Uint8Array();
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

const baseQueryPacketCommitmentsRequest: object = {
  port_id: "",
  channel_id: "",
};

export const QueryPacketCommitmentsRequest = {
  encode(
    message: QueryPacketCommitmentsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.channel_id !== "") {
      writer.uint32(18).string(message.channel_id);
    }
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryPacketCommitmentsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryPacketCommitmentsRequest,
    } as QueryPacketCommitmentsRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.port_id = reader.string();
          break;
        case 2:
          message.channel_id = reader.string();
          break;
        case 3:
          message.pagination = PageRequest.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryPacketCommitmentsRequest {
    const message = {
      ...baseQueryPacketCommitmentsRequest,
    } as QueryPacketCommitmentsRequest;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = String(object.port_id);
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = String(object.channel_id);
    } else {
      message.channel_id = "";
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryPacketCommitmentsRequest): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryPacketCommitmentsRequest>
  ): QueryPacketCommitmentsRequest {
    const message = {
      ...baseQueryPacketCommitmentsRequest,
    } as QueryPacketCommitmentsRequest;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = object.port_id;
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = object.channel_id;
    } else {
      message.channel_id = "";
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryPacketCommitmentsResponse: object = {};

export const QueryPacketCommitmentsResponse = {
  encode(
    message: QueryPacketCommitmentsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.commitments) {
      PacketState.encode(v!, writer.uint32(10).fork()).ldelim();
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
  ): QueryPacketCommitmentsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryPacketCommitmentsResponse,
    } as QueryPacketCommitmentsResponse;
    message.commitments = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.commitments.push(PacketState.decode(reader, reader.uint32()));
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

  fromJSON(object: any): QueryPacketCommitmentsResponse {
    const message = {
      ...baseQueryPacketCommitmentsResponse,
    } as QueryPacketCommitmentsResponse;
    message.commitments = [];
    if (object.commitments !== undefined && object.commitments !== null) {
      for (const e of object.commitments) {
        message.commitments.push(PacketState.fromJSON(e));
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

  toJSON(message: QueryPacketCommitmentsResponse): unknown {
    const obj: any = {};
    if (message.commitments) {
      obj.commitments = message.commitments.map((e) =>
        e ? PacketState.toJSON(e) : undefined
      );
    } else {
      obj.commitments = [];
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
    object: DeepPartial<QueryPacketCommitmentsResponse>
  ): QueryPacketCommitmentsResponse {
    const message = {
      ...baseQueryPacketCommitmentsResponse,
    } as QueryPacketCommitmentsResponse;
    message.commitments = [];
    if (object.commitments !== undefined && object.commitments !== null) {
      for (const e of object.commitments) {
        message.commitments.push(PacketState.fromPartial(e));
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

const baseQueryPacketReceiptRequest: object = {
  port_id: "",
  channel_id: "",
  sequence: 0,
};

export const QueryPacketReceiptRequest = {
  encode(
    message: QueryPacketReceiptRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.channel_id !== "") {
      writer.uint32(18).string(message.channel_id);
    }
    if (message.sequence !== 0) {
      writer.uint32(24).uint64(message.sequence);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryPacketReceiptRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryPacketReceiptRequest,
    } as QueryPacketReceiptRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.port_id = reader.string();
          break;
        case 2:
          message.channel_id = reader.string();
          break;
        case 3:
          message.sequence = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryPacketReceiptRequest {
    const message = {
      ...baseQueryPacketReceiptRequest,
    } as QueryPacketReceiptRequest;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = String(object.port_id);
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = String(object.channel_id);
    } else {
      message.channel_id = "";
    }
    if (object.sequence !== undefined && object.sequence !== null) {
      message.sequence = Number(object.sequence);
    } else {
      message.sequence = 0;
    }
    return message;
  },

  toJSON(message: QueryPacketReceiptRequest): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    message.sequence !== undefined && (obj.sequence = message.sequence);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryPacketReceiptRequest>
  ): QueryPacketReceiptRequest {
    const message = {
      ...baseQueryPacketReceiptRequest,
    } as QueryPacketReceiptRequest;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = object.port_id;
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = object.channel_id;
    } else {
      message.channel_id = "";
    }
    if (object.sequence !== undefined && object.sequence !== null) {
      message.sequence = object.sequence;
    } else {
      message.sequence = 0;
    }
    return message;
  },
};

const baseQueryPacketReceiptResponse: object = { received: false };

export const QueryPacketReceiptResponse = {
  encode(
    message: QueryPacketReceiptResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.received === true) {
      writer.uint32(16).bool(message.received);
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
  ): QueryPacketReceiptResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryPacketReceiptResponse,
    } as QueryPacketReceiptResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 2:
          message.received = reader.bool();
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

  fromJSON(object: any): QueryPacketReceiptResponse {
    const message = {
      ...baseQueryPacketReceiptResponse,
    } as QueryPacketReceiptResponse;
    if (object.received !== undefined && object.received !== null) {
      message.received = Boolean(object.received);
    } else {
      message.received = false;
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

  toJSON(message: QueryPacketReceiptResponse): unknown {
    const obj: any = {};
    message.received !== undefined && (obj.received = message.received);
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
    object: DeepPartial<QueryPacketReceiptResponse>
  ): QueryPacketReceiptResponse {
    const message = {
      ...baseQueryPacketReceiptResponse,
    } as QueryPacketReceiptResponse;
    if (object.received !== undefined && object.received !== null) {
      message.received = object.received;
    } else {
      message.received = false;
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

const baseQueryPacketAcknowledgementRequest: object = {
  port_id: "",
  channel_id: "",
  sequence: 0,
};

export const QueryPacketAcknowledgementRequest = {
  encode(
    message: QueryPacketAcknowledgementRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.channel_id !== "") {
      writer.uint32(18).string(message.channel_id);
    }
    if (message.sequence !== 0) {
      writer.uint32(24).uint64(message.sequence);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryPacketAcknowledgementRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryPacketAcknowledgementRequest,
    } as QueryPacketAcknowledgementRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.port_id = reader.string();
          break;
        case 2:
          message.channel_id = reader.string();
          break;
        case 3:
          message.sequence = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryPacketAcknowledgementRequest {
    const message = {
      ...baseQueryPacketAcknowledgementRequest,
    } as QueryPacketAcknowledgementRequest;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = String(object.port_id);
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = String(object.channel_id);
    } else {
      message.channel_id = "";
    }
    if (object.sequence !== undefined && object.sequence !== null) {
      message.sequence = Number(object.sequence);
    } else {
      message.sequence = 0;
    }
    return message;
  },

  toJSON(message: QueryPacketAcknowledgementRequest): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    message.sequence !== undefined && (obj.sequence = message.sequence);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryPacketAcknowledgementRequest>
  ): QueryPacketAcknowledgementRequest {
    const message = {
      ...baseQueryPacketAcknowledgementRequest,
    } as QueryPacketAcknowledgementRequest;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = object.port_id;
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = object.channel_id;
    } else {
      message.channel_id = "";
    }
    if (object.sequence !== undefined && object.sequence !== null) {
      message.sequence = object.sequence;
    } else {
      message.sequence = 0;
    }
    return message;
  },
};

const baseQueryPacketAcknowledgementResponse: object = {};

export const QueryPacketAcknowledgementResponse = {
  encode(
    message: QueryPacketAcknowledgementResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.acknowledgement.length !== 0) {
      writer.uint32(10).bytes(message.acknowledgement);
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
  ): QueryPacketAcknowledgementResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryPacketAcknowledgementResponse,
    } as QueryPacketAcknowledgementResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.acknowledgement = reader.bytes();
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

  fromJSON(object: any): QueryPacketAcknowledgementResponse {
    const message = {
      ...baseQueryPacketAcknowledgementResponse,
    } as QueryPacketAcknowledgementResponse;
    if (
      object.acknowledgement !== undefined &&
      object.acknowledgement !== null
    ) {
      message.acknowledgement = bytesFromBase64(object.acknowledgement);
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

  toJSON(message: QueryPacketAcknowledgementResponse): unknown {
    const obj: any = {};
    message.acknowledgement !== undefined &&
      (obj.acknowledgement = base64FromBytes(
        message.acknowledgement !== undefined
          ? message.acknowledgement
          : new Uint8Array()
      ));
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
    object: DeepPartial<QueryPacketAcknowledgementResponse>
  ): QueryPacketAcknowledgementResponse {
    const message = {
      ...baseQueryPacketAcknowledgementResponse,
    } as QueryPacketAcknowledgementResponse;
    if (
      object.acknowledgement !== undefined &&
      object.acknowledgement !== null
    ) {
      message.acknowledgement = object.acknowledgement;
    } else {
      message.acknowledgement = new Uint8Array();
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

const baseQueryPacketAcknowledgementsRequest: object = {
  port_id: "",
  channel_id: "",
  packet_commitment_sequences: 0,
};

export const QueryPacketAcknowledgementsRequest = {
  encode(
    message: QueryPacketAcknowledgementsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.channel_id !== "") {
      writer.uint32(18).string(message.channel_id);
    }
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(26).fork()).ldelim();
    }
    writer.uint32(34).fork();
    for (const v of message.packet_commitment_sequences) {
      writer.uint64(v);
    }
    writer.ldelim();
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryPacketAcknowledgementsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryPacketAcknowledgementsRequest,
    } as QueryPacketAcknowledgementsRequest;
    message.packet_commitment_sequences = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.port_id = reader.string();
          break;
        case 2:
          message.channel_id = reader.string();
          break;
        case 3:
          message.pagination = PageRequest.decode(reader, reader.uint32());
          break;
        case 4:
          if ((tag & 7) === 2) {
            const end2 = reader.uint32() + reader.pos;
            while (reader.pos < end2) {
              message.packet_commitment_sequences.push(
                longToNumber(reader.uint64() as Long)
              );
            }
          } else {
            message.packet_commitment_sequences.push(
              longToNumber(reader.uint64() as Long)
            );
          }
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryPacketAcknowledgementsRequest {
    const message = {
      ...baseQueryPacketAcknowledgementsRequest,
    } as QueryPacketAcknowledgementsRequest;
    message.packet_commitment_sequences = [];
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = String(object.port_id);
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = String(object.channel_id);
    } else {
      message.channel_id = "";
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    if (
      object.packet_commitment_sequences !== undefined &&
      object.packet_commitment_sequences !== null
    ) {
      for (const e of object.packet_commitment_sequences) {
        message.packet_commitment_sequences.push(Number(e));
      }
    }
    return message;
  },

  toJSON(message: QueryPacketAcknowledgementsRequest): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    if (message.packet_commitment_sequences) {
      obj.packet_commitment_sequences = message.packet_commitment_sequences.map(
        (e) => e
      );
    } else {
      obj.packet_commitment_sequences = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryPacketAcknowledgementsRequest>
  ): QueryPacketAcknowledgementsRequest {
    const message = {
      ...baseQueryPacketAcknowledgementsRequest,
    } as QueryPacketAcknowledgementsRequest;
    message.packet_commitment_sequences = [];
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = object.port_id;
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = object.channel_id;
    } else {
      message.channel_id = "";
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    if (
      object.packet_commitment_sequences !== undefined &&
      object.packet_commitment_sequences !== null
    ) {
      for (const e of object.packet_commitment_sequences) {
        message.packet_commitment_sequences.push(e);
      }
    }
    return message;
  },
};

const baseQueryPacketAcknowledgementsResponse: object = {};

export const QueryPacketAcknowledgementsResponse = {
  encode(
    message: QueryPacketAcknowledgementsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.acknowledgements) {
      PacketState.encode(v!, writer.uint32(10).fork()).ldelim();
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
  ): QueryPacketAcknowledgementsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryPacketAcknowledgementsResponse,
    } as QueryPacketAcknowledgementsResponse;
    message.acknowledgements = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.acknowledgements.push(
            PacketState.decode(reader, reader.uint32())
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

  fromJSON(object: any): QueryPacketAcknowledgementsResponse {
    const message = {
      ...baseQueryPacketAcknowledgementsResponse,
    } as QueryPacketAcknowledgementsResponse;
    message.acknowledgements = [];
    if (
      object.acknowledgements !== undefined &&
      object.acknowledgements !== null
    ) {
      for (const e of object.acknowledgements) {
        message.acknowledgements.push(PacketState.fromJSON(e));
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

  toJSON(message: QueryPacketAcknowledgementsResponse): unknown {
    const obj: any = {};
    if (message.acknowledgements) {
      obj.acknowledgements = message.acknowledgements.map((e) =>
        e ? PacketState.toJSON(e) : undefined
      );
    } else {
      obj.acknowledgements = [];
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
    object: DeepPartial<QueryPacketAcknowledgementsResponse>
  ): QueryPacketAcknowledgementsResponse {
    const message = {
      ...baseQueryPacketAcknowledgementsResponse,
    } as QueryPacketAcknowledgementsResponse;
    message.acknowledgements = [];
    if (
      object.acknowledgements !== undefined &&
      object.acknowledgements !== null
    ) {
      for (const e of object.acknowledgements) {
        message.acknowledgements.push(PacketState.fromPartial(e));
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

const baseQueryUnreceivedPacketsRequest: object = {
  port_id: "",
  channel_id: "",
  packet_commitment_sequences: 0,
};

export const QueryUnreceivedPacketsRequest = {
  encode(
    message: QueryUnreceivedPacketsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.channel_id !== "") {
      writer.uint32(18).string(message.channel_id);
    }
    writer.uint32(26).fork();
    for (const v of message.packet_commitment_sequences) {
      writer.uint64(v);
    }
    writer.ldelim();
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryUnreceivedPacketsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryUnreceivedPacketsRequest,
    } as QueryUnreceivedPacketsRequest;
    message.packet_commitment_sequences = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.port_id = reader.string();
          break;
        case 2:
          message.channel_id = reader.string();
          break;
        case 3:
          if ((tag & 7) === 2) {
            const end2 = reader.uint32() + reader.pos;
            while (reader.pos < end2) {
              message.packet_commitment_sequences.push(
                longToNumber(reader.uint64() as Long)
              );
            }
          } else {
            message.packet_commitment_sequences.push(
              longToNumber(reader.uint64() as Long)
            );
          }
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryUnreceivedPacketsRequest {
    const message = {
      ...baseQueryUnreceivedPacketsRequest,
    } as QueryUnreceivedPacketsRequest;
    message.packet_commitment_sequences = [];
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = String(object.port_id);
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = String(object.channel_id);
    } else {
      message.channel_id = "";
    }
    if (
      object.packet_commitment_sequences !== undefined &&
      object.packet_commitment_sequences !== null
    ) {
      for (const e of object.packet_commitment_sequences) {
        message.packet_commitment_sequences.push(Number(e));
      }
    }
    return message;
  },

  toJSON(message: QueryUnreceivedPacketsRequest): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    if (message.packet_commitment_sequences) {
      obj.packet_commitment_sequences = message.packet_commitment_sequences.map(
        (e) => e
      );
    } else {
      obj.packet_commitment_sequences = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryUnreceivedPacketsRequest>
  ): QueryUnreceivedPacketsRequest {
    const message = {
      ...baseQueryUnreceivedPacketsRequest,
    } as QueryUnreceivedPacketsRequest;
    message.packet_commitment_sequences = [];
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = object.port_id;
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = object.channel_id;
    } else {
      message.channel_id = "";
    }
    if (
      object.packet_commitment_sequences !== undefined &&
      object.packet_commitment_sequences !== null
    ) {
      for (const e of object.packet_commitment_sequences) {
        message.packet_commitment_sequences.push(e);
      }
    }
    return message;
  },
};

const baseQueryUnreceivedPacketsResponse: object = { sequences: 0 };

export const QueryUnreceivedPacketsResponse = {
  encode(
    message: QueryUnreceivedPacketsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    writer.uint32(10).fork();
    for (const v of message.sequences) {
      writer.uint64(v);
    }
    writer.ldelim();
    if (message.height !== undefined) {
      Height.encode(message.height, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryUnreceivedPacketsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryUnreceivedPacketsResponse,
    } as QueryUnreceivedPacketsResponse;
    message.sequences = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          if ((tag & 7) === 2) {
            const end2 = reader.uint32() + reader.pos;
            while (reader.pos < end2) {
              message.sequences.push(longToNumber(reader.uint64() as Long));
            }
          } else {
            message.sequences.push(longToNumber(reader.uint64() as Long));
          }
          break;
        case 2:
          message.height = Height.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryUnreceivedPacketsResponse {
    const message = {
      ...baseQueryUnreceivedPacketsResponse,
    } as QueryUnreceivedPacketsResponse;
    message.sequences = [];
    if (object.sequences !== undefined && object.sequences !== null) {
      for (const e of object.sequences) {
        message.sequences.push(Number(e));
      }
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = Height.fromJSON(object.height);
    } else {
      message.height = undefined;
    }
    return message;
  },

  toJSON(message: QueryUnreceivedPacketsResponse): unknown {
    const obj: any = {};
    if (message.sequences) {
      obj.sequences = message.sequences.map((e) => e);
    } else {
      obj.sequences = [];
    }
    message.height !== undefined &&
      (obj.height = message.height ? Height.toJSON(message.height) : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryUnreceivedPacketsResponse>
  ): QueryUnreceivedPacketsResponse {
    const message = {
      ...baseQueryUnreceivedPacketsResponse,
    } as QueryUnreceivedPacketsResponse;
    message.sequences = [];
    if (object.sequences !== undefined && object.sequences !== null) {
      for (const e of object.sequences) {
        message.sequences.push(e);
      }
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = Height.fromPartial(object.height);
    } else {
      message.height = undefined;
    }
    return message;
  },
};

const baseQueryUnreceivedAcksRequest: object = {
  port_id: "",
  channel_id: "",
  packet_ack_sequences: 0,
};

export const QueryUnreceivedAcksRequest = {
  encode(
    message: QueryUnreceivedAcksRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.channel_id !== "") {
      writer.uint32(18).string(message.channel_id);
    }
    writer.uint32(26).fork();
    for (const v of message.packet_ack_sequences) {
      writer.uint64(v);
    }
    writer.ldelim();
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryUnreceivedAcksRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryUnreceivedAcksRequest,
    } as QueryUnreceivedAcksRequest;
    message.packet_ack_sequences = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.port_id = reader.string();
          break;
        case 2:
          message.channel_id = reader.string();
          break;
        case 3:
          if ((tag & 7) === 2) {
            const end2 = reader.uint32() + reader.pos;
            while (reader.pos < end2) {
              message.packet_ack_sequences.push(
                longToNumber(reader.uint64() as Long)
              );
            }
          } else {
            message.packet_ack_sequences.push(
              longToNumber(reader.uint64() as Long)
            );
          }
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryUnreceivedAcksRequest {
    const message = {
      ...baseQueryUnreceivedAcksRequest,
    } as QueryUnreceivedAcksRequest;
    message.packet_ack_sequences = [];
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = String(object.port_id);
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = String(object.channel_id);
    } else {
      message.channel_id = "";
    }
    if (
      object.packet_ack_sequences !== undefined &&
      object.packet_ack_sequences !== null
    ) {
      for (const e of object.packet_ack_sequences) {
        message.packet_ack_sequences.push(Number(e));
      }
    }
    return message;
  },

  toJSON(message: QueryUnreceivedAcksRequest): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    if (message.packet_ack_sequences) {
      obj.packet_ack_sequences = message.packet_ack_sequences.map((e) => e);
    } else {
      obj.packet_ack_sequences = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryUnreceivedAcksRequest>
  ): QueryUnreceivedAcksRequest {
    const message = {
      ...baseQueryUnreceivedAcksRequest,
    } as QueryUnreceivedAcksRequest;
    message.packet_ack_sequences = [];
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = object.port_id;
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = object.channel_id;
    } else {
      message.channel_id = "";
    }
    if (
      object.packet_ack_sequences !== undefined &&
      object.packet_ack_sequences !== null
    ) {
      for (const e of object.packet_ack_sequences) {
        message.packet_ack_sequences.push(e);
      }
    }
    return message;
  },
};

const baseQueryUnreceivedAcksResponse: object = { sequences: 0 };

export const QueryUnreceivedAcksResponse = {
  encode(
    message: QueryUnreceivedAcksResponse,
    writer: Writer = Writer.create()
  ): Writer {
    writer.uint32(10).fork();
    for (const v of message.sequences) {
      writer.uint64(v);
    }
    writer.ldelim();
    if (message.height !== undefined) {
      Height.encode(message.height, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryUnreceivedAcksResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryUnreceivedAcksResponse,
    } as QueryUnreceivedAcksResponse;
    message.sequences = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          if ((tag & 7) === 2) {
            const end2 = reader.uint32() + reader.pos;
            while (reader.pos < end2) {
              message.sequences.push(longToNumber(reader.uint64() as Long));
            }
          } else {
            message.sequences.push(longToNumber(reader.uint64() as Long));
          }
          break;
        case 2:
          message.height = Height.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryUnreceivedAcksResponse {
    const message = {
      ...baseQueryUnreceivedAcksResponse,
    } as QueryUnreceivedAcksResponse;
    message.sequences = [];
    if (object.sequences !== undefined && object.sequences !== null) {
      for (const e of object.sequences) {
        message.sequences.push(Number(e));
      }
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = Height.fromJSON(object.height);
    } else {
      message.height = undefined;
    }
    return message;
  },

  toJSON(message: QueryUnreceivedAcksResponse): unknown {
    const obj: any = {};
    if (message.sequences) {
      obj.sequences = message.sequences.map((e) => e);
    } else {
      obj.sequences = [];
    }
    message.height !== undefined &&
      (obj.height = message.height ? Height.toJSON(message.height) : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryUnreceivedAcksResponse>
  ): QueryUnreceivedAcksResponse {
    const message = {
      ...baseQueryUnreceivedAcksResponse,
    } as QueryUnreceivedAcksResponse;
    message.sequences = [];
    if (object.sequences !== undefined && object.sequences !== null) {
      for (const e of object.sequences) {
        message.sequences.push(e);
      }
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = Height.fromPartial(object.height);
    } else {
      message.height = undefined;
    }
    return message;
  },
};

const baseQueryNextSequenceReceiveRequest: object = {
  port_id: "",
  channel_id: "",
};

export const QueryNextSequenceReceiveRequest = {
  encode(
    message: QueryNextSequenceReceiveRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.channel_id !== "") {
      writer.uint32(18).string(message.channel_id);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryNextSequenceReceiveRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryNextSequenceReceiveRequest,
    } as QueryNextSequenceReceiveRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.port_id = reader.string();
          break;
        case 2:
          message.channel_id = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryNextSequenceReceiveRequest {
    const message = {
      ...baseQueryNextSequenceReceiveRequest,
    } as QueryNextSequenceReceiveRequest;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = String(object.port_id);
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = String(object.channel_id);
    } else {
      message.channel_id = "";
    }
    return message;
  },

  toJSON(message: QueryNextSequenceReceiveRequest): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryNextSequenceReceiveRequest>
  ): QueryNextSequenceReceiveRequest {
    const message = {
      ...baseQueryNextSequenceReceiveRequest,
    } as QueryNextSequenceReceiveRequest;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = object.port_id;
    } else {
      message.port_id = "";
    }
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = object.channel_id;
    } else {
      message.channel_id = "";
    }
    return message;
  },
};

const baseQueryNextSequenceReceiveResponse: object = {
  next_sequence_receive: 0,
};

export const QueryNextSequenceReceiveResponse = {
  encode(
    message: QueryNextSequenceReceiveResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.next_sequence_receive !== 0) {
      writer.uint32(8).uint64(message.next_sequence_receive);
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
  ): QueryNextSequenceReceiveResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryNextSequenceReceiveResponse,
    } as QueryNextSequenceReceiveResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.next_sequence_receive = longToNumber(reader.uint64() as Long);
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

  fromJSON(object: any): QueryNextSequenceReceiveResponse {
    const message = {
      ...baseQueryNextSequenceReceiveResponse,
    } as QueryNextSequenceReceiveResponse;
    if (
      object.next_sequence_receive !== undefined &&
      object.next_sequence_receive !== null
    ) {
      message.next_sequence_receive = Number(object.next_sequence_receive);
    } else {
      message.next_sequence_receive = 0;
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

  toJSON(message: QueryNextSequenceReceiveResponse): unknown {
    const obj: any = {};
    message.next_sequence_receive !== undefined &&
      (obj.next_sequence_receive = message.next_sequence_receive);
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
    object: DeepPartial<QueryNextSequenceReceiveResponse>
  ): QueryNextSequenceReceiveResponse {
    const message = {
      ...baseQueryNextSequenceReceiveResponse,
    } as QueryNextSequenceReceiveResponse;
    if (
      object.next_sequence_receive !== undefined &&
      object.next_sequence_receive !== null
    ) {
      message.next_sequence_receive = object.next_sequence_receive;
    } else {
      message.next_sequence_receive = 0;
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
  /** Channel queries an IBC Channel. */
  Channel(request: QueryChannelRequest): Promise<QueryChannelResponse>;
  /** Channels queries all the IBC channels of a chain. */
  Channels(request: QueryChannelsRequest): Promise<QueryChannelsResponse>;
  /**
   * ConnectionChannels queries all the channels associated with a connection
   * end.
   */
  ConnectionChannels(
    request: QueryConnectionChannelsRequest
  ): Promise<QueryConnectionChannelsResponse>;
  /**
   * ChannelClientState queries for the client state for the channel associated
   * with the provided channel identifiers.
   */
  ChannelClientState(
    request: QueryChannelClientStateRequest
  ): Promise<QueryChannelClientStateResponse>;
  /**
   * ChannelConsensusState queries for the consensus state for the channel
   * associated with the provided channel identifiers.
   */
  ChannelConsensusState(
    request: QueryChannelConsensusStateRequest
  ): Promise<QueryChannelConsensusStateResponse>;
  /** PacketCommitment queries a stored packet commitment hash. */
  PacketCommitment(
    request: QueryPacketCommitmentRequest
  ): Promise<QueryPacketCommitmentResponse>;
  /**
   * PacketCommitments returns all the packet commitments hashes associated
   * with a channel.
   */
  PacketCommitments(
    request: QueryPacketCommitmentsRequest
  ): Promise<QueryPacketCommitmentsResponse>;
  /**
   * PacketReceipt queries if a given packet sequence has been received on the
   * queried chain
   */
  PacketReceipt(
    request: QueryPacketReceiptRequest
  ): Promise<QueryPacketReceiptResponse>;
  /** PacketAcknowledgement queries a stored packet acknowledgement hash. */
  PacketAcknowledgement(
    request: QueryPacketAcknowledgementRequest
  ): Promise<QueryPacketAcknowledgementResponse>;
  /**
   * PacketAcknowledgements returns all the packet acknowledgements associated
   * with a channel.
   */
  PacketAcknowledgements(
    request: QueryPacketAcknowledgementsRequest
  ): Promise<QueryPacketAcknowledgementsResponse>;
  /**
   * UnreceivedPackets returns all the unreceived IBC packets associated with a
   * channel and sequences.
   */
  UnreceivedPackets(
    request: QueryUnreceivedPacketsRequest
  ): Promise<QueryUnreceivedPacketsResponse>;
  /**
   * UnreceivedAcks returns all the unreceived IBC acknowledgements associated
   * with a channel and sequences.
   */
  UnreceivedAcks(
    request: QueryUnreceivedAcksRequest
  ): Promise<QueryUnreceivedAcksResponse>;
  /** NextSequenceReceive returns the next receive sequence for a given channel. */
  NextSequenceReceive(
    request: QueryNextSequenceReceiveRequest
  ): Promise<QueryNextSequenceReceiveResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  Channel(request: QueryChannelRequest): Promise<QueryChannelResponse> {
    const data = QueryChannelRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Query",
      "Channel",
      data
    );
    return promise.then((data) =>
      QueryChannelResponse.decode(new Reader(data))
    );
  }

  Channels(request: QueryChannelsRequest): Promise<QueryChannelsResponse> {
    const data = QueryChannelsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Query",
      "Channels",
      data
    );
    return promise.then((data) =>
      QueryChannelsResponse.decode(new Reader(data))
    );
  }

  ConnectionChannels(
    request: QueryConnectionChannelsRequest
  ): Promise<QueryConnectionChannelsResponse> {
    const data = QueryConnectionChannelsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Query",
      "ConnectionChannels",
      data
    );
    return promise.then((data) =>
      QueryConnectionChannelsResponse.decode(new Reader(data))
    );
  }

  ChannelClientState(
    request: QueryChannelClientStateRequest
  ): Promise<QueryChannelClientStateResponse> {
    const data = QueryChannelClientStateRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Query",
      "ChannelClientState",
      data
    );
    return promise.then((data) =>
      QueryChannelClientStateResponse.decode(new Reader(data))
    );
  }

  ChannelConsensusState(
    request: QueryChannelConsensusStateRequest
  ): Promise<QueryChannelConsensusStateResponse> {
    const data = QueryChannelConsensusStateRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Query",
      "ChannelConsensusState",
      data
    );
    return promise.then((data) =>
      QueryChannelConsensusStateResponse.decode(new Reader(data))
    );
  }

  PacketCommitment(
    request: QueryPacketCommitmentRequest
  ): Promise<QueryPacketCommitmentResponse> {
    const data = QueryPacketCommitmentRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Query",
      "PacketCommitment",
      data
    );
    return promise.then((data) =>
      QueryPacketCommitmentResponse.decode(new Reader(data))
    );
  }

  PacketCommitments(
    request: QueryPacketCommitmentsRequest
  ): Promise<QueryPacketCommitmentsResponse> {
    const data = QueryPacketCommitmentsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Query",
      "PacketCommitments",
      data
    );
    return promise.then((data) =>
      QueryPacketCommitmentsResponse.decode(new Reader(data))
    );
  }

  PacketReceipt(
    request: QueryPacketReceiptRequest
  ): Promise<QueryPacketReceiptResponse> {
    const data = QueryPacketReceiptRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Query",
      "PacketReceipt",
      data
    );
    return promise.then((data) =>
      QueryPacketReceiptResponse.decode(new Reader(data))
    );
  }

  PacketAcknowledgement(
    request: QueryPacketAcknowledgementRequest
  ): Promise<QueryPacketAcknowledgementResponse> {
    const data = QueryPacketAcknowledgementRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Query",
      "PacketAcknowledgement",
      data
    );
    return promise.then((data) =>
      QueryPacketAcknowledgementResponse.decode(new Reader(data))
    );
  }

  PacketAcknowledgements(
    request: QueryPacketAcknowledgementsRequest
  ): Promise<QueryPacketAcknowledgementsResponse> {
    const data = QueryPacketAcknowledgementsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Query",
      "PacketAcknowledgements",
      data
    );
    return promise.then((data) =>
      QueryPacketAcknowledgementsResponse.decode(new Reader(data))
    );
  }

  UnreceivedPackets(
    request: QueryUnreceivedPacketsRequest
  ): Promise<QueryUnreceivedPacketsResponse> {
    const data = QueryUnreceivedPacketsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Query",
      "UnreceivedPackets",
      data
    );
    return promise.then((data) =>
      QueryUnreceivedPacketsResponse.decode(new Reader(data))
    );
  }

  UnreceivedAcks(
    request: QueryUnreceivedAcksRequest
  ): Promise<QueryUnreceivedAcksResponse> {
    const data = QueryUnreceivedAcksRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Query",
      "UnreceivedAcks",
      data
    );
    return promise.then((data) =>
      QueryUnreceivedAcksResponse.decode(new Reader(data))
    );
  }

  NextSequenceReceive(
    request: QueryNextSequenceReceiveRequest
  ): Promise<QueryNextSequenceReceiveResponse> {
    const data = QueryNextSequenceReceiveRequest.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Query",
      "NextSequenceReceive",
      data
    );
    return promise.then((data) =>
      QueryNextSequenceReceiveResponse.decode(new Reader(data))
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
