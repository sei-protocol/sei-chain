/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { MerklePrefix } from "../../../../ibc/core/commitment/v1/commitment";

export const protobufPackage = "ibc.core.connection.v1";

/**
 * State defines if a connection is in one of the following states:
 * INIT, TRYOPEN, OPEN or UNINITIALIZED.
 */
export enum State {
  /** STATE_UNINITIALIZED_UNSPECIFIED - Default State */
  STATE_UNINITIALIZED_UNSPECIFIED = 0,
  /** STATE_INIT - A connection end has just started the opening handshake. */
  STATE_INIT = 1,
  /**
   * STATE_TRYOPEN - A connection end has acknowledged the handshake step on the counterparty
   * chain.
   */
  STATE_TRYOPEN = 2,
  /** STATE_OPEN - A connection end has completed the handshake. */
  STATE_OPEN = 3,
  UNRECOGNIZED = -1,
}

export function stateFromJSON(object: any): State {
  switch (object) {
    case 0:
    case "STATE_UNINITIALIZED_UNSPECIFIED":
      return State.STATE_UNINITIALIZED_UNSPECIFIED;
    case 1:
    case "STATE_INIT":
      return State.STATE_INIT;
    case 2:
    case "STATE_TRYOPEN":
      return State.STATE_TRYOPEN;
    case 3:
    case "STATE_OPEN":
      return State.STATE_OPEN;
    case -1:
    case "UNRECOGNIZED":
    default:
      return State.UNRECOGNIZED;
  }
}

export function stateToJSON(object: State): string {
  switch (object) {
    case State.STATE_UNINITIALIZED_UNSPECIFIED:
      return "STATE_UNINITIALIZED_UNSPECIFIED";
    case State.STATE_INIT:
      return "STATE_INIT";
    case State.STATE_TRYOPEN:
      return "STATE_TRYOPEN";
    case State.STATE_OPEN:
      return "STATE_OPEN";
    default:
      return "UNKNOWN";
  }
}

/**
 * ConnectionEnd defines a stateful object on a chain connected to another
 * separate one.
 * NOTE: there must only be 2 defined ConnectionEnds to establish
 * a connection between two chains.
 */
export interface ConnectionEnd {
  /** client associated with this connection. */
  client_id: string;
  /**
   * IBC version which can be utilised to determine encodings or protocols for
   * channels or packets utilising this connection.
   */
  versions: Version[];
  /** current state of the connection end. */
  state: State;
  /** counterparty chain associated with this connection. */
  counterparty: Counterparty | undefined;
  /**
   * delay period that must pass before a consensus state can be used for
   * packet-verification NOTE: delay period logic is only implemented by some
   * clients.
   */
  delay_period: number;
}

/**
 * IdentifiedConnection defines a connection with additional connection
 * identifier field.
 */
export interface IdentifiedConnection {
  /** connection identifier. */
  id: string;
  /** client associated with this connection. */
  client_id: string;
  /**
   * IBC version which can be utilised to determine encodings or protocols for
   * channels or packets utilising this connection
   */
  versions: Version[];
  /** current state of the connection end. */
  state: State;
  /** counterparty chain associated with this connection. */
  counterparty: Counterparty | undefined;
  /** delay period associated with this connection. */
  delay_period: number;
}

/** Counterparty defines the counterparty chain associated with a connection end. */
export interface Counterparty {
  /**
   * identifies the client on the counterparty chain associated with a given
   * connection.
   */
  client_id: string;
  /**
   * identifies the connection end on the counterparty chain associated with a
   * given connection.
   */
  connection_id: string;
  /** commitment merkle prefix of the counterparty chain. */
  prefix: MerklePrefix | undefined;
}

/** ClientPaths define all the connection paths for a client state. */
export interface ClientPaths {
  /** list of connection paths */
  paths: string[];
}

/** ConnectionPaths define all the connection paths for a given client state. */
export interface ConnectionPaths {
  /** client state unique identifier */
  client_id: string;
  /** list of connection paths */
  paths: string[];
}

/**
 * Version defines the versioning scheme used to negotiate the IBC verison in
 * the connection handshake.
 */
export interface Version {
  /** unique version identifier */
  identifier: string;
  /** list of features compatible with the specified identifier */
  features: string[];
}

/** Params defines the set of Connection parameters. */
export interface Params {
  /**
   * maximum expected time per block (in nanoseconds), used to enforce block delay. This parameter should reflect the
   * largest amount of time that the chain might reasonably take to produce the next block under normal operating
   * conditions. A safe choice is 3-5x the expected time per block.
   */
  max_expected_time_per_block: number;
}

const baseConnectionEnd: object = { client_id: "", state: 0, delay_period: 0 };

export const ConnectionEnd = {
  encode(message: ConnectionEnd, writer: Writer = Writer.create()): Writer {
    if (message.client_id !== "") {
      writer.uint32(10).string(message.client_id);
    }
    for (const v of message.versions) {
      Version.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    if (message.state !== 0) {
      writer.uint32(24).int32(message.state);
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
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ConnectionEnd {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseConnectionEnd } as ConnectionEnd;
    message.versions = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_id = reader.string();
          break;
        case 2:
          message.versions.push(Version.decode(reader, reader.uint32()));
          break;
        case 3:
          message.state = reader.int32() as any;
          break;
        case 4:
          message.counterparty = Counterparty.decode(reader, reader.uint32());
          break;
        case 5:
          message.delay_period = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ConnectionEnd {
    const message = { ...baseConnectionEnd } as ConnectionEnd;
    message.versions = [];
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
    }
    if (object.versions !== undefined && object.versions !== null) {
      for (const e of object.versions) {
        message.versions.push(Version.fromJSON(e));
      }
    }
    if (object.state !== undefined && object.state !== null) {
      message.state = stateFromJSON(object.state);
    } else {
      message.state = 0;
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
    return message;
  },

  toJSON(message: ConnectionEnd): unknown {
    const obj: any = {};
    message.client_id !== undefined && (obj.client_id = message.client_id);
    if (message.versions) {
      obj.versions = message.versions.map((e) =>
        e ? Version.toJSON(e) : undefined
      );
    } else {
      obj.versions = [];
    }
    message.state !== undefined && (obj.state = stateToJSON(message.state));
    message.counterparty !== undefined &&
      (obj.counterparty = message.counterparty
        ? Counterparty.toJSON(message.counterparty)
        : undefined);
    message.delay_period !== undefined &&
      (obj.delay_period = message.delay_period);
    return obj;
  },

  fromPartial(object: DeepPartial<ConnectionEnd>): ConnectionEnd {
    const message = { ...baseConnectionEnd } as ConnectionEnd;
    message.versions = [];
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
    }
    if (object.versions !== undefined && object.versions !== null) {
      for (const e of object.versions) {
        message.versions.push(Version.fromPartial(e));
      }
    }
    if (object.state !== undefined && object.state !== null) {
      message.state = object.state;
    } else {
      message.state = 0;
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
    return message;
  },
};

const baseIdentifiedConnection: object = {
  id: "",
  client_id: "",
  state: 0,
  delay_period: 0,
};

export const IdentifiedConnection = {
  encode(
    message: IdentifiedConnection,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.id !== "") {
      writer.uint32(10).string(message.id);
    }
    if (message.client_id !== "") {
      writer.uint32(18).string(message.client_id);
    }
    for (const v of message.versions) {
      Version.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    if (message.state !== 0) {
      writer.uint32(32).int32(message.state);
    }
    if (message.counterparty !== undefined) {
      Counterparty.encode(
        message.counterparty,
        writer.uint32(42).fork()
      ).ldelim();
    }
    if (message.delay_period !== 0) {
      writer.uint32(48).uint64(message.delay_period);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): IdentifiedConnection {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseIdentifiedConnection } as IdentifiedConnection;
    message.versions = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.id = reader.string();
          break;
        case 2:
          message.client_id = reader.string();
          break;
        case 3:
          message.versions.push(Version.decode(reader, reader.uint32()));
          break;
        case 4:
          message.state = reader.int32() as any;
          break;
        case 5:
          message.counterparty = Counterparty.decode(reader, reader.uint32());
          break;
        case 6:
          message.delay_period = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): IdentifiedConnection {
    const message = { ...baseIdentifiedConnection } as IdentifiedConnection;
    message.versions = [];
    if (object.id !== undefined && object.id !== null) {
      message.id = String(object.id);
    } else {
      message.id = "";
    }
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
    }
    if (object.versions !== undefined && object.versions !== null) {
      for (const e of object.versions) {
        message.versions.push(Version.fromJSON(e));
      }
    }
    if (object.state !== undefined && object.state !== null) {
      message.state = stateFromJSON(object.state);
    } else {
      message.state = 0;
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
    return message;
  },

  toJSON(message: IdentifiedConnection): unknown {
    const obj: any = {};
    message.id !== undefined && (obj.id = message.id);
    message.client_id !== undefined && (obj.client_id = message.client_id);
    if (message.versions) {
      obj.versions = message.versions.map((e) =>
        e ? Version.toJSON(e) : undefined
      );
    } else {
      obj.versions = [];
    }
    message.state !== undefined && (obj.state = stateToJSON(message.state));
    message.counterparty !== undefined &&
      (obj.counterparty = message.counterparty
        ? Counterparty.toJSON(message.counterparty)
        : undefined);
    message.delay_period !== undefined &&
      (obj.delay_period = message.delay_period);
    return obj;
  },

  fromPartial(object: DeepPartial<IdentifiedConnection>): IdentifiedConnection {
    const message = { ...baseIdentifiedConnection } as IdentifiedConnection;
    message.versions = [];
    if (object.id !== undefined && object.id !== null) {
      message.id = object.id;
    } else {
      message.id = "";
    }
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
    }
    if (object.versions !== undefined && object.versions !== null) {
      for (const e of object.versions) {
        message.versions.push(Version.fromPartial(e));
      }
    }
    if (object.state !== undefined && object.state !== null) {
      message.state = object.state;
    } else {
      message.state = 0;
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
    return message;
  },
};

const baseCounterparty: object = { client_id: "", connection_id: "" };

export const Counterparty = {
  encode(message: Counterparty, writer: Writer = Writer.create()): Writer {
    if (message.client_id !== "") {
      writer.uint32(10).string(message.client_id);
    }
    if (message.connection_id !== "") {
      writer.uint32(18).string(message.connection_id);
    }
    if (message.prefix !== undefined) {
      MerklePrefix.encode(message.prefix, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Counterparty {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseCounterparty } as Counterparty;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_id = reader.string();
          break;
        case 2:
          message.connection_id = reader.string();
          break;
        case 3:
          message.prefix = MerklePrefix.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Counterparty {
    const message = { ...baseCounterparty } as Counterparty;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
    }
    if (object.connection_id !== undefined && object.connection_id !== null) {
      message.connection_id = String(object.connection_id);
    } else {
      message.connection_id = "";
    }
    if (object.prefix !== undefined && object.prefix !== null) {
      message.prefix = MerklePrefix.fromJSON(object.prefix);
    } else {
      message.prefix = undefined;
    }
    return message;
  },

  toJSON(message: Counterparty): unknown {
    const obj: any = {};
    message.client_id !== undefined && (obj.client_id = message.client_id);
    message.connection_id !== undefined &&
      (obj.connection_id = message.connection_id);
    message.prefix !== undefined &&
      (obj.prefix = message.prefix
        ? MerklePrefix.toJSON(message.prefix)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<Counterparty>): Counterparty {
    const message = { ...baseCounterparty } as Counterparty;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
    }
    if (object.connection_id !== undefined && object.connection_id !== null) {
      message.connection_id = object.connection_id;
    } else {
      message.connection_id = "";
    }
    if (object.prefix !== undefined && object.prefix !== null) {
      message.prefix = MerklePrefix.fromPartial(object.prefix);
    } else {
      message.prefix = undefined;
    }
    return message;
  },
};

const baseClientPaths: object = { paths: "" };

export const ClientPaths = {
  encode(message: ClientPaths, writer: Writer = Writer.create()): Writer {
    for (const v of message.paths) {
      writer.uint32(10).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ClientPaths {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseClientPaths } as ClientPaths;
    message.paths = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.paths.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ClientPaths {
    const message = { ...baseClientPaths } as ClientPaths;
    message.paths = [];
    if (object.paths !== undefined && object.paths !== null) {
      for (const e of object.paths) {
        message.paths.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: ClientPaths): unknown {
    const obj: any = {};
    if (message.paths) {
      obj.paths = message.paths.map((e) => e);
    } else {
      obj.paths = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<ClientPaths>): ClientPaths {
    const message = { ...baseClientPaths } as ClientPaths;
    message.paths = [];
    if (object.paths !== undefined && object.paths !== null) {
      for (const e of object.paths) {
        message.paths.push(e);
      }
    }
    return message;
  },
};

const baseConnectionPaths: object = { client_id: "", paths: "" };

export const ConnectionPaths = {
  encode(message: ConnectionPaths, writer: Writer = Writer.create()): Writer {
    if (message.client_id !== "") {
      writer.uint32(10).string(message.client_id);
    }
    for (const v of message.paths) {
      writer.uint32(18).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ConnectionPaths {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseConnectionPaths } as ConnectionPaths;
    message.paths = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_id = reader.string();
          break;
        case 2:
          message.paths.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ConnectionPaths {
    const message = { ...baseConnectionPaths } as ConnectionPaths;
    message.paths = [];
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
    }
    if (object.paths !== undefined && object.paths !== null) {
      for (const e of object.paths) {
        message.paths.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: ConnectionPaths): unknown {
    const obj: any = {};
    message.client_id !== undefined && (obj.client_id = message.client_id);
    if (message.paths) {
      obj.paths = message.paths.map((e) => e);
    } else {
      obj.paths = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<ConnectionPaths>): ConnectionPaths {
    const message = { ...baseConnectionPaths } as ConnectionPaths;
    message.paths = [];
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
    }
    if (object.paths !== undefined && object.paths !== null) {
      for (const e of object.paths) {
        message.paths.push(e);
      }
    }
    return message;
  },
};

const baseVersion: object = { identifier: "", features: "" };

export const Version = {
  encode(message: Version, writer: Writer = Writer.create()): Writer {
    if (message.identifier !== "") {
      writer.uint32(10).string(message.identifier);
    }
    for (const v of message.features) {
      writer.uint32(18).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Version {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseVersion } as Version;
    message.features = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.identifier = reader.string();
          break;
        case 2:
          message.features.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Version {
    const message = { ...baseVersion } as Version;
    message.features = [];
    if (object.identifier !== undefined && object.identifier !== null) {
      message.identifier = String(object.identifier);
    } else {
      message.identifier = "";
    }
    if (object.features !== undefined && object.features !== null) {
      for (const e of object.features) {
        message.features.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: Version): unknown {
    const obj: any = {};
    message.identifier !== undefined && (obj.identifier = message.identifier);
    if (message.features) {
      obj.features = message.features.map((e) => e);
    } else {
      obj.features = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<Version>): Version {
    const message = { ...baseVersion } as Version;
    message.features = [];
    if (object.identifier !== undefined && object.identifier !== null) {
      message.identifier = object.identifier;
    } else {
      message.identifier = "";
    }
    if (object.features !== undefined && object.features !== null) {
      for (const e of object.features) {
        message.features.push(e);
      }
    }
    return message;
  },
};

const baseParams: object = { max_expected_time_per_block: 0 };

export const Params = {
  encode(message: Params, writer: Writer = Writer.create()): Writer {
    if (message.max_expected_time_per_block !== 0) {
      writer.uint32(8).uint64(message.max_expected_time_per_block);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Params {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseParams } as Params;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.max_expected_time_per_block = longToNumber(
            reader.uint64() as Long
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Params {
    const message = { ...baseParams } as Params;
    if (
      object.max_expected_time_per_block !== undefined &&
      object.max_expected_time_per_block !== null
    ) {
      message.max_expected_time_per_block = Number(
        object.max_expected_time_per_block
      );
    } else {
      message.max_expected_time_per_block = 0;
    }
    return message;
  },

  toJSON(message: Params): unknown {
    const obj: any = {};
    message.max_expected_time_per_block !== undefined &&
      (obj.max_expected_time_per_block = message.max_expected_time_per_block);
    return obj;
  },

  fromPartial(object: DeepPartial<Params>): Params {
    const message = { ...baseParams } as Params;
    if (
      object.max_expected_time_per_block !== undefined &&
      object.max_expected_time_per_block !== null
    ) {
      message.max_expected_time_per_block = object.max_expected_time_per_block;
    } else {
      message.max_expected_time_per_block = 0;
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
