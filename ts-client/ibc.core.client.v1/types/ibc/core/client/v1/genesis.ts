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
  clientsConsensus: ClientConsensusStates[];
  /** metadata from each client */
  clientsMetadata: IdentifiedGenesisMetadata[];
  params: Params | undefined;
  /** create localhost on initialization */
  createLocalhost: boolean;
  /** the sequence for the next generated client identifier */
  nextClientSequence: number;
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
  clientId: string;
  clientMetadata: GenesisMetadata[];
}

const baseGenesisState: object = {
  createLocalhost: false,
  nextClientSequence: 0,
};

export const GenesisState = {
  encode(message: GenesisState, writer: Writer = Writer.create()): Writer {
    for (const v of message.clients) {
      IdentifiedClientState.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.clientsConsensus) {
      ClientConsensusStates.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.clientsMetadata) {
      IdentifiedGenesisMetadata.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    if (message.params !== undefined) {
      Params.encode(message.params, writer.uint32(34).fork()).ldelim();
    }
    if (message.createLocalhost === true) {
      writer.uint32(40).bool(message.createLocalhost);
    }
    if (message.nextClientSequence !== 0) {
      writer.uint32(48).uint64(message.nextClientSequence);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisState } as GenesisState;
    message.clients = [];
    message.clientsConsensus = [];
    message.clientsMetadata = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.clients.push(
            IdentifiedClientState.decode(reader, reader.uint32())
          );
          break;
        case 2:
          message.clientsConsensus.push(
            ClientConsensusStates.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.clientsMetadata.push(
            IdentifiedGenesisMetadata.decode(reader, reader.uint32())
          );
          break;
        case 4:
          message.params = Params.decode(reader, reader.uint32());
          break;
        case 5:
          message.createLocalhost = reader.bool();
          break;
        case 6:
          message.nextClientSequence = longToNumber(reader.uint64() as Long);
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
    message.clientsConsensus = [];
    message.clientsMetadata = [];
    if (object.clients !== undefined && object.clients !== null) {
      for (const e of object.clients) {
        message.clients.push(IdentifiedClientState.fromJSON(e));
      }
    }
    if (
      object.clientsConsensus !== undefined &&
      object.clientsConsensus !== null
    ) {
      for (const e of object.clientsConsensus) {
        message.clientsConsensus.push(ClientConsensusStates.fromJSON(e));
      }
    }
    if (
      object.clientsMetadata !== undefined &&
      object.clientsMetadata !== null
    ) {
      for (const e of object.clientsMetadata) {
        message.clientsMetadata.push(IdentifiedGenesisMetadata.fromJSON(e));
      }
    }
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromJSON(object.params);
    } else {
      message.params = undefined;
    }
    if (
      object.createLocalhost !== undefined &&
      object.createLocalhost !== null
    ) {
      message.createLocalhost = Boolean(object.createLocalhost);
    } else {
      message.createLocalhost = false;
    }
    if (
      object.nextClientSequence !== undefined &&
      object.nextClientSequence !== null
    ) {
      message.nextClientSequence = Number(object.nextClientSequence);
    } else {
      message.nextClientSequence = 0;
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
    if (message.clientsConsensus) {
      obj.clientsConsensus = message.clientsConsensus.map((e) =>
        e ? ClientConsensusStates.toJSON(e) : undefined
      );
    } else {
      obj.clientsConsensus = [];
    }
    if (message.clientsMetadata) {
      obj.clientsMetadata = message.clientsMetadata.map((e) =>
        e ? IdentifiedGenesisMetadata.toJSON(e) : undefined
      );
    } else {
      obj.clientsMetadata = [];
    }
    message.params !== undefined &&
      (obj.params = message.params ? Params.toJSON(message.params) : undefined);
    message.createLocalhost !== undefined &&
      (obj.createLocalhost = message.createLocalhost);
    message.nextClientSequence !== undefined &&
      (obj.nextClientSequence = message.nextClientSequence);
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisState>): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.clients = [];
    message.clientsConsensus = [];
    message.clientsMetadata = [];
    if (object.clients !== undefined && object.clients !== null) {
      for (const e of object.clients) {
        message.clients.push(IdentifiedClientState.fromPartial(e));
      }
    }
    if (
      object.clientsConsensus !== undefined &&
      object.clientsConsensus !== null
    ) {
      for (const e of object.clientsConsensus) {
        message.clientsConsensus.push(ClientConsensusStates.fromPartial(e));
      }
    }
    if (
      object.clientsMetadata !== undefined &&
      object.clientsMetadata !== null
    ) {
      for (const e of object.clientsMetadata) {
        message.clientsMetadata.push(IdentifiedGenesisMetadata.fromPartial(e));
      }
    }
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromPartial(object.params);
    } else {
      message.params = undefined;
    }
    if (
      object.createLocalhost !== undefined &&
      object.createLocalhost !== null
    ) {
      message.createLocalhost = object.createLocalhost;
    } else {
      message.createLocalhost = false;
    }
    if (
      object.nextClientSequence !== undefined &&
      object.nextClientSequence !== null
    ) {
      message.nextClientSequence = object.nextClientSequence;
    } else {
      message.nextClientSequence = 0;
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

const baseIdentifiedGenesisMetadata: object = { clientId: "" };

export const IdentifiedGenesisMetadata = {
  encode(
    message: IdentifiedGenesisMetadata,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.clientId !== "") {
      writer.uint32(10).string(message.clientId);
    }
    for (const v of message.clientMetadata) {
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
    message.clientMetadata = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.clientId = reader.string();
          break;
        case 2:
          message.clientMetadata.push(
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
    message.clientMetadata = [];
    if (object.clientId !== undefined && object.clientId !== null) {
      message.clientId = String(object.clientId);
    } else {
      message.clientId = "";
    }
    if (object.clientMetadata !== undefined && object.clientMetadata !== null) {
      for (const e of object.clientMetadata) {
        message.clientMetadata.push(GenesisMetadata.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: IdentifiedGenesisMetadata): unknown {
    const obj: any = {};
    message.clientId !== undefined && (obj.clientId = message.clientId);
    if (message.clientMetadata) {
      obj.clientMetadata = message.clientMetadata.map((e) =>
        e ? GenesisMetadata.toJSON(e) : undefined
      );
    } else {
      obj.clientMetadata = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<IdentifiedGenesisMetadata>
  ): IdentifiedGenesisMetadata {
    const message = {
      ...baseIdentifiedGenesisMetadata,
    } as IdentifiedGenesisMetadata;
    message.clientMetadata = [];
    if (object.clientId !== undefined && object.clientId !== null) {
      message.clientId = object.clientId;
    } else {
      message.clientId = "";
    }
    if (object.clientMetadata !== undefined && object.clientMetadata !== null) {
      for (const e of object.clientMetadata) {
        message.clientMetadata.push(GenesisMetadata.fromPartial(e));
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
