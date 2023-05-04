/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import {
  IdentifiedClientState,
  ClientConsensusStates,
  Params,
} from "../../../../ibc/core/client/v1/client";

export const protobufPackage = "ibc.core.client.v1";

/** GenesisState defines the ibc client submodule's genesis state. */
export interface GenesisState {
  /** client states with their corresponding identifiers */
  clients: IdentifiedClientState[];
  /** consensus states from each client */
  clients_consensus: ClientConsensusStates[];
  /** metadata from each client */
  clients_metadata: IdentifiedGenesisMetadata[];
  params: Params | undefined;
  /** create localhost on initialization */
  create_localhost: boolean;
  /** the sequence for the next generated client identifier */
  next_client_sequence: number;
}

/**
 * GenesisMetadata defines the genesis type for metadata that clients may return
 * with ExportMetadata
 */
export interface GenesisMetadata {
  /** store key of metadata without clientID-prefix */
  key: Uint8Array;
  /** metadata value */
  value: Uint8Array;
}

/**
 * IdentifiedGenesisMetadata has the client metadata with the corresponding
 * client id.
 */
export interface IdentifiedGenesisMetadata {
  client_id: string;
  client_metadata: GenesisMetadata[];
}

const baseGenesisState: object = {
  create_localhost: false,
  next_client_sequence: 0,
};

export const GenesisState = {
  encode(message: GenesisState, writer: Writer = Writer.create()): Writer {
    for (const v of message.clients) {
      IdentifiedClientState.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.clients_consensus) {
      ClientConsensusStates.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.clients_metadata) {
      IdentifiedGenesisMetadata.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    if (message.params !== undefined) {
      Params.encode(message.params, writer.uint32(34).fork()).ldelim();
    }
    if (message.create_localhost === true) {
      writer.uint32(40).bool(message.create_localhost);
    }
    if (message.next_client_sequence !== 0) {
      writer.uint32(48).uint64(message.next_client_sequence);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisState } as GenesisState;
    message.clients = [];
    message.clients_consensus = [];
    message.clients_metadata = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.clients.push(
            IdentifiedClientState.decode(reader, reader.uint32())
          );
          break;
        case 2:
          message.clients_consensus.push(
            ClientConsensusStates.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.clients_metadata.push(
            IdentifiedGenesisMetadata.decode(reader, reader.uint32())
          );
          break;
        case 4:
          message.params = Params.decode(reader, reader.uint32());
          break;
        case 5:
          message.create_localhost = reader.bool();
          break;
        case 6:
          message.next_client_sequence = longToNumber(reader.uint64() as Long);
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
    message.clients = [];
    message.clients_consensus = [];
    message.clients_metadata = [];
    if (object.clients !== undefined && object.clients !== null) {
      for (const e of object.clients) {
        message.clients.push(IdentifiedClientState.fromJSON(e));
      }
    }
    if (
      object.clients_consensus !== undefined &&
      object.clients_consensus !== null
    ) {
      for (const e of object.clients_consensus) {
        message.clients_consensus.push(ClientConsensusStates.fromJSON(e));
      }
    }
    if (
      object.clients_metadata !== undefined &&
      object.clients_metadata !== null
    ) {
      for (const e of object.clients_metadata) {
        message.clients_metadata.push(IdentifiedGenesisMetadata.fromJSON(e));
      }
    }
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromJSON(object.params);
    } else {
      message.params = undefined;
    }
    if (
      object.create_localhost !== undefined &&
      object.create_localhost !== null
    ) {
      message.create_localhost = Boolean(object.create_localhost);
    } else {
      message.create_localhost = false;
    }
    if (
      object.next_client_sequence !== undefined &&
      object.next_client_sequence !== null
    ) {
      message.next_client_sequence = Number(object.next_client_sequence);
    } else {
      message.next_client_sequence = 0;
    }
    return message;
  },

  toJSON(message: GenesisState): unknown {
    const obj: any = {};
    if (message.clients) {
      obj.clients = message.clients.map((e) =>
        e ? IdentifiedClientState.toJSON(e) : undefined
      );
    } else {
      obj.clients = [];
    }
    if (message.clients_consensus) {
      obj.clients_consensus = message.clients_consensus.map((e) =>
        e ? ClientConsensusStates.toJSON(e) : undefined
      );
    } else {
      obj.clients_consensus = [];
    }
    if (message.clients_metadata) {
      obj.clients_metadata = message.clients_metadata.map((e) =>
        e ? IdentifiedGenesisMetadata.toJSON(e) : undefined
      );
    } else {
      obj.clients_metadata = [];
    }
    message.params !== undefined &&
      (obj.params = message.params ? Params.toJSON(message.params) : undefined);
    message.create_localhost !== undefined &&
      (obj.create_localhost = message.create_localhost);
    message.next_client_sequence !== undefined &&
      (obj.next_client_sequence = message.next_client_sequence);
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisState>): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.clients = [];
    message.clients_consensus = [];
    message.clients_metadata = [];
    if (object.clients !== undefined && object.clients !== null) {
      for (const e of object.clients) {
        message.clients.push(IdentifiedClientState.fromPartial(e));
      }
    }
    if (
      object.clients_consensus !== undefined &&
      object.clients_consensus !== null
    ) {
      for (const e of object.clients_consensus) {
        message.clients_consensus.push(ClientConsensusStates.fromPartial(e));
      }
    }
    if (
      object.clients_metadata !== undefined &&
      object.clients_metadata !== null
    ) {
      for (const e of object.clients_metadata) {
        message.clients_metadata.push(IdentifiedGenesisMetadata.fromPartial(e));
      }
    }
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromPartial(object.params);
    } else {
      message.params = undefined;
    }
    if (
      object.create_localhost !== undefined &&
      object.create_localhost !== null
    ) {
      message.create_localhost = object.create_localhost;
    } else {
      message.create_localhost = false;
    }
    if (
      object.next_client_sequence !== undefined &&
      object.next_client_sequence !== null
    ) {
      message.next_client_sequence = object.next_client_sequence;
    } else {
      message.next_client_sequence = 0;
    }
    return message;
  },
};

const baseGenesisMetadata: object = {};

export const GenesisMetadata = {
  encode(message: GenesisMetadata, writer: Writer = Writer.create()): Writer {
    if (message.key.length !== 0) {
      writer.uint32(10).bytes(message.key);
    }
    if (message.value.length !== 0) {
      writer.uint32(18).bytes(message.value);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisMetadata {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisMetadata } as GenesisMetadata;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.key = reader.bytes();
          break;
        case 2:
          message.value = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): GenesisMetadata {
    const message = { ...baseGenesisMetadata } as GenesisMetadata;
    if (object.key !== undefined && object.key !== null) {
      message.key = bytesFromBase64(object.key);
    }
    if (object.value !== undefined && object.value !== null) {
      message.value = bytesFromBase64(object.value);
    }
    return message;
  },

  toJSON(message: GenesisMetadata): unknown {
    const obj: any = {};
    message.key !== undefined &&
      (obj.key = base64FromBytes(
        message.key !== undefined ? message.key : new Uint8Array()
      ));
    message.value !== undefined &&
      (obj.value = base64FromBytes(
        message.value !== undefined ? message.value : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisMetadata>): GenesisMetadata {
    const message = { ...baseGenesisMetadata } as GenesisMetadata;
    if (object.key !== undefined && object.key !== null) {
      message.key = object.key;
    } else {
      message.key = new Uint8Array();
    }
    if (object.value !== undefined && object.value !== null) {
      message.value = object.value;
    } else {
      message.value = new Uint8Array();
    }
    return message;
  },
};

const baseIdentifiedGenesisMetadata: object = { client_id: "" };

export const IdentifiedGenesisMetadata = {
  encode(
    message: IdentifiedGenesisMetadata,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.client_id !== "") {
      writer.uint32(10).string(message.client_id);
    }
    for (const v of message.client_metadata) {
      GenesisMetadata.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): IdentifiedGenesisMetadata {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseIdentifiedGenesisMetadata,
    } as IdentifiedGenesisMetadata;
    message.client_metadata = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_id = reader.string();
          break;
        case 2:
          message.client_metadata.push(
            GenesisMetadata.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): IdentifiedGenesisMetadata {
    const message = {
      ...baseIdentifiedGenesisMetadata,
    } as IdentifiedGenesisMetadata;
    message.client_metadata = [];
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
    }
    if (
      object.client_metadata !== undefined &&
      object.client_metadata !== null
    ) {
      for (const e of object.client_metadata) {
        message.client_metadata.push(GenesisMetadata.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: IdentifiedGenesisMetadata): unknown {
    const obj: any = {};
    message.client_id !== undefined && (obj.client_id = message.client_id);
    if (message.client_metadata) {
      obj.client_metadata = message.client_metadata.map((e) =>
        e ? GenesisMetadata.toJSON(e) : undefined
      );
    } else {
      obj.client_metadata = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<IdentifiedGenesisMetadata>
  ): IdentifiedGenesisMetadata {
    const message = {
      ...baseIdentifiedGenesisMetadata,
    } as IdentifiedGenesisMetadata;
    message.client_metadata = [];
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
    }
    if (
      object.client_metadata !== undefined &&
      object.client_metadata !== null
    ) {
      for (const e of object.client_metadata) {
        message.client_metadata.push(GenesisMetadata.fromPartial(e));
      }
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
