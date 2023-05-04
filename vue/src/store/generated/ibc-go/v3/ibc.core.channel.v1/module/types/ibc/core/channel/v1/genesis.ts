/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import {
  IdentifiedChannel,
  PacketState,
} from "../../../../ibc/core/channel/v1/channel";

export const protobufPackage = "ibc.core.channel.v1";

/** GenesisState defines the ibc channel submodule's genesis state. */
export interface GenesisState {
  channels: IdentifiedChannel[];
  acknowledgements: PacketState[];
  commitments: PacketState[];
  receipts: PacketState[];
  send_sequences: PacketSequence[];
  recv_sequences: PacketSequence[];
  ack_sequences: PacketSequence[];
  /** the sequence for the next generated channel identifier */
  next_channel_sequence: number;
}

/**
 * PacketSequence defines the genesis type necessary to retrieve and store
 * next send and receive sequences.
 */
export interface PacketSequence {
  port_id: string;
  channel_id: string;
  sequence: number;
}

const baseGenesisState: object = { next_channel_sequence: 0 };

export const GenesisState = {
  encode(message: GenesisState, writer: Writer = Writer.create()): Writer {
    for (const v of message.channels) {
      IdentifiedChannel.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.acknowledgements) {
      PacketState.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.commitments) {
      PacketState.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    for (const v of message.receipts) {
      PacketState.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    for (const v of message.send_sequences) {
      PacketSequence.encode(v!, writer.uint32(42).fork()).ldelim();
    }
    for (const v of message.recv_sequences) {
      PacketSequence.encode(v!, writer.uint32(50).fork()).ldelim();
    }
    for (const v of message.ack_sequences) {
      PacketSequence.encode(v!, writer.uint32(58).fork()).ldelim();
    }
    if (message.next_channel_sequence !== 0) {
      writer.uint32(64).uint64(message.next_channel_sequence);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisState } as GenesisState;
    message.channels = [];
    message.acknowledgements = [];
    message.commitments = [];
    message.receipts = [];
    message.send_sequences = [];
    message.recv_sequences = [];
    message.ack_sequences = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.channels.push(
            IdentifiedChannel.decode(reader, reader.uint32())
          );
          break;
        case 2:
          message.acknowledgements.push(
            PacketState.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.commitments.push(PacketState.decode(reader, reader.uint32()));
          break;
        case 4:
          message.receipts.push(PacketState.decode(reader, reader.uint32()));
          break;
        case 5:
          message.send_sequences.push(
            PacketSequence.decode(reader, reader.uint32())
          );
          break;
        case 6:
          message.recv_sequences.push(
            PacketSequence.decode(reader, reader.uint32())
          );
          break;
        case 7:
          message.ack_sequences.push(
            PacketSequence.decode(reader, reader.uint32())
          );
          break;
        case 8:
          message.next_channel_sequence = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.channels = [];
    message.acknowledgements = [];
    message.commitments = [];
    message.receipts = [];
    message.send_sequences = [];
    message.recv_sequences = [];
    message.ack_sequences = [];
    if (object.channels !== undefined && object.channels !== null) {
      for (const e of object.channels) {
        message.channels.push(IdentifiedChannel.fromJSON(e));
      }
    }
    if (
      object.acknowledgements !== undefined &&
      object.acknowledgements !== null
    ) {
      for (const e of object.acknowledgements) {
        message.acknowledgements.push(PacketState.fromJSON(e));
      }
    }
    if (object.commitments !== undefined && object.commitments !== null) {
      for (const e of object.commitments) {
        message.commitments.push(PacketState.fromJSON(e));
      }
    }
    if (object.receipts !== undefined && object.receipts !== null) {
      for (const e of object.receipts) {
        message.receipts.push(PacketState.fromJSON(e));
      }
    }
    if (object.send_sequences !== undefined && object.send_sequences !== null) {
      for (const e of object.send_sequences) {
        message.send_sequences.push(PacketSequence.fromJSON(e));
      }
    }
    if (object.recv_sequences !== undefined && object.recv_sequences !== null) {
      for (const e of object.recv_sequences) {
        message.recv_sequences.push(PacketSequence.fromJSON(e));
      }
    }
    if (object.ack_sequences !== undefined && object.ack_sequences !== null) {
      for (const e of object.ack_sequences) {
        message.ack_sequences.push(PacketSequence.fromJSON(e));
      }
    }
    if (
      object.next_channel_sequence !== undefined &&
      object.next_channel_sequence !== null
    ) {
      message.next_channel_sequence = Number(object.next_channel_sequence);
    } else {
      message.next_channel_sequence = 0;
    }
    return message;
  },

  toJSON(message: GenesisState): unknown {
    const obj: any = {};
    if (message.channels) {
      obj.channels = message.channels.map((e) =>
        e ? IdentifiedChannel.toJSON(e) : undefined
      );
    } else {
      obj.channels = [];
    }
    if (message.acknowledgements) {
      obj.acknowledgements = message.acknowledgements.map((e) =>
        e ? PacketState.toJSON(e) : undefined
      );
    } else {
      obj.acknowledgements = [];
    }
    if (message.commitments) {
      obj.commitments = message.commitments.map((e) =>
        e ? PacketState.toJSON(e) : undefined
      );
    } else {
      obj.commitments = [];
    }
    if (message.receipts) {
      obj.receipts = message.receipts.map((e) =>
        e ? PacketState.toJSON(e) : undefined
      );
    } else {
      obj.receipts = [];
    }
    if (message.send_sequences) {
      obj.send_sequences = message.send_sequences.map((e) =>
        e ? PacketSequence.toJSON(e) : undefined
      );
    } else {
      obj.send_sequences = [];
    }
    if (message.recv_sequences) {
      obj.recv_sequences = message.recv_sequences.map((e) =>
        e ? PacketSequence.toJSON(e) : undefined
      );
    } else {
      obj.recv_sequences = [];
    }
    if (message.ack_sequences) {
      obj.ack_sequences = message.ack_sequences.map((e) =>
        e ? PacketSequence.toJSON(e) : undefined
      );
    } else {
      obj.ack_sequences = [];
    }
    message.next_channel_sequence !== undefined &&
      (obj.next_channel_sequence = message.next_channel_sequence);
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisState>): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.channels = [];
    message.acknowledgements = [];
    message.commitments = [];
    message.receipts = [];
    message.send_sequences = [];
    message.recv_sequences = [];
    message.ack_sequences = [];
    if (object.channels !== undefined && object.channels !== null) {
      for (const e of object.channels) {
        message.channels.push(IdentifiedChannel.fromPartial(e));
      }
    }
    if (
      object.acknowledgements !== undefined &&
      object.acknowledgements !== null
    ) {
      for (const e of object.acknowledgements) {
        message.acknowledgements.push(PacketState.fromPartial(e));
      }
    }
    if (object.commitments !== undefined && object.commitments !== null) {
      for (const e of object.commitments) {
        message.commitments.push(PacketState.fromPartial(e));
      }
    }
    if (object.receipts !== undefined && object.receipts !== null) {
      for (const e of object.receipts) {
        message.receipts.push(PacketState.fromPartial(e));
      }
    }
    if (object.send_sequences !== undefined && object.send_sequences !== null) {
      for (const e of object.send_sequences) {
        message.send_sequences.push(PacketSequence.fromPartial(e));
      }
    }
    if (object.recv_sequences !== undefined && object.recv_sequences !== null) {
      for (const e of object.recv_sequences) {
        message.recv_sequences.push(PacketSequence.fromPartial(e));
      }
    }
    if (object.ack_sequences !== undefined && object.ack_sequences !== null) {
      for (const e of object.ack_sequences) {
        message.ack_sequences.push(PacketSequence.fromPartial(e));
      }
    }
    if (
      object.next_channel_sequence !== undefined &&
      object.next_channel_sequence !== null
    ) {
      message.next_channel_sequence = object.next_channel_sequence;
    } else {
      message.next_channel_sequence = 0;
    }
    return message;
  },
};

const basePacketSequence: object = { port_id: "", channel_id: "", sequence: 0 };

export const PacketSequence = {
  encode(message: PacketSequence, writer: Writer = Writer.create()): Writer {
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

  decode(input: Reader | Uint8Array, length?: number): PacketSequence {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePacketSequence } as PacketSequence;
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

  fromJSON(object: any): PacketSequence {
    const message = { ...basePacketSequence } as PacketSequence;
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

  toJSON(message: PacketSequence): unknown {
    const obj: any = {};
    message.port_id !== undefined && (obj.port_id = message.port_id);
    message.channel_id !== undefined && (obj.channel_id = message.channel_id);
    message.sequence !== undefined && (obj.sequence = message.sequence);
    return obj;
  },

  fromPartial(object: DeepPartial<PacketSequence>): PacketSequence {
    const message = { ...basePacketSequence } as PacketSequence;
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

declare var self: any | undefined;
declare var window: any | undefined;
var globalThis: any = (() => {
  if (typeof globalThis !== "undefined") return globalThis;
  if (typeof self !== "undefined") return self;
  if (typeof window !== "undefined") return window;
  if (typeof global !== "undefined") return global;
  throw "Unable to locate global object";
})();

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
