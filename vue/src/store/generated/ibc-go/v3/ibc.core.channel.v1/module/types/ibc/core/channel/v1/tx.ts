/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import { Channel, Packet } from "../../../../ibc/core/channel/v1/channel";
import { Height } from "../../../../ibc/core/client/v1/client";

export const protobufPackage = "ibc.core.channel.v1";

/** ResponseResultType defines the possible outcomes of the execution of a message */
export enum ResponseResultType {
  /** RESPONSE_RESULT_UNSPECIFIED - Default zero value enumeration */
  RESPONSE_RESULT_UNSPECIFIED = 0,
  /** RESPONSE_RESULT_NOOP - The message did not call the IBC application callbacks (because, for example, the packet had already been relayed) */
  RESPONSE_RESULT_NOOP = 1,
  /** RESPONSE_RESULT_SUCCESS - The message was executed successfully */
  RESPONSE_RESULT_SUCCESS = 2,
  UNRECOGNIZED = -1,
}

export function responseResultTypeFromJSON(object: any): ResponseResultType {
  switch (object) {
    case 0:
    case "RESPONSE_RESULT_UNSPECIFIED":
      return ResponseResultType.RESPONSE_RESULT_UNSPECIFIED;
    case 1:
    case "RESPONSE_RESULT_NOOP":
      return ResponseResultType.RESPONSE_RESULT_NOOP;
    case 2:
    case "RESPONSE_RESULT_SUCCESS":
      return ResponseResultType.RESPONSE_RESULT_SUCCESS;
    case -1:
    case "UNRECOGNIZED":
    default:
      return ResponseResultType.UNRECOGNIZED;
  }
}

export function responseResultTypeToJSON(object: ResponseResultType): string {
  switch (object) {
    case ResponseResultType.RESPONSE_RESULT_UNSPECIFIED:
      return "RESPONSE_RESULT_UNSPECIFIED";
    case ResponseResultType.RESPONSE_RESULT_NOOP:
      return "RESPONSE_RESULT_NOOP";
    case ResponseResultType.RESPONSE_RESULT_SUCCESS:
      return "RESPONSE_RESULT_SUCCESS";
    default:
      return "UNKNOWN";
  }
}

/**
 * MsgChannelOpenInit defines an sdk.Msg to initialize a channel handshake. It
 * is called by a relayer on Chain A.
 */
export interface MsgChannelOpenInit {
  port_id: string;
  channel: Channel | undefined;
  signer: string;
}

/** MsgChannelOpenInitResponse defines the Msg/ChannelOpenInit response type. */
export interface MsgChannelOpenInitResponse {
  channel_id: string;
}

/**
 * MsgChannelOpenInit defines a msg sent by a Relayer to try to open a channel
 * on Chain B. The version field within the Channel field has been deprecated. Its
 * value will be ignored by core IBC.
 */
export interface MsgChannelOpenTry {
  port_id: string;
  /**
   * in the case of crossing hello's, when both chains call OpenInit, we need
   * the channel identifier of the previous channel in state INIT
   */
  previous_channel_id: string;
  /** NOTE: the version field within the channel has been deprecated. Its value will be ignored by core IBC. */
  channel: Channel | undefined;
  counterparty_version: string;
  proof_init: Uint8Array;
  proof_height: Height | undefined;
  signer: string;
}

/** MsgChannelOpenTryResponse defines the Msg/ChannelOpenTry response type. */
export interface MsgChannelOpenTryResponse {}

/**
 * MsgChannelOpenAck defines a msg sent by a Relayer to Chain A to acknowledge
 * the change of channel state to TRYOPEN on Chain B.
 */
export interface MsgChannelOpenAck {
  port_id: string;
  channel_id: string;
  counterparty_channel_id: string;
  counterparty_version: string;
  proof_try: Uint8Array;
  proof_height: Height | undefined;
  signer: string;
}

/** MsgChannelOpenAckResponse defines the Msg/ChannelOpenAck response type. */
export interface MsgChannelOpenAckResponse {}

/**
 * MsgChannelOpenConfirm defines a msg sent by a Relayer to Chain B to
 * acknowledge the change of channel state to OPEN on Chain A.
 */
export interface MsgChannelOpenConfirm {
  port_id: string;
  channel_id: string;
  proof_ack: Uint8Array;
  proof_height: Height | undefined;
  signer: string;
}

/**
 * MsgChannelOpenConfirmResponse defines the Msg/ChannelOpenConfirm response
 * type.
 */
export interface MsgChannelOpenConfirmResponse {}

/**
 * MsgChannelCloseInit defines a msg sent by a Relayer to Chain A
 * to close a channel with Chain B.
 */
export interface MsgChannelCloseInit {
  port_id: string;
  channel_id: string;
  signer: string;
}

/** MsgChannelCloseInitResponse defines the Msg/ChannelCloseInit response type. */
export interface MsgChannelCloseInitResponse {}

/**
 * MsgChannelCloseConfirm defines a msg sent by a Relayer to Chain B
 * to acknowledge the change of channel state to CLOSED on Chain A.
 */
export interface MsgChannelCloseConfirm {
  port_id: string;
  channel_id: string;
  proof_init: Uint8Array;
  proof_height: Height | undefined;
  signer: string;
}

/**
 * MsgChannelCloseConfirmResponse defines the Msg/ChannelCloseConfirm response
 * type.
 */
export interface MsgChannelCloseConfirmResponse {}

/** MsgRecvPacket receives incoming IBC packet */
export interface MsgRecvPacket {
  packet: Packet | undefined;
  proof_commitment: Uint8Array;
  proof_height: Height | undefined;
  signer: string;
}

/** MsgRecvPacketResponse defines the Msg/RecvPacket response type. */
export interface MsgRecvPacketResponse {
  result: ResponseResultType;
}

/** MsgTimeout receives timed-out packet */
export interface MsgTimeout {
  packet: Packet | undefined;
  proof_unreceived: Uint8Array;
  proof_height: Height | undefined;
  next_sequence_recv: number;
  signer: string;
}

/** MsgTimeoutResponse defines the Msg/Timeout response type. */
export interface MsgTimeoutResponse {
  result: ResponseResultType;
}

/** MsgTimeoutOnClose timed-out packet upon counterparty channel closure. */
export interface MsgTimeoutOnClose {
  packet: Packet | undefined;
  proof_unreceived: Uint8Array;
  proof_close: Uint8Array;
  proof_height: Height | undefined;
  next_sequence_recv: number;
  signer: string;
}

/** MsgTimeoutOnCloseResponse defines the Msg/TimeoutOnClose response type. */
export interface MsgTimeoutOnCloseResponse {
  result: ResponseResultType;
}

/** MsgAcknowledgement receives incoming IBC acknowledgement */
export interface MsgAcknowledgement {
  packet: Packet | undefined;
  acknowledgement: Uint8Array;
  proof_acked: Uint8Array;
  proof_height: Height | undefined;
  signer: string;
}

/** MsgAcknowledgementResponse defines the Msg/Acknowledgement response type. */
export interface MsgAcknowledgementResponse {
  result: ResponseResultType;
}

const baseMsgChannelOpenInit: object = { port_id: "", signer: "" };

export const MsgChannelOpenInit = {
  encode(
    message: MsgChannelOpenInit,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.channel !== undefined) {
      Channel.encode(message.channel, writer.uint32(18).fork()).ldelim();
    }
    if (message.signer !== "") {
      writer.uint32(26).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgChannelOpenInit {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgChannelOpenInit } as MsgChannelOpenInit;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.port_id = reader.string();
          break;
        case 2:
          message.channel = Channel.decode(reader, reader.uint32());
          break;
        case 3:
          message.signer = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgChannelOpenInit {
    const message = { ...baseMsgChannelOpenInit } as MsgChannelOpenInit;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = String(object.port_id);
    } else {
      message.port_id = "";
    }
    if (object.channel !== undefined && object.channel !== null) {
      message.channel = Channel.fromJSON(object.channel);
    } else {
      message.channel = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgChannelOpenInit): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel !== undefined &&
      (obj.channel = message.channel
        ? Channel.toJSON(message.channel)
        : undefined);
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgChannelOpenInit>): MsgChannelOpenInit {
    const message = { ...baseMsgChannelOpenInit } as MsgChannelOpenInit;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = object.port_id;
    } else {
      message.port_id = "";
    }
    if (object.channel !== undefined && object.channel !== null) {
      message.channel = Channel.fromPartial(object.channel);
    } else {
      message.channel = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgChannelOpenInitResponse: object = { channel_id: "" };

export const MsgChannelOpenInitResponse = {
  encode(
    message: MsgChannelOpenInitResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.channel_id !== "") {
      writer.uint32(10).string(message.channel_id);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgChannelOpenInitResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgChannelOpenInitResponse,
    } as MsgChannelOpenInitResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.channel_id = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgChannelOpenInitResponse {
    const message = {
      ...baseMsgChannelOpenInitResponse,
    } as MsgChannelOpenInitResponse;
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = String(object.channel_id);
    } else {
      message.channel_id = "";
    }
    return message;
  },

  toJSON(message: MsgChannelOpenInitResponse): unknown {
    const obj: any = {};
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgChannelOpenInitResponse>
  ): MsgChannelOpenInitResponse {
    const message = {
      ...baseMsgChannelOpenInitResponse,
    } as MsgChannelOpenInitResponse;
    if (object.channel_id !== undefined && object.channel_id !== null) {
      message.channel_id = object.channel_id;
    } else {
      message.channel_id = "";
    }
    return message;
  },
};

const baseMsgChannelOpenTry: object = {
  port_id: "",
  previous_channel_id: "",
  counterparty_version: "",
  signer: "",
};

export const MsgChannelOpenTry = {
  encode(message: MsgChannelOpenTry, writer: Writer = Writer.create()): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.previous_channel_id !== "") {
      writer.uint32(18).string(message.previous_channel_id);
    }
    if (message.channel !== undefined) {
      Channel.encode(message.channel, writer.uint32(26).fork()).ldelim();
    }
    if (message.counterparty_version !== "") {
      writer.uint32(34).string(message.counterparty_version);
    }
    if (message.proof_init.length !== 0) {
      writer.uint32(42).bytes(message.proof_init);
    }
    if (message.proof_height !== undefined) {
      Height.encode(message.proof_height, writer.uint32(50).fork()).ldelim();
    }
    if (message.signer !== "") {
      writer.uint32(58).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgChannelOpenTry {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgChannelOpenTry } as MsgChannelOpenTry;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.port_id = reader.string();
          break;
        case 2:
          message.previous_channel_id = reader.string();
          break;
        case 3:
          message.channel = Channel.decode(reader, reader.uint32());
          break;
        case 4:
          message.counterparty_version = reader.string();
          break;
        case 5:
          message.proof_init = reader.bytes();
          break;
        case 6:
          message.proof_height = Height.decode(reader, reader.uint32());
          break;
        case 7:
          message.signer = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgChannelOpenTry {
    const message = { ...baseMsgChannelOpenTry } as MsgChannelOpenTry;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = String(object.port_id);
    } else {
      message.port_id = "";
    }
    if (
      object.previous_channel_id !== undefined &&
      object.previous_channel_id !== null
    ) {
      message.previous_channel_id = String(object.previous_channel_id);
    } else {
      message.previous_channel_id = "";
    }
    if (object.channel !== undefined && object.channel !== null) {
      message.channel = Channel.fromJSON(object.channel);
    } else {
      message.channel = undefined;
    }
    if (
      object.counterparty_version !== undefined &&
      object.counterparty_version !== null
    ) {
      message.counterparty_version = String(object.counterparty_version);
    } else {
      message.counterparty_version = "";
    }
    if (object.proof_init !== undefined && object.proof_init !== null) {
      message.proof_init = bytesFromBase64(object.proof_init);
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromJSON(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgChannelOpenTry): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.previous_channel_id !== undefined &&
      (obj.previous_channel_id = message.previous_channel_id);
    message.channel !== undefined &&
      (obj.channel = message.channel
        ? Channel.toJSON(message.channel)
        : undefined);
    message.counterparty_version !== undefined &&
      (obj.counterparty_version = message.counterparty_version);
    message.proof_init !== undefined &&
      (obj.proof_init = base64FromBytes(
        message.proof_init !== undefined ? message.proof_init : new Uint8Array()
      ));
    message.proof_height !== undefined &&
      (obj.proof_height = message.proof_height
        ? Height.toJSON(message.proof_height)
        : undefined);
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgChannelOpenTry>): MsgChannelOpenTry {
    const message = { ...baseMsgChannelOpenTry } as MsgChannelOpenTry;
    if (object.port_id !== undefined && object.port_id !== null) {
      message.port_id = object.port_id;
    } else {
      message.port_id = "";
    }
    if (
      object.previous_channel_id !== undefined &&
      object.previous_channel_id !== null
    ) {
      message.previous_channel_id = object.previous_channel_id;
    } else {
      message.previous_channel_id = "";
    }
    if (object.channel !== undefined && object.channel !== null) {
      message.channel = Channel.fromPartial(object.channel);
    } else {
      message.channel = undefined;
    }
    if (
      object.counterparty_version !== undefined &&
      object.counterparty_version !== null
    ) {
      message.counterparty_version = object.counterparty_version;
    } else {
      message.counterparty_version = "";
    }
    if (object.proof_init !== undefined && object.proof_init !== null) {
      message.proof_init = object.proof_init;
    } else {
      message.proof_init = new Uint8Array();
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromPartial(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgChannelOpenTryResponse: object = {};

export const MsgChannelOpenTryResponse = {
  encode(
    _: MsgChannelOpenTryResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgChannelOpenTryResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgChannelOpenTryResponse,
    } as MsgChannelOpenTryResponse;
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

  fromJSON(_: any): MsgChannelOpenTryResponse {
    const message = {
      ...baseMsgChannelOpenTryResponse,
    } as MsgChannelOpenTryResponse;
    return message;
  },

  toJSON(_: MsgChannelOpenTryResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgChannelOpenTryResponse>
  ): MsgChannelOpenTryResponse {
    const message = {
      ...baseMsgChannelOpenTryResponse,
    } as MsgChannelOpenTryResponse;
    return message;
  },
};

const baseMsgChannelOpenAck: object = {
  port_id: "",
  channel_id: "",
  counterparty_channel_id: "",
  counterparty_version: "",
  signer: "",
};

export const MsgChannelOpenAck = {
  encode(message: MsgChannelOpenAck, writer: Writer = Writer.create()): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.channel_id !== "") {
      writer.uint32(18).string(message.channel_id);
    }
    if (message.counterparty_channel_id !== "") {
      writer.uint32(26).string(message.counterparty_channel_id);
    }
    if (message.counterparty_version !== "") {
      writer.uint32(34).string(message.counterparty_version);
    }
    if (message.proof_try.length !== 0) {
      writer.uint32(42).bytes(message.proof_try);
    }
    if (message.proof_height !== undefined) {
      Height.encode(message.proof_height, writer.uint32(50).fork()).ldelim();
    }
    if (message.signer !== "") {
      writer.uint32(58).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgChannelOpenAck {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgChannelOpenAck } as MsgChannelOpenAck;
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
          message.counterparty_channel_id = reader.string();
          break;
        case 4:
          message.counterparty_version = reader.string();
          break;
        case 5:
          message.proof_try = reader.bytes();
          break;
        case 6:
          message.proof_height = Height.decode(reader, reader.uint32());
          break;
        case 7:
          message.signer = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgChannelOpenAck {
    const message = { ...baseMsgChannelOpenAck } as MsgChannelOpenAck;
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
      object.counterparty_channel_id !== undefined &&
      object.counterparty_channel_id !== null
    ) {
      message.counterparty_channel_id = String(object.counterparty_channel_id);
    } else {
      message.counterparty_channel_id = "";
    }
    if (
      object.counterparty_version !== undefined &&
      object.counterparty_version !== null
    ) {
      message.counterparty_version = String(object.counterparty_version);
    } else {
      message.counterparty_version = "";
    }
    if (object.proof_try !== undefined && object.proof_try !== null) {
      message.proof_try = bytesFromBase64(object.proof_try);
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromJSON(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgChannelOpenAck): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    message.counterparty_channel_id !== undefined &&
      (obj.counterparty_channel_id = message.counterparty_channel_id);
    message.counterparty_version !== undefined &&
      (obj.counterparty_version = message.counterparty_version);
    message.proof_try !== undefined &&
      (obj.proof_try = base64FromBytes(
        message.proof_try !== undefined ? message.proof_try : new Uint8Array()
      ));
    message.proof_height !== undefined &&
      (obj.proof_height = message.proof_height
        ? Height.toJSON(message.proof_height)
        : undefined);
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgChannelOpenAck>): MsgChannelOpenAck {
    const message = { ...baseMsgChannelOpenAck } as MsgChannelOpenAck;
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
      object.counterparty_channel_id !== undefined &&
      object.counterparty_channel_id !== null
    ) {
      message.counterparty_channel_id = object.counterparty_channel_id;
    } else {
      message.counterparty_channel_id = "";
    }
    if (
      object.counterparty_version !== undefined &&
      object.counterparty_version !== null
    ) {
      message.counterparty_version = object.counterparty_version;
    } else {
      message.counterparty_version = "";
    }
    if (object.proof_try !== undefined && object.proof_try !== null) {
      message.proof_try = object.proof_try;
    } else {
      message.proof_try = new Uint8Array();
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromPartial(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgChannelOpenAckResponse: object = {};

export const MsgChannelOpenAckResponse = {
  encode(
    _: MsgChannelOpenAckResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgChannelOpenAckResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgChannelOpenAckResponse,
    } as MsgChannelOpenAckResponse;
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

  fromJSON(_: any): MsgChannelOpenAckResponse {
    const message = {
      ...baseMsgChannelOpenAckResponse,
    } as MsgChannelOpenAckResponse;
    return message;
  },

  toJSON(_: MsgChannelOpenAckResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgChannelOpenAckResponse>
  ): MsgChannelOpenAckResponse {
    const message = {
      ...baseMsgChannelOpenAckResponse,
    } as MsgChannelOpenAckResponse;
    return message;
  },
};

const baseMsgChannelOpenConfirm: object = {
  port_id: "",
  channel_id: "",
  signer: "",
};

export const MsgChannelOpenConfirm = {
  encode(
    message: MsgChannelOpenConfirm,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.channel_id !== "") {
      writer.uint32(18).string(message.channel_id);
    }
    if (message.proof_ack.length !== 0) {
      writer.uint32(26).bytes(message.proof_ack);
    }
    if (message.proof_height !== undefined) {
      Height.encode(message.proof_height, writer.uint32(34).fork()).ldelim();
    }
    if (message.signer !== "") {
      writer.uint32(42).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgChannelOpenConfirm {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgChannelOpenConfirm } as MsgChannelOpenConfirm;
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
          message.proof_ack = reader.bytes();
          break;
        case 4:
          message.proof_height = Height.decode(reader, reader.uint32());
          break;
        case 5:
          message.signer = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgChannelOpenConfirm {
    const message = { ...baseMsgChannelOpenConfirm } as MsgChannelOpenConfirm;
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
    if (object.proof_ack !== undefined && object.proof_ack !== null) {
      message.proof_ack = bytesFromBase64(object.proof_ack);
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromJSON(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgChannelOpenConfirm): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    message.proof_ack !== undefined &&
      (obj.proof_ack = base64FromBytes(
        message.proof_ack !== undefined ? message.proof_ack : new Uint8Array()
      ));
    message.proof_height !== undefined &&
      (obj.proof_height = message.proof_height
        ? Height.toJSON(message.proof_height)
        : undefined);
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgChannelOpenConfirm>
  ): MsgChannelOpenConfirm {
    const message = { ...baseMsgChannelOpenConfirm } as MsgChannelOpenConfirm;
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
    if (object.proof_ack !== undefined && object.proof_ack !== null) {
      message.proof_ack = object.proof_ack;
    } else {
      message.proof_ack = new Uint8Array();
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromPartial(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgChannelOpenConfirmResponse: object = {};

export const MsgChannelOpenConfirmResponse = {
  encode(
    _: MsgChannelOpenConfirmResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgChannelOpenConfirmResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgChannelOpenConfirmResponse,
    } as MsgChannelOpenConfirmResponse;
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

  fromJSON(_: any): MsgChannelOpenConfirmResponse {
    const message = {
      ...baseMsgChannelOpenConfirmResponse,
    } as MsgChannelOpenConfirmResponse;
    return message;
  },

  toJSON(_: MsgChannelOpenConfirmResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgChannelOpenConfirmResponse>
  ): MsgChannelOpenConfirmResponse {
    const message = {
      ...baseMsgChannelOpenConfirmResponse,
    } as MsgChannelOpenConfirmResponse;
    return message;
  },
};

const baseMsgChannelCloseInit: object = {
  port_id: "",
  channel_id: "",
  signer: "",
};

export const MsgChannelCloseInit = {
  encode(
    message: MsgChannelCloseInit,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.channel_id !== "") {
      writer.uint32(18).string(message.channel_id);
    }
    if (message.signer !== "") {
      writer.uint32(26).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgChannelCloseInit {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgChannelCloseInit } as MsgChannelCloseInit;
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
          message.signer = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgChannelCloseInit {
    const message = { ...baseMsgChannelCloseInit } as MsgChannelCloseInit;
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
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgChannelCloseInit): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgChannelCloseInit>): MsgChannelCloseInit {
    const message = { ...baseMsgChannelCloseInit } as MsgChannelCloseInit;
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
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgChannelCloseInitResponse: object = {};

export const MsgChannelCloseInitResponse = {
  encode(
    _: MsgChannelCloseInitResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgChannelCloseInitResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgChannelCloseInitResponse,
    } as MsgChannelCloseInitResponse;
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

  fromJSON(_: any): MsgChannelCloseInitResponse {
    const message = {
      ...baseMsgChannelCloseInitResponse,
    } as MsgChannelCloseInitResponse;
    return message;
  },

  toJSON(_: MsgChannelCloseInitResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgChannelCloseInitResponse>
  ): MsgChannelCloseInitResponse {
    const message = {
      ...baseMsgChannelCloseInitResponse,
    } as MsgChannelCloseInitResponse;
    return message;
  },
};

const baseMsgChannelCloseConfirm: object = {
  port_id: "",
  channel_id: "",
  signer: "",
};

export const MsgChannelCloseConfirm = {
  encode(
    message: MsgChannelCloseConfirm,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.port_id !== "") {
      writer.uint32(10).string(message.port_id);
    }
    if (message.channel_id !== "") {
      writer.uint32(18).string(message.channel_id);
    }
    if (message.proof_init.length !== 0) {
      writer.uint32(26).bytes(message.proof_init);
    }
    if (message.proof_height !== undefined) {
      Height.encode(message.proof_height, writer.uint32(34).fork()).ldelim();
    }
    if (message.signer !== "") {
      writer.uint32(42).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgChannelCloseConfirm {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgChannelCloseConfirm } as MsgChannelCloseConfirm;
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
          message.proof_init = reader.bytes();
          break;
        case 4:
          message.proof_height = Height.decode(reader, reader.uint32());
          break;
        case 5:
          message.signer = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgChannelCloseConfirm {
    const message = { ...baseMsgChannelCloseConfirm } as MsgChannelCloseConfirm;
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
    if (object.proof_init !== undefined && object.proof_init !== null) {
      message.proof_init = bytesFromBase64(object.proof_init);
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromJSON(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgChannelCloseConfirm): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    message.proof_init !== undefined &&
      (obj.proof_init = base64FromBytes(
        message.proof_init !== undefined ? message.proof_init : new Uint8Array()
      ));
    message.proof_height !== undefined &&
      (obj.proof_height = message.proof_height
        ? Height.toJSON(message.proof_height)
        : undefined);
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgChannelCloseConfirm>
  ): MsgChannelCloseConfirm {
    const message = { ...baseMsgChannelCloseConfirm } as MsgChannelCloseConfirm;
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
    if (object.proof_init !== undefined && object.proof_init !== null) {
      message.proof_init = object.proof_init;
    } else {
      message.proof_init = new Uint8Array();
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromPartial(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgChannelCloseConfirmResponse: object = {};

export const MsgChannelCloseConfirmResponse = {
  encode(
    _: MsgChannelCloseConfirmResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgChannelCloseConfirmResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgChannelCloseConfirmResponse,
    } as MsgChannelCloseConfirmResponse;
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

  fromJSON(_: any): MsgChannelCloseConfirmResponse {
    const message = {
      ...baseMsgChannelCloseConfirmResponse,
    } as MsgChannelCloseConfirmResponse;
    return message;
  },

  toJSON(_: MsgChannelCloseConfirmResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgChannelCloseConfirmResponse>
  ): MsgChannelCloseConfirmResponse {
    const message = {
      ...baseMsgChannelCloseConfirmResponse,
    } as MsgChannelCloseConfirmResponse;
    return message;
  },
};

const baseMsgRecvPacket: object = { signer: "" };

export const MsgRecvPacket = {
  encode(message: MsgRecvPacket, writer: Writer = Writer.create()): Writer {
    if (message.packet !== undefined) {
      Packet.encode(message.packet, writer.uint32(10).fork()).ldelim();
    }
    if (message.proof_commitment.length !== 0) {
      writer.uint32(18).bytes(message.proof_commitment);
    }
    if (message.proof_height !== undefined) {
      Height.encode(message.proof_height, writer.uint32(26).fork()).ldelim();
    }
    if (message.signer !== "") {
      writer.uint32(34).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgRecvPacket {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgRecvPacket } as MsgRecvPacket;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.packet = Packet.decode(reader, reader.uint32());
          break;
        case 2:
          message.proof_commitment = reader.bytes();
          break;
        case 3:
          message.proof_height = Height.decode(reader, reader.uint32());
          break;
        case 4:
          message.signer = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgRecvPacket {
    const message = { ...baseMsgRecvPacket } as MsgRecvPacket;
    if (object.packet !== undefined && object.packet !== null) {
      message.packet = Packet.fromJSON(object.packet);
    } else {
      message.packet = undefined;
    }
    if (
      object.proof_commitment !== undefined &&
      object.proof_commitment !== null
    ) {
      message.proof_commitment = bytesFromBase64(object.proof_commitment);
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromJSON(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgRecvPacket): unknown {
    const obj: any = {};
    message.packet !== undefined &&
      (obj.packet = message.packet ? Packet.toJSON(message.packet) : undefined);
    message.proof_commitment !== undefined &&
      (obj.proof_commitment = base64FromBytes(
        message.proof_commitment !== undefined
          ? message.proof_commitment
          : new Uint8Array()
      ));
    message.proof_height !== undefined &&
      (obj.proof_height = message.proof_height
        ? Height.toJSON(message.proof_height)
        : undefined);
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgRecvPacket>): MsgRecvPacket {
    const message = { ...baseMsgRecvPacket } as MsgRecvPacket;
    if (object.packet !== undefined && object.packet !== null) {
      message.packet = Packet.fromPartial(object.packet);
    } else {
      message.packet = undefined;
    }
    if (
      object.proof_commitment !== undefined &&
      object.proof_commitment !== null
    ) {
      message.proof_commitment = object.proof_commitment;
    } else {
      message.proof_commitment = new Uint8Array();
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromPartial(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgRecvPacketResponse: object = { result: 0 };

export const MsgRecvPacketResponse = {
  encode(
    message: MsgRecvPacketResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.result !== 0) {
      writer.uint32(8).int32(message.result);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgRecvPacketResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgRecvPacketResponse } as MsgRecvPacketResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.result = reader.int32() as any;
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgRecvPacketResponse {
    const message = { ...baseMsgRecvPacketResponse } as MsgRecvPacketResponse;
    if (object.result !== undefined && object.result !== null) {
      message.result = responseResultTypeFromJSON(object.result);
    } else {
      message.result = 0;
    }
    return message;
  },

  toJSON(message: MsgRecvPacketResponse): unknown {
    const obj: any = {};
    message.result !== undefined &&
      (obj.result = responseResultTypeToJSON(message.result));
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgRecvPacketResponse>
  ): MsgRecvPacketResponse {
    const message = { ...baseMsgRecvPacketResponse } as MsgRecvPacketResponse;
    if (object.result !== undefined && object.result !== null) {
      message.result = object.result;
    } else {
      message.result = 0;
    }
    return message;
  },
};

const baseMsgTimeout: object = { next_sequence_recv: 0, signer: "" };

export const MsgTimeout = {
  encode(message: MsgTimeout, writer: Writer = Writer.create()): Writer {
    if (message.packet !== undefined) {
      Packet.encode(message.packet, writer.uint32(10).fork()).ldelim();
    }
    if (message.proof_unreceived.length !== 0) {
      writer.uint32(18).bytes(message.proof_unreceived);
    }
    if (message.proof_height !== undefined) {
      Height.encode(message.proof_height, writer.uint32(26).fork()).ldelim();
    }
    if (message.next_sequence_recv !== 0) {
      writer.uint32(32).uint64(message.next_sequence_recv);
    }
    if (message.signer !== "") {
      writer.uint32(42).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgTimeout {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgTimeout } as MsgTimeout;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.packet = Packet.decode(reader, reader.uint32());
          break;
        case 2:
          message.proof_unreceived = reader.bytes();
          break;
        case 3:
          message.proof_height = Height.decode(reader, reader.uint32());
          break;
        case 4:
          message.next_sequence_recv = longToNumber(reader.uint64() as Long);
          break;
        case 5:
          message.signer = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgTimeout {
    const message = { ...baseMsgTimeout } as MsgTimeout;
    if (object.packet !== undefined && object.packet !== null) {
      message.packet = Packet.fromJSON(object.packet);
    } else {
      message.packet = undefined;
    }
    if (
      object.proof_unreceived !== undefined &&
      object.proof_unreceived !== null
    ) {
      message.proof_unreceived = bytesFromBase64(object.proof_unreceived);
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromJSON(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (
      object.next_sequence_recv !== undefined &&
      object.next_sequence_recv !== null
    ) {
      message.next_sequence_recv = Number(object.next_sequence_recv);
    } else {
      message.next_sequence_recv = 0;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgTimeout): unknown {
    const obj: any = {};
    message.packet !== undefined &&
      (obj.packet = message.packet ? Packet.toJSON(message.packet) : undefined);
    message.proof_unreceived !== undefined &&
      (obj.proof_unreceived = base64FromBytes(
        message.proof_unreceived !== undefined
          ? message.proof_unreceived
          : new Uint8Array()
      ));
    message.proof_height !== undefined &&
      (obj.proof_height = message.proof_height
        ? Height.toJSON(message.proof_height)
        : undefined);
    message.next_sequence_recv !== undefined &&
      (obj.next_sequence_recv = message.next_sequence_recv);
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgTimeout>): MsgTimeout {
    const message = { ...baseMsgTimeout } as MsgTimeout;
    if (object.packet !== undefined && object.packet !== null) {
      message.packet = Packet.fromPartial(object.packet);
    } else {
      message.packet = undefined;
    }
    if (
      object.proof_unreceived !== undefined &&
      object.proof_unreceived !== null
    ) {
      message.proof_unreceived = object.proof_unreceived;
    } else {
      message.proof_unreceived = new Uint8Array();
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromPartial(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (
      object.next_sequence_recv !== undefined &&
      object.next_sequence_recv !== null
    ) {
      message.next_sequence_recv = object.next_sequence_recv;
    } else {
      message.next_sequence_recv = 0;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgTimeoutResponse: object = { result: 0 };

export const MsgTimeoutResponse = {
  encode(
    message: MsgTimeoutResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.result !== 0) {
      writer.uint32(8).int32(message.result);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgTimeoutResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgTimeoutResponse } as MsgTimeoutResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.result = reader.int32() as any;
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgTimeoutResponse {
    const message = { ...baseMsgTimeoutResponse } as MsgTimeoutResponse;
    if (object.result !== undefined && object.result !== null) {
      message.result = responseResultTypeFromJSON(object.result);
    } else {
      message.result = 0;
    }
    return message;
  },

  toJSON(message: MsgTimeoutResponse): unknown {
    const obj: any = {};
    message.result !== undefined &&
      (obj.result = responseResultTypeToJSON(message.result));
    return obj;
  },

  fromPartial(object: DeepPartial<MsgTimeoutResponse>): MsgTimeoutResponse {
    const message = { ...baseMsgTimeoutResponse } as MsgTimeoutResponse;
    if (object.result !== undefined && object.result !== null) {
      message.result = object.result;
    } else {
      message.result = 0;
    }
    return message;
  },
};

const baseMsgTimeoutOnClose: object = { next_sequence_recv: 0, signer: "" };

export const MsgTimeoutOnClose = {
  encode(message: MsgTimeoutOnClose, writer: Writer = Writer.create()): Writer {
    if (message.packet !== undefined) {
      Packet.encode(message.packet, writer.uint32(10).fork()).ldelim();
    }
    if (message.proof_unreceived.length !== 0) {
      writer.uint32(18).bytes(message.proof_unreceived);
    }
    if (message.proof_close.length !== 0) {
      writer.uint32(26).bytes(message.proof_close);
    }
    if (message.proof_height !== undefined) {
      Height.encode(message.proof_height, writer.uint32(34).fork()).ldelim();
    }
    if (message.next_sequence_recv !== 0) {
      writer.uint32(40).uint64(message.next_sequence_recv);
    }
    if (message.signer !== "") {
      writer.uint32(50).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgTimeoutOnClose {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgTimeoutOnClose } as MsgTimeoutOnClose;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.packet = Packet.decode(reader, reader.uint32());
          break;
        case 2:
          message.proof_unreceived = reader.bytes();
          break;
        case 3:
          message.proof_close = reader.bytes();
          break;
        case 4:
          message.proof_height = Height.decode(reader, reader.uint32());
          break;
        case 5:
          message.next_sequence_recv = longToNumber(reader.uint64() as Long);
          break;
        case 6:
          message.signer = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgTimeoutOnClose {
    const message = { ...baseMsgTimeoutOnClose } as MsgTimeoutOnClose;
    if (object.packet !== undefined && object.packet !== null) {
      message.packet = Packet.fromJSON(object.packet);
    } else {
      message.packet = undefined;
    }
    if (
      object.proof_unreceived !== undefined &&
      object.proof_unreceived !== null
    ) {
      message.proof_unreceived = bytesFromBase64(object.proof_unreceived);
    }
    if (object.proof_close !== undefined && object.proof_close !== null) {
      message.proof_close = bytesFromBase64(object.proof_close);
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromJSON(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (
      object.next_sequence_recv !== undefined &&
      object.next_sequence_recv !== null
    ) {
      message.next_sequence_recv = Number(object.next_sequence_recv);
    } else {
      message.next_sequence_recv = 0;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgTimeoutOnClose): unknown {
    const obj: any = {};
    message.packet !== undefined &&
      (obj.packet = message.packet ? Packet.toJSON(message.packet) : undefined);
    message.proof_unreceived !== undefined &&
      (obj.proof_unreceived = base64FromBytes(
        message.proof_unreceived !== undefined
          ? message.proof_unreceived
          : new Uint8Array()
      ));
    message.proof_close !== undefined &&
      (obj.proof_close = base64FromBytes(
        message.proof_close !== undefined
          ? message.proof_close
          : new Uint8Array()
      ));
    message.proof_height !== undefined &&
      (obj.proof_height = message.proof_height
        ? Height.toJSON(message.proof_height)
        : undefined);
    message.next_sequence_recv !== undefined &&
      (obj.next_sequence_recv = message.next_sequence_recv);
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgTimeoutOnClose>): MsgTimeoutOnClose {
    const message = { ...baseMsgTimeoutOnClose } as MsgTimeoutOnClose;
    if (object.packet !== undefined && object.packet !== null) {
      message.packet = Packet.fromPartial(object.packet);
    } else {
      message.packet = undefined;
    }
    if (
      object.proof_unreceived !== undefined &&
      object.proof_unreceived !== null
    ) {
      message.proof_unreceived = object.proof_unreceived;
    } else {
      message.proof_unreceived = new Uint8Array();
    }
    if (object.proof_close !== undefined && object.proof_close !== null) {
      message.proof_close = object.proof_close;
    } else {
      message.proof_close = new Uint8Array();
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromPartial(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (
      object.next_sequence_recv !== undefined &&
      object.next_sequence_recv !== null
    ) {
      message.next_sequence_recv = object.next_sequence_recv;
    } else {
      message.next_sequence_recv = 0;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgTimeoutOnCloseResponse: object = { result: 0 };

export const MsgTimeoutOnCloseResponse = {
  encode(
    message: MsgTimeoutOnCloseResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.result !== 0) {
      writer.uint32(8).int32(message.result);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgTimeoutOnCloseResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgTimeoutOnCloseResponse,
    } as MsgTimeoutOnCloseResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.result = reader.int32() as any;
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgTimeoutOnCloseResponse {
    const message = {
      ...baseMsgTimeoutOnCloseResponse,
    } as MsgTimeoutOnCloseResponse;
    if (object.result !== undefined && object.result !== null) {
      message.result = responseResultTypeFromJSON(object.result);
    } else {
      message.result = 0;
    }
    return message;
  },

  toJSON(message: MsgTimeoutOnCloseResponse): unknown {
    const obj: any = {};
    message.result !== undefined &&
      (obj.result = responseResultTypeToJSON(message.result));
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgTimeoutOnCloseResponse>
  ): MsgTimeoutOnCloseResponse {
    const message = {
      ...baseMsgTimeoutOnCloseResponse,
    } as MsgTimeoutOnCloseResponse;
    if (object.result !== undefined && object.result !== null) {
      message.result = object.result;
    } else {
      message.result = 0;
    }
    return message;
  },
};

const baseMsgAcknowledgement: object = { signer: "" };

export const MsgAcknowledgement = {
  encode(
    message: MsgAcknowledgement,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.packet !== undefined) {
      Packet.encode(message.packet, writer.uint32(10).fork()).ldelim();
    }
    if (message.acknowledgement.length !== 0) {
      writer.uint32(18).bytes(message.acknowledgement);
    }
    if (message.proof_acked.length !== 0) {
      writer.uint32(26).bytes(message.proof_acked);
    }
    if (message.proof_height !== undefined) {
      Height.encode(message.proof_height, writer.uint32(34).fork()).ldelim();
    }
    if (message.signer !== "") {
      writer.uint32(42).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgAcknowledgement {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgAcknowledgement } as MsgAcknowledgement;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.packet = Packet.decode(reader, reader.uint32());
          break;
        case 2:
          message.acknowledgement = reader.bytes();
          break;
        case 3:
          message.proof_acked = reader.bytes();
          break;
        case 4:
          message.proof_height = Height.decode(reader, reader.uint32());
          break;
        case 5:
          message.signer = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgAcknowledgement {
    const message = { ...baseMsgAcknowledgement } as MsgAcknowledgement;
    if (object.packet !== undefined && object.packet !== null) {
      message.packet = Packet.fromJSON(object.packet);
    } else {
      message.packet = undefined;
    }
    if (
      object.acknowledgement !== undefined &&
      object.acknowledgement !== null
    ) {
      message.acknowledgement = bytesFromBase64(object.acknowledgement);
    }
    if (object.proof_acked !== undefined && object.proof_acked !== null) {
      message.proof_acked = bytesFromBase64(object.proof_acked);
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromJSON(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgAcknowledgement): unknown {
    const obj: any = {};
    message.packet !== undefined &&
      (obj.packet = message.packet ? Packet.toJSON(message.packet) : undefined);
    message.acknowledgement !== undefined &&
      (obj.acknowledgement = base64FromBytes(
        message.acknowledgement !== undefined
          ? message.acknowledgement
          : new Uint8Array()
      ));
    message.proof_acked !== undefined &&
      (obj.proof_acked = base64FromBytes(
        message.proof_acked !== undefined
          ? message.proof_acked
          : new Uint8Array()
      ));
    message.proof_height !== undefined &&
      (obj.proof_height = message.proof_height
        ? Height.toJSON(message.proof_height)
        : undefined);
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgAcknowledgement>): MsgAcknowledgement {
    const message = { ...baseMsgAcknowledgement } as MsgAcknowledgement;
    if (object.packet !== undefined && object.packet !== null) {
      message.packet = Packet.fromPartial(object.packet);
    } else {
      message.packet = undefined;
    }
    if (
      object.acknowledgement !== undefined &&
      object.acknowledgement !== null
    ) {
      message.acknowledgement = object.acknowledgement;
    } else {
      message.acknowledgement = new Uint8Array();
    }
    if (object.proof_acked !== undefined && object.proof_acked !== null) {
      message.proof_acked = object.proof_acked;
    } else {
      message.proof_acked = new Uint8Array();
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromPartial(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgAcknowledgementResponse: object = { result: 0 };

export const MsgAcknowledgementResponse = {
  encode(
    message: MsgAcknowledgementResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.result !== 0) {
      writer.uint32(8).int32(message.result);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgAcknowledgementResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgAcknowledgementResponse,
    } as MsgAcknowledgementResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.result = reader.int32() as any;
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgAcknowledgementResponse {
    const message = {
      ...baseMsgAcknowledgementResponse,
    } as MsgAcknowledgementResponse;
    if (object.result !== undefined && object.result !== null) {
      message.result = responseResultTypeFromJSON(object.result);
    } else {
      message.result = 0;
    }
    return message;
  },

  toJSON(message: MsgAcknowledgementResponse): unknown {
    const obj: any = {};
    message.result !== undefined &&
      (obj.result = responseResultTypeToJSON(message.result));
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgAcknowledgementResponse>
  ): MsgAcknowledgementResponse {
    const message = {
      ...baseMsgAcknowledgementResponse,
    } as MsgAcknowledgementResponse;
    if (object.result !== undefined && object.result !== null) {
      message.result = object.result;
    } else {
      message.result = 0;
    }
    return message;
  },
};

/** Msg defines the ibc/channel Msg service. */
export interface Msg {
  /** ChannelOpenInit defines a rpc handler method for MsgChannelOpenInit. */
  ChannelOpenInit(
    request: MsgChannelOpenInit
  ): Promise<MsgChannelOpenInitResponse>;
  /** ChannelOpenTry defines a rpc handler method for MsgChannelOpenTry. */
  ChannelOpenTry(
    request: MsgChannelOpenTry
  ): Promise<MsgChannelOpenTryResponse>;
  /** ChannelOpenAck defines a rpc handler method for MsgChannelOpenAck. */
  ChannelOpenAck(
    request: MsgChannelOpenAck
  ): Promise<MsgChannelOpenAckResponse>;
  /** ChannelOpenConfirm defines a rpc handler method for MsgChannelOpenConfirm. */
  ChannelOpenConfirm(
    request: MsgChannelOpenConfirm
  ): Promise<MsgChannelOpenConfirmResponse>;
  /** ChannelCloseInit defines a rpc handler method for MsgChannelCloseInit. */
  ChannelCloseInit(
    request: MsgChannelCloseInit
  ): Promise<MsgChannelCloseInitResponse>;
  /**
   * ChannelCloseConfirm defines a rpc handler method for
   * MsgChannelCloseConfirm.
   */
  ChannelCloseConfirm(
    request: MsgChannelCloseConfirm
  ): Promise<MsgChannelCloseConfirmResponse>;
  /** RecvPacket defines a rpc handler method for MsgRecvPacket. */
  RecvPacket(request: MsgRecvPacket): Promise<MsgRecvPacketResponse>;
  /** Timeout defines a rpc handler method for MsgTimeout. */
  Timeout(request: MsgTimeout): Promise<MsgTimeoutResponse>;
  /** TimeoutOnClose defines a rpc handler method for MsgTimeoutOnClose. */
  TimeoutOnClose(
    request: MsgTimeoutOnClose
  ): Promise<MsgTimeoutOnCloseResponse>;
  /** Acknowledgement defines a rpc handler method for MsgAcknowledgement. */
  Acknowledgement(
    request: MsgAcknowledgement
  ): Promise<MsgAcknowledgementResponse>;
}

export class MsgClientImpl implements Msg {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  ChannelOpenInit(
    request: MsgChannelOpenInit
  ): Promise<MsgChannelOpenInitResponse> {
    const data = MsgChannelOpenInit.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Msg",
      "ChannelOpenInit",
      data
    );
    return promise.then((data) =>
      MsgChannelOpenInitResponse.decode(new Reader(data))
    );
  }

  ChannelOpenTry(
    request: MsgChannelOpenTry
  ): Promise<MsgChannelOpenTryResponse> {
    const data = MsgChannelOpenTry.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Msg",
      "ChannelOpenTry",
      data
    );
    return promise.then((data) =>
      MsgChannelOpenTryResponse.decode(new Reader(data))
    );
  }

  ChannelOpenAck(
    request: MsgChannelOpenAck
  ): Promise<MsgChannelOpenAckResponse> {
    const data = MsgChannelOpenAck.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Msg",
      "ChannelOpenAck",
      data
    );
    return promise.then((data) =>
      MsgChannelOpenAckResponse.decode(new Reader(data))
    );
  }

  ChannelOpenConfirm(
    request: MsgChannelOpenConfirm
  ): Promise<MsgChannelOpenConfirmResponse> {
    const data = MsgChannelOpenConfirm.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Msg",
      "ChannelOpenConfirm",
      data
    );
    return promise.then((data) =>
      MsgChannelOpenConfirmResponse.decode(new Reader(data))
    );
  }

  ChannelCloseInit(
    request: MsgChannelCloseInit
  ): Promise<MsgChannelCloseInitResponse> {
    const data = MsgChannelCloseInit.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Msg",
      "ChannelCloseInit",
      data
    );
    return promise.then((data) =>
      MsgChannelCloseInitResponse.decode(new Reader(data))
    );
  }

  ChannelCloseConfirm(
    request: MsgChannelCloseConfirm
  ): Promise<MsgChannelCloseConfirmResponse> {
    const data = MsgChannelCloseConfirm.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Msg",
      "ChannelCloseConfirm",
      data
    );
    return promise.then((data) =>
      MsgChannelCloseConfirmResponse.decode(new Reader(data))
    );
  }

  RecvPacket(request: MsgRecvPacket): Promise<MsgRecvPacketResponse> {
    const data = MsgRecvPacket.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Msg",
      "RecvPacket",
      data
    );
    return promise.then((data) =>
      MsgRecvPacketResponse.decode(new Reader(data))
    );
  }

  Timeout(request: MsgTimeout): Promise<MsgTimeoutResponse> {
    const data = MsgTimeout.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Msg",
      "Timeout",
      data
    );
    return promise.then((data) => MsgTimeoutResponse.decode(new Reader(data)));
  }

  TimeoutOnClose(
    request: MsgTimeoutOnClose
  ): Promise<MsgTimeoutOnCloseResponse> {
    const data = MsgTimeoutOnClose.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Msg",
      "TimeoutOnClose",
      data
    );
    return promise.then((data) =>
      MsgTimeoutOnCloseResponse.decode(new Reader(data))
    );
  }

  Acknowledgement(
    request: MsgAcknowledgement
  ): Promise<MsgAcknowledgementResponse> {
    const data = MsgAcknowledgement.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.channel.v1.Msg",
      "Acknowledgement",
      data
    );
    return promise.then((data) =>
      MsgAcknowledgementResponse.decode(new Reader(data))
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
