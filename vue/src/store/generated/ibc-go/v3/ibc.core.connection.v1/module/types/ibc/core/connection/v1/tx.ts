/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import {
  Counterparty,
  Version,
} from "../../../../ibc/core/connection/v1/connection";
import { Any } from "../../../../google/protobuf/any";
import { Height } from "../../../../ibc/core/client/v1/client";

export const protobufPackage = "ibc.core.connection.v1";

/**
 * MsgConnectionOpenInit defines the msg sent by an account on Chain A to
 * initialize a connection with Chain B.
 */
export interface MsgConnectionOpenInit {
  client_id: string;
  counterparty: Counterparty | undefined;
  version: Version | undefined;
  delay_period: number;
  signer: string;
}

/**
 * MsgConnectionOpenInitResponse defines the Msg/ConnectionOpenInit response
 * type.
 */
export interface MsgConnectionOpenInitResponse {}

/**
 * MsgConnectionOpenTry defines a msg sent by a Relayer to try to open a
 * connection on Chain B.
 */
export interface MsgConnectionOpenTry {
  client_id: string;
  /**
   * in the case of crossing hello's, when both chains call OpenInit, we need
   * the connection identifier of the previous connection in state INIT
   */
  previous_connection_id: string;
  client_state: Any | undefined;
  counterparty: Counterparty | undefined;
  delay_period: number;
  counterparty_versions: Version[];
  proof_height: Height | undefined;
  /**
   * proof of the initialization the connection on Chain A: `UNITIALIZED ->
   * INIT`
   */
  proof_init: Uint8Array;
  /** proof of client state included in message */
  proof_client: Uint8Array;
  /** proof of client consensus state */
  proof_consensus: Uint8Array;
  consensus_height: Height | undefined;
  signer: string;
}

/** MsgConnectionOpenTryResponse defines the Msg/ConnectionOpenTry response type. */
export interface MsgConnectionOpenTryResponse {}

/**
 * MsgConnectionOpenAck defines a msg sent by a Relayer to Chain A to
 * acknowledge the change of connection state to TRYOPEN on Chain B.
 */
export interface MsgConnectionOpenAck {
  connection_id: string;
  counterparty_connection_id: string;
  version: Version | undefined;
  client_state: Any | undefined;
  proof_height: Height | undefined;
  /**
   * proof of the initialization the connection on Chain B: `UNITIALIZED ->
   * TRYOPEN`
   */
  proof_try: Uint8Array;
  /** proof of client state included in message */
  proof_client: Uint8Array;
  /** proof of client consensus state */
  proof_consensus: Uint8Array;
  consensus_height: Height | undefined;
  signer: string;
}

/** MsgConnectionOpenAckResponse defines the Msg/ConnectionOpenAck response type. */
export interface MsgConnectionOpenAckResponse {}

/**
 * MsgConnectionOpenConfirm defines a msg sent by a Relayer to Chain B to
 * acknowledge the change of connection state to OPEN on Chain A.
 */
export interface MsgConnectionOpenConfirm {
  connection_id: string;
  /** proof for the change of the connection state on Chain A: `INIT -> OPEN` */
  proof_ack: Uint8Array;
  proof_height: Height | undefined;
  signer: string;
}

/**
 * MsgConnectionOpenConfirmResponse defines the Msg/ConnectionOpenConfirm
 * response type.
 */
export interface MsgConnectionOpenConfirmResponse {}

const baseMsgConnectionOpenInit: object = {
  client_id: "",
  delay_period: 0,
  signer: "",
};

export const MsgConnectionOpenInit = {
  encode(
    message: MsgConnectionOpenInit,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.client_id !== "") {
      writer.uint32(10).string(message.client_id);
    }
    if (message.counterparty !== undefined) {
      Counterparty.encode(
        message.counterparty,
        writer.uint32(18).fork()
      ).ldelim();
    }
    if (message.version !== undefined) {
      Version.encode(message.version, writer.uint32(26).fork()).ldelim();
    }
    if (message.delay_period !== 0) {
      writer.uint32(32).uint64(message.delay_period);
    }
    if (message.signer !== "") {
      writer.uint32(42).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgConnectionOpenInit {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgConnectionOpenInit } as MsgConnectionOpenInit;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_id = reader.string();
          break;
        case 2:
          message.counterparty = Counterparty.decode(reader, reader.uint32());
          break;
        case 3:
          message.version = Version.decode(reader, reader.uint32());
          break;
        case 4:
          message.delay_period = longToNumber(reader.uint64() as Long);
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

  fromJSON(object: any): MsgConnectionOpenInit {
    const message = { ...baseMsgConnectionOpenInit } as MsgConnectionOpenInit;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
    }
    if (object.counterparty !== undefined && object.counterparty !== null) {
      message.counterparty = Counterparty.fromJSON(object.counterparty);
    } else {
      message.counterparty = undefined;
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = Version.fromJSON(object.version);
    } else {
      message.version = undefined;
    }
    if (object.delay_period !== undefined && object.delay_period !== null) {
      message.delay_period = Number(object.delay_period);
    } else {
      message.delay_period = 0;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgConnectionOpenInit): unknown {
    const obj: any = {};
    message.client_id !== undefined && (obj.client_id = message.client_id);
    message.counterparty !== undefined &&
      (obj.counterparty = message.counterparty
        ? Counterparty.toJSON(message.counterparty)
        : undefined);
    message.version !== undefined &&
      (obj.version = message.version
        ? Version.toJSON(message.version)
        : undefined);
    message.delay_period !== undefined &&
      (obj.delay_period = message.delay_period);
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgConnectionOpenInit>
  ): MsgConnectionOpenInit {
    const message = { ...baseMsgConnectionOpenInit } as MsgConnectionOpenInit;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
    }
    if (object.counterparty !== undefined && object.counterparty !== null) {
      message.counterparty = Counterparty.fromPartial(object.counterparty);
    } else {
      message.counterparty = undefined;
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = Version.fromPartial(object.version);
    } else {
      message.version = undefined;
    }
    if (object.delay_period !== undefined && object.delay_period !== null) {
      message.delay_period = object.delay_period;
    } else {
      message.delay_period = 0;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgConnectionOpenInitResponse: object = {};

export const MsgConnectionOpenInitResponse = {
  encode(
    _: MsgConnectionOpenInitResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgConnectionOpenInitResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgConnectionOpenInitResponse,
    } as MsgConnectionOpenInitResponse;
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

  fromJSON(_: any): MsgConnectionOpenInitResponse {
    const message = {
      ...baseMsgConnectionOpenInitResponse,
    } as MsgConnectionOpenInitResponse;
    return message;
  },

  toJSON(_: MsgConnectionOpenInitResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgConnectionOpenInitResponse>
  ): MsgConnectionOpenInitResponse {
    const message = {
      ...baseMsgConnectionOpenInitResponse,
    } as MsgConnectionOpenInitResponse;
    return message;
  },
};

const baseMsgConnectionOpenTry: object = {
  client_id: "",
  previous_connection_id: "",
  delay_period: 0,
  signer: "",
};

export const MsgConnectionOpenTry = {
  encode(
    message: MsgConnectionOpenTry,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.client_id !== "") {
      writer.uint32(10).string(message.client_id);
    }
    if (message.previous_connection_id !== "") {
      writer.uint32(18).string(message.previous_connection_id);
    }
    if (message.client_state !== undefined) {
      Any.encode(message.client_state, writer.uint32(26).fork()).ldelim();
    }
    if (message.counterparty !== undefined) {
      Counterparty.encode(
        message.counterparty,
        writer.uint32(34).fork()
      ).ldelim();
    }
    if (message.delay_period !== 0) {
      writer.uint32(40).uint64(message.delay_period);
    }
    for (const v of message.counterparty_versions) {
      Version.encode(v!, writer.uint32(50).fork()).ldelim();
    }
    if (message.proof_height !== undefined) {
      Height.encode(message.proof_height, writer.uint32(58).fork()).ldelim();
    }
    if (message.proof_init.length !== 0) {
      writer.uint32(66).bytes(message.proof_init);
    }
    if (message.proof_client.length !== 0) {
      writer.uint32(74).bytes(message.proof_client);
    }
    if (message.proof_consensus.length !== 0) {
      writer.uint32(82).bytes(message.proof_consensus);
    }
    if (message.consensus_height !== undefined) {
      Height.encode(
        message.consensus_height,
        writer.uint32(90).fork()
      ).ldelim();
    }
    if (message.signer !== "") {
      writer.uint32(98).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgConnectionOpenTry {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgConnectionOpenTry } as MsgConnectionOpenTry;
    message.counterparty_versions = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_id = reader.string();
          break;
        case 2:
          message.previous_connection_id = reader.string();
          break;
        case 3:
          message.client_state = Any.decode(reader, reader.uint32());
          break;
        case 4:
          message.counterparty = Counterparty.decode(reader, reader.uint32());
          break;
        case 5:
          message.delay_period = longToNumber(reader.uint64() as Long);
          break;
        case 6:
          message.counterparty_versions.push(
            Version.decode(reader, reader.uint32())
          );
          break;
        case 7:
          message.proof_height = Height.decode(reader, reader.uint32());
          break;
        case 8:
          message.proof_init = reader.bytes();
          break;
        case 9:
          message.proof_client = reader.bytes();
          break;
        case 10:
          message.proof_consensus = reader.bytes();
          break;
        case 11:
          message.consensus_height = Height.decode(reader, reader.uint32());
          break;
        case 12:
          message.signer = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgConnectionOpenTry {
    const message = { ...baseMsgConnectionOpenTry } as MsgConnectionOpenTry;
    message.counterparty_versions = [];
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
    }
    if (
      object.previous_connection_id !== undefined &&
      object.previous_connection_id !== null
    ) {
      message.previous_connection_id = String(object.previous_connection_id);
    } else {
      message.previous_connection_id = "";
    }
    if (object.client_state !== undefined && object.client_state !== null) {
      message.client_state = Any.fromJSON(object.client_state);
    } else {
      message.client_state = undefined;
    }
    if (object.counterparty !== undefined && object.counterparty !== null) {
      message.counterparty = Counterparty.fromJSON(object.counterparty);
    } else {
      message.counterparty = undefined;
    }
    if (object.delay_period !== undefined && object.delay_period !== null) {
      message.delay_period = Number(object.delay_period);
    } else {
      message.delay_period = 0;
    }
    if (
      object.counterparty_versions !== undefined &&
      object.counterparty_versions !== null
    ) {
      for (const e of object.counterparty_versions) {
        message.counterparty_versions.push(Version.fromJSON(e));
      }
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromJSON(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (object.proof_init !== undefined && object.proof_init !== null) {
      message.proof_init = bytesFromBase64(object.proof_init);
    }
    if (object.proof_client !== undefined && object.proof_client !== null) {
      message.proof_client = bytesFromBase64(object.proof_client);
    }
    if (
      object.proof_consensus !== undefined &&
      object.proof_consensus !== null
    ) {
      message.proof_consensus = bytesFromBase64(object.proof_consensus);
    }
    if (
      object.consensus_height !== undefined &&
      object.consensus_height !== null
    ) {
      message.consensus_height = Height.fromJSON(object.consensus_height);
    } else {
      message.consensus_height = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgConnectionOpenTry): unknown {
    const obj: any = {};
    message.client_id !== undefined && (obj.client_id = message.client_id);
    message.previous_connection_id !== undefined &&
      (obj.previous_connection_id = message.previous_connection_id);
    message.client_state !== undefined &&
      (obj.client_state = message.client_state
        ? Any.toJSON(message.client_state)
        : undefined);
    message.counterparty !== undefined &&
      (obj.counterparty = message.counterparty
        ? Counterparty.toJSON(message.counterparty)
        : undefined);
    message.delay_period !== undefined &&
      (obj.delay_period = message.delay_period);
    if (message.counterparty_versions) {
      obj.counterparty_versions = message.counterparty_versions.map((e) =>
        e ? Version.toJSON(e) : undefined
      );
    } else {
      obj.counterparty_versions = [];
    }
    message.proof_height !== undefined &&
      (obj.proof_height = message.proof_height
        ? Height.toJSON(message.proof_height)
        : undefined);
    message.proof_init !== undefined &&
      (obj.proof_init = base64FromBytes(
        message.proof_init !== undefined ? message.proof_init : new Uint8Array()
      ));
    message.proof_client !== undefined &&
      (obj.proof_client = base64FromBytes(
        message.proof_client !== undefined
          ? message.proof_client
          : new Uint8Array()
      ));
    message.proof_consensus !== undefined &&
      (obj.proof_consensus = base64FromBytes(
        message.proof_consensus !== undefined
          ? message.proof_consensus
          : new Uint8Array()
      ));
    message.consensus_height !== undefined &&
      (obj.consensus_height = message.consensus_height
        ? Height.toJSON(message.consensus_height)
        : undefined);
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgConnectionOpenTry>): MsgConnectionOpenTry {
    const message = { ...baseMsgConnectionOpenTry } as MsgConnectionOpenTry;
    message.counterparty_versions = [];
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
    }
    if (
      object.previous_connection_id !== undefined &&
      object.previous_connection_id !== null
    ) {
      message.previous_connection_id = object.previous_connection_id;
    } else {
      message.previous_connection_id = "";
    }
    if (object.client_state !== undefined && object.client_state !== null) {
      message.client_state = Any.fromPartial(object.client_state);
    } else {
      message.client_state = undefined;
    }
    if (object.counterparty !== undefined && object.counterparty !== null) {
      message.counterparty = Counterparty.fromPartial(object.counterparty);
    } else {
      message.counterparty = undefined;
    }
    if (object.delay_period !== undefined && object.delay_period !== null) {
      message.delay_period = object.delay_period;
    } else {
      message.delay_period = 0;
    }
    if (
      object.counterparty_versions !== undefined &&
      object.counterparty_versions !== null
    ) {
      for (const e of object.counterparty_versions) {
        message.counterparty_versions.push(Version.fromPartial(e));
      }
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromPartial(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (object.proof_init !== undefined && object.proof_init !== null) {
      message.proof_init = object.proof_init;
    } else {
      message.proof_init = new Uint8Array();
    }
    if (object.proof_client !== undefined && object.proof_client !== null) {
      message.proof_client = object.proof_client;
    } else {
      message.proof_client = new Uint8Array();
    }
    if (
      object.proof_consensus !== undefined &&
      object.proof_consensus !== null
    ) {
      message.proof_consensus = object.proof_consensus;
    } else {
      message.proof_consensus = new Uint8Array();
    }
    if (
      object.consensus_height !== undefined &&
      object.consensus_height !== null
    ) {
      message.consensus_height = Height.fromPartial(object.consensus_height);
    } else {
      message.consensus_height = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgConnectionOpenTryResponse: object = {};

export const MsgConnectionOpenTryResponse = {
  encode(
    _: MsgConnectionOpenTryResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgConnectionOpenTryResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgConnectionOpenTryResponse,
    } as MsgConnectionOpenTryResponse;
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

  fromJSON(_: any): MsgConnectionOpenTryResponse {
    const message = {
      ...baseMsgConnectionOpenTryResponse,
    } as MsgConnectionOpenTryResponse;
    return message;
  },

  toJSON(_: MsgConnectionOpenTryResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgConnectionOpenTryResponse>
  ): MsgConnectionOpenTryResponse {
    const message = {
      ...baseMsgConnectionOpenTryResponse,
    } as MsgConnectionOpenTryResponse;
    return message;
  },
};

const baseMsgConnectionOpenAck: object = {
  connection_id: "",
  counterparty_connection_id: "",
  signer: "",
};

export const MsgConnectionOpenAck = {
  encode(
    message: MsgConnectionOpenAck,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.connection_id !== "") {
      writer.uint32(10).string(message.connection_id);
    }
    if (message.counterparty_connection_id !== "") {
      writer.uint32(18).string(message.counterparty_connection_id);
    }
    if (message.version !== undefined) {
      Version.encode(message.version, writer.uint32(26).fork()).ldelim();
    }
    if (message.client_state !== undefined) {
      Any.encode(message.client_state, writer.uint32(34).fork()).ldelim();
    }
    if (message.proof_height !== undefined) {
      Height.encode(message.proof_height, writer.uint32(42).fork()).ldelim();
    }
    if (message.proof_try.length !== 0) {
      writer.uint32(50).bytes(message.proof_try);
    }
    if (message.proof_client.length !== 0) {
      writer.uint32(58).bytes(message.proof_client);
    }
    if (message.proof_consensus.length !== 0) {
      writer.uint32(66).bytes(message.proof_consensus);
    }
    if (message.consensus_height !== undefined) {
      Height.encode(
        message.consensus_height,
        writer.uint32(74).fork()
      ).ldelim();
    }
    if (message.signer !== "") {
      writer.uint32(82).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgConnectionOpenAck {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgConnectionOpenAck } as MsgConnectionOpenAck;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.connection_id = reader.string();
          break;
        case 2:
          message.counterparty_connection_id = reader.string();
          break;
        case 3:
          message.version = Version.decode(reader, reader.uint32());
          break;
        case 4:
          message.client_state = Any.decode(reader, reader.uint32());
          break;
        case 5:
          message.proof_height = Height.decode(reader, reader.uint32());
          break;
        case 6:
          message.proof_try = reader.bytes();
          break;
        case 7:
          message.proof_client = reader.bytes();
          break;
        case 8:
          message.proof_consensus = reader.bytes();
          break;
        case 9:
          message.consensus_height = Height.decode(reader, reader.uint32());
          break;
        case 10:
          message.signer = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgConnectionOpenAck {
    const message = { ...baseMsgConnectionOpenAck } as MsgConnectionOpenAck;
    if (object.connection_id !== undefined && object.connection_id !== null) {
      message.connection_id = String(object.connection_id);
    } else {
      message.connection_id = "";
    }
    if (
      object.counterparty_connection_id !== undefined &&
      object.counterparty_connection_id !== null
    ) {
      message.counterparty_connection_id = String(
        object.counterparty_connection_id
      );
    } else {
      message.counterparty_connection_id = "";
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = Version.fromJSON(object.version);
    } else {
      message.version = undefined;
    }
    if (object.client_state !== undefined && object.client_state !== null) {
      message.client_state = Any.fromJSON(object.client_state);
    } else {
      message.client_state = undefined;
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromJSON(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (object.proof_try !== undefined && object.proof_try !== null) {
      message.proof_try = bytesFromBase64(object.proof_try);
    }
    if (object.proof_client !== undefined && object.proof_client !== null) {
      message.proof_client = bytesFromBase64(object.proof_client);
    }
    if (
      object.proof_consensus !== undefined &&
      object.proof_consensus !== null
    ) {
      message.proof_consensus = bytesFromBase64(object.proof_consensus);
    }
    if (
      object.consensus_height !== undefined &&
      object.consensus_height !== null
    ) {
      message.consensus_height = Height.fromJSON(object.consensus_height);
    } else {
      message.consensus_height = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgConnectionOpenAck): unknown {
    const obj: any = {};
    message.connection_id !== undefined &&
      (obj.connection_id = message.connection_id);
    message.counterparty_connection_id !== undefined &&
      (obj.counterparty_connection_id = message.counterparty_connection_id);
    message.version !== undefined &&
      (obj.version = message.version
        ? Version.toJSON(message.version)
        : undefined);
    message.client_state !== undefined &&
      (obj.client_state = message.client_state
        ? Any.toJSON(message.client_state)
        : undefined);
    message.proof_height !== undefined &&
      (obj.proof_height = message.proof_height
        ? Height.toJSON(message.proof_height)
        : undefined);
    message.proof_try !== undefined &&
      (obj.proof_try = base64FromBytes(
        message.proof_try !== undefined ? message.proof_try : new Uint8Array()
      ));
    message.proof_client !== undefined &&
      (obj.proof_client = base64FromBytes(
        message.proof_client !== undefined
          ? message.proof_client
          : new Uint8Array()
      ));
    message.proof_consensus !== undefined &&
      (obj.proof_consensus = base64FromBytes(
        message.proof_consensus !== undefined
          ? message.proof_consensus
          : new Uint8Array()
      ));
    message.consensus_height !== undefined &&
      (obj.consensus_height = message.consensus_height
        ? Height.toJSON(message.consensus_height)
        : undefined);
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgConnectionOpenAck>): MsgConnectionOpenAck {
    const message = { ...baseMsgConnectionOpenAck } as MsgConnectionOpenAck;
    if (object.connection_id !== undefined && object.connection_id !== null) {
      message.connection_id = object.connection_id;
    } else {
      message.connection_id = "";
    }
    if (
      object.counterparty_connection_id !== undefined &&
      object.counterparty_connection_id !== null
    ) {
      message.counterparty_connection_id = object.counterparty_connection_id;
    } else {
      message.counterparty_connection_id = "";
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = Version.fromPartial(object.version);
    } else {
      message.version = undefined;
    }
    if (object.client_state !== undefined && object.client_state !== null) {
      message.client_state = Any.fromPartial(object.client_state);
    } else {
      message.client_state = undefined;
    }
    if (object.proof_height !== undefined && object.proof_height !== null) {
      message.proof_height = Height.fromPartial(object.proof_height);
    } else {
      message.proof_height = undefined;
    }
    if (object.proof_try !== undefined && object.proof_try !== null) {
      message.proof_try = object.proof_try;
    } else {
      message.proof_try = new Uint8Array();
    }
    if (object.proof_client !== undefined && object.proof_client !== null) {
      message.proof_client = object.proof_client;
    } else {
      message.proof_client = new Uint8Array();
    }
    if (
      object.proof_consensus !== undefined &&
      object.proof_consensus !== null
    ) {
      message.proof_consensus = object.proof_consensus;
    } else {
      message.proof_consensus = new Uint8Array();
    }
    if (
      object.consensus_height !== undefined &&
      object.consensus_height !== null
    ) {
      message.consensus_height = Height.fromPartial(object.consensus_height);
    } else {
      message.consensus_height = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgConnectionOpenAckResponse: object = {};

export const MsgConnectionOpenAckResponse = {
  encode(
    _: MsgConnectionOpenAckResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgConnectionOpenAckResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgConnectionOpenAckResponse,
    } as MsgConnectionOpenAckResponse;
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

  fromJSON(_: any): MsgConnectionOpenAckResponse {
    const message = {
      ...baseMsgConnectionOpenAckResponse,
    } as MsgConnectionOpenAckResponse;
    return message;
  },

  toJSON(_: MsgConnectionOpenAckResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgConnectionOpenAckResponse>
  ): MsgConnectionOpenAckResponse {
    const message = {
      ...baseMsgConnectionOpenAckResponse,
    } as MsgConnectionOpenAckResponse;
    return message;
  },
};

const baseMsgConnectionOpenConfirm: object = { connection_id: "", signer: "" };

export const MsgConnectionOpenConfirm = {
  encode(
    message: MsgConnectionOpenConfirm,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.connection_id !== "") {
      writer.uint32(10).string(message.connection_id);
    }
    if (message.proof_ack.length !== 0) {
      writer.uint32(18).bytes(message.proof_ack);
    }
    if (message.proof_height !== undefined) {
      Height.encode(message.proof_height, writer.uint32(26).fork()).ldelim();
    }
    if (message.signer !== "") {
      writer.uint32(34).string(message.signer);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgConnectionOpenConfirm {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgConnectionOpenConfirm,
    } as MsgConnectionOpenConfirm;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.connection_id = reader.string();
          break;
        case 2:
          message.proof_ack = reader.bytes();
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

  fromJSON(object: any): MsgConnectionOpenConfirm {
    const message = {
      ...baseMsgConnectionOpenConfirm,
    } as MsgConnectionOpenConfirm;
    if (object.connection_id !== undefined && object.connection_id !== null) {
      message.connection_id = String(object.connection_id);
    } else {
      message.connection_id = "";
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

  toJSON(message: MsgConnectionOpenConfirm): unknown {
    const obj: any = {};
    message.connection_id !== undefined &&
      (obj.connection_id = message.connection_id);
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
    object: DeepPartial<MsgConnectionOpenConfirm>
  ): MsgConnectionOpenConfirm {
    const message = {
      ...baseMsgConnectionOpenConfirm,
    } as MsgConnectionOpenConfirm;
    if (object.connection_id !== undefined && object.connection_id !== null) {
      message.connection_id = object.connection_id;
    } else {
      message.connection_id = "";
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

const baseMsgConnectionOpenConfirmResponse: object = {};

export const MsgConnectionOpenConfirmResponse = {
  encode(
    _: MsgConnectionOpenConfirmResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgConnectionOpenConfirmResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgConnectionOpenConfirmResponse,
    } as MsgConnectionOpenConfirmResponse;
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

  fromJSON(_: any): MsgConnectionOpenConfirmResponse {
    const message = {
      ...baseMsgConnectionOpenConfirmResponse,
    } as MsgConnectionOpenConfirmResponse;
    return message;
  },

  toJSON(_: MsgConnectionOpenConfirmResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgConnectionOpenConfirmResponse>
  ): MsgConnectionOpenConfirmResponse {
    const message = {
      ...baseMsgConnectionOpenConfirmResponse,
    } as MsgConnectionOpenConfirmResponse;
    return message;
  },
};

/** Msg defines the ibc/connection Msg service. */
export interface Msg {
  /** ConnectionOpenInit defines a rpc handler method for MsgConnectionOpenInit. */
  ConnectionOpenInit(
    request: MsgConnectionOpenInit
  ): Promise<MsgConnectionOpenInitResponse>;
  /** ConnectionOpenTry defines a rpc handler method for MsgConnectionOpenTry. */
  ConnectionOpenTry(
    request: MsgConnectionOpenTry
  ): Promise<MsgConnectionOpenTryResponse>;
  /** ConnectionOpenAck defines a rpc handler method for MsgConnectionOpenAck. */
  ConnectionOpenAck(
    request: MsgConnectionOpenAck
  ): Promise<MsgConnectionOpenAckResponse>;
  /**
   * ConnectionOpenConfirm defines a rpc handler method for
   * MsgConnectionOpenConfirm.
   */
  ConnectionOpenConfirm(
    request: MsgConnectionOpenConfirm
  ): Promise<MsgConnectionOpenConfirmResponse>;
}

export class MsgClientImpl implements Msg {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  ConnectionOpenInit(
    request: MsgConnectionOpenInit
  ): Promise<MsgConnectionOpenInitResponse> {
    const data = MsgConnectionOpenInit.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.connection.v1.Msg",
      "ConnectionOpenInit",
      data
    );
    return promise.then((data) =>
      MsgConnectionOpenInitResponse.decode(new Reader(data))
    );
  }

  ConnectionOpenTry(
    request: MsgConnectionOpenTry
  ): Promise<MsgConnectionOpenTryResponse> {
    const data = MsgConnectionOpenTry.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.connection.v1.Msg",
      "ConnectionOpenTry",
      data
    );
    return promise.then((data) =>
      MsgConnectionOpenTryResponse.decode(new Reader(data))
    );
  }

  ConnectionOpenAck(
    request: MsgConnectionOpenAck
  ): Promise<MsgConnectionOpenAckResponse> {
    const data = MsgConnectionOpenAck.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.connection.v1.Msg",
      "ConnectionOpenAck",
      data
    );
    return promise.then((data) =>
      MsgConnectionOpenAckResponse.decode(new Reader(data))
    );
  }

  ConnectionOpenConfirm(
    request: MsgConnectionOpenConfirm
  ): Promise<MsgConnectionOpenConfirmResponse> {
    const data = MsgConnectionOpenConfirm.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.connection.v1.Msg",
      "ConnectionOpenConfirm",
      data
    );
    return promise.then((data) =>
      MsgConnectionOpenConfirmResponse.decode(new Reader(data))
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
