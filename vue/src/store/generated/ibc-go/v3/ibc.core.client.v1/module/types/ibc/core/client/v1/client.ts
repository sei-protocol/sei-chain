/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Any } from "../../../../google/protobuf/any";
import { Plan } from "../../../../cosmos/upgrade/v1beta1/upgrade";

export const protobufPackage = "ibc.core.client.v1";

/**
 * IdentifiedClientState defines a client state with an additional client
 * identifier field.
 */
export interface IdentifiedClientState {
  /** client identifier */
  client_id: string;
  /** client state */
  client_state: Any | undefined;
}

/**
 * ConsensusStateWithHeight defines a consensus state with an additional height
 * field.
 */
export interface ConsensusStateWithHeight {
  /** consensus state height */
  height: Height | undefined;
  /** consensus state */
  consensus_state: Any | undefined;
}

/**
 * ClientConsensusStates defines all the stored consensus states for a given
 * client.
 */
export interface ClientConsensusStates {
  /** client identifier */
  client_id: string;
  /** consensus states and their heights associated with the client */
  consensus_states: ConsensusStateWithHeight[];
}

/**
 * ClientUpdateProposal is a governance proposal. If it passes, the substitute
 * client's latest consensus state is copied over to the subject client. The proposal
 * handler may fail if the subject and the substitute do not match in client and
 * chain parameters (with exception to latest height, frozen height, and chain-id).
 */
export interface ClientUpdateProposal {
  /** the title of the update proposal */
  title: string;
  /** the description of the proposal */
  description: string;
  /** the client identifier for the client to be updated if the proposal passes */
  subject_client_id: string;
  /**
   * the substitute client identifier for the client standing in for the subject
   * client
   */
  substitute_client_id: string;
}

/**
 * UpgradeProposal is a gov Content type for initiating an IBC breaking
 * upgrade.
 */
export interface UpgradeProposal {
  title: string;
  description: string;
  plan: Plan | undefined;
  /**
   * An UpgradedClientState must be provided to perform an IBC breaking upgrade.
   * This will make the chain commit to the correct upgraded (self) client state
   * before the upgrade occurs, so that connecting chains can verify that the
   * new upgraded client is valid by verifying a proof on the previous version
   * of the chain. This will allow IBC connections to persist smoothly across
   * planned chain upgrades
   */
  upgraded_client_state: Any | undefined;
}

/**
 * Height is a monotonically increasing data type
 * that can be compared against another Height for the purposes of updating and
 * freezing clients
 *
 * Normally the RevisionHeight is incremented at each height while keeping
 * RevisionNumber the same. However some consensus algorithms may choose to
 * reset the height in certain conditions e.g. hard forks, state-machine
 * breaking changes In these cases, the RevisionNumber is incremented so that
 * height continues to be monitonically increasing even as the RevisionHeight
 * gets reset
 */
export interface Height {
  /** the revision that the client is currently on */
  revision_number: number;
  /** the height within the given revision */
  revision_height: number;
}

/** Params defines the set of IBC light client parameters. */
export interface Params {
  /** allowed_clients defines the list of allowed client state types. */
  allowed_clients: string[];
}

const baseIdentifiedClientState: object = { client_id: "" };

export const IdentifiedClientState = {
  encode(
    message: IdentifiedClientState,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.client_id !== "") {
      writer.uint32(10).string(message.client_id);
    }
    if (message.client_state !== undefined) {
      Any.encode(message.client_state, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): IdentifiedClientState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseIdentifiedClientState } as IdentifiedClientState;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_id = reader.string();
          break;
        case 2:
          message.client_state = Any.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): IdentifiedClientState {
    const message = { ...baseIdentifiedClientState } as IdentifiedClientState;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
    }
    if (object.client_state !== undefined && object.client_state !== null) {
      message.client_state = Any.fromJSON(object.client_state);
    } else {
      message.client_state = undefined;
    }
    return message;
  },

  toJSON(message: IdentifiedClientState): unknown {
    const obj: any = {};
    message.client_id !== undefined && (obj.client_id = message.client_id);
    message.client_state !== undefined &&
      (obj.client_state = message.client_state
        ? Any.toJSON(message.client_state)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<IdentifiedClientState>
  ): IdentifiedClientState {
    const message = { ...baseIdentifiedClientState } as IdentifiedClientState;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
    }
    if (object.client_state !== undefined && object.client_state !== null) {
      message.client_state = Any.fromPartial(object.client_state);
    } else {
      message.client_state = undefined;
    }
    return message;
  },
};

const baseConsensusStateWithHeight: object = {};

export const ConsensusStateWithHeight = {
  encode(
    message: ConsensusStateWithHeight,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.height !== undefined) {
      Height.encode(message.height, writer.uint32(10).fork()).ldelim();
    }
    if (message.consensus_state !== undefined) {
      Any.encode(message.consensus_state, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ConsensusStateWithHeight {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseConsensusStateWithHeight,
    } as ConsensusStateWithHeight;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.height = Height.decode(reader, reader.uint32());
          break;
        case 2:
          message.consensus_state = Any.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ConsensusStateWithHeight {
    const message = {
      ...baseConsensusStateWithHeight,
    } as ConsensusStateWithHeight;
    if (object.height !== undefined && object.height !== null) {
      message.height = Height.fromJSON(object.height);
    } else {
      message.height = undefined;
    }
    if (
      object.consensus_state !== undefined &&
      object.consensus_state !== null
    ) {
      message.consensus_state = Any.fromJSON(object.consensus_state);
    } else {
      message.consensus_state = undefined;
    }
    return message;
  },

  toJSON(message: ConsensusStateWithHeight): unknown {
    const obj: any = {};
    message.height !== undefined &&
      (obj.height = message.height ? Height.toJSON(message.height) : undefined);
    message.consensus_state !== undefined &&
      (obj.consensus_state = message.consensus_state
        ? Any.toJSON(message.consensus_state)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<ConsensusStateWithHeight>
  ): ConsensusStateWithHeight {
    const message = {
      ...baseConsensusStateWithHeight,
    } as ConsensusStateWithHeight;
    if (object.height !== undefined && object.height !== null) {
      message.height = Height.fromPartial(object.height);
    } else {
      message.height = undefined;
    }
    if (
      object.consensus_state !== undefined &&
      object.consensus_state !== null
    ) {
      message.consensus_state = Any.fromPartial(object.consensus_state);
    } else {
      message.consensus_state = undefined;
    }
    return message;
  },
};

const baseClientConsensusStates: object = { client_id: "" };

export const ClientConsensusStates = {
  encode(
    message: ClientConsensusStates,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.client_id !== "") {
      writer.uint32(10).string(message.client_id);
    }
    for (const v of message.consensus_states) {
      ConsensusStateWithHeight.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ClientConsensusStates {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseClientConsensusStates } as ClientConsensusStates;
    message.consensus_states = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_id = reader.string();
          break;
        case 2:
          message.consensus_states.push(
            ConsensusStateWithHeight.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ClientConsensusStates {
    const message = { ...baseClientConsensusStates } as ClientConsensusStates;
    message.consensus_states = [];
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
    }
    if (
      object.consensus_states !== undefined &&
      object.consensus_states !== null
    ) {
      for (const e of object.consensus_states) {
        message.consensus_states.push(ConsensusStateWithHeight.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: ClientConsensusStates): unknown {
    const obj: any = {};
    message.client_id !== undefined && (obj.client_id = message.client_id);
    if (message.consensus_states) {
      obj.consensus_states = message.consensus_states.map((e) =>
        e ? ConsensusStateWithHeight.toJSON(e) : undefined
      );
    } else {
      obj.consensus_states = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<ClientConsensusStates>
  ): ClientConsensusStates {
    const message = { ...baseClientConsensusStates } as ClientConsensusStates;
    message.consensus_states = [];
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
    }
    if (
      object.consensus_states !== undefined &&
      object.consensus_states !== null
    ) {
      for (const e of object.consensus_states) {
        message.consensus_states.push(ConsensusStateWithHeight.fromPartial(e));
      }
    }
    return message;
  },
};

const baseClientUpdateProposal: object = {
  title: "",
  description: "",
  subject_client_id: "",
  substitute_client_id: "",
};

export const ClientUpdateProposal = {
  encode(
    message: ClientUpdateProposal,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    if (message.subject_client_id !== "") {
      writer.uint32(26).string(message.subject_client_id);
    }
    if (message.substitute_client_id !== "") {
      writer.uint32(34).string(message.substitute_client_id);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ClientUpdateProposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseClientUpdateProposal } as ClientUpdateProposal;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.title = reader.string();
          break;
        case 2:
          message.description = reader.string();
          break;
        case 3:
          message.subject_client_id = reader.string();
          break;
        case 4:
          message.substitute_client_id = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ClientUpdateProposal {
    const message = { ...baseClientUpdateProposal } as ClientUpdateProposal;
    if (object.title !== undefined && object.title !== null) {
      message.title = String(object.title);
    } else {
      message.title = "";
    }
    if (object.description !== undefined && object.description !== null) {
      message.description = String(object.description);
    } else {
      message.description = "";
    }
    if (
      object.subject_client_id !== undefined &&
      object.subject_client_id !== null
    ) {
      message.subject_client_id = String(object.subject_client_id);
    } else {
      message.subject_client_id = "";
    }
    if (
      object.substitute_client_id !== undefined &&
      object.substitute_client_id !== null
    ) {
      message.substitute_client_id = String(object.substitute_client_id);
    } else {
      message.substitute_client_id = "";
    }
    return message;
  },

  toJSON(message: ClientUpdateProposal): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    message.subject_client_id !== undefined &&
      (obj.subject_client_id = message.subject_client_id);
    message.substitute_client_id !== undefined &&
      (obj.substitute_client_id = message.substitute_client_id);
    return obj;
  },

  fromPartial(object: DeepPartial<ClientUpdateProposal>): ClientUpdateProposal {
    const message = { ...baseClientUpdateProposal } as ClientUpdateProposal;
    if (object.title !== undefined && object.title !== null) {
      message.title = object.title;
    } else {
      message.title = "";
    }
    if (object.description !== undefined && object.description !== null) {
      message.description = object.description;
    } else {
      message.description = "";
    }
    if (
      object.subject_client_id !== undefined &&
      object.subject_client_id !== null
    ) {
      message.subject_client_id = object.subject_client_id;
    } else {
      message.subject_client_id = "";
    }
    if (
      object.substitute_client_id !== undefined &&
      object.substitute_client_id !== null
    ) {
      message.substitute_client_id = object.substitute_client_id;
    } else {
      message.substitute_client_id = "";
    }
    return message;
  },
};

const baseUpgradeProposal: object = { title: "", description: "" };

export const UpgradeProposal = {
  encode(message: UpgradeProposal, writer: Writer = Writer.create()): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    if (message.plan !== undefined) {
      Plan.encode(message.plan, writer.uint32(26).fork()).ldelim();
    }
    if (message.upgraded_client_state !== undefined) {
      Any.encode(
        message.upgraded_client_state,
        writer.uint32(34).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): UpgradeProposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseUpgradeProposal } as UpgradeProposal;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.title = reader.string();
          break;
        case 2:
          message.description = reader.string();
          break;
        case 3:
          message.plan = Plan.decode(reader, reader.uint32());
          break;
        case 4:
          message.upgraded_client_state = Any.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): UpgradeProposal {
    const message = { ...baseUpgradeProposal } as UpgradeProposal;
    if (object.title !== undefined && object.title !== null) {
      message.title = String(object.title);
    } else {
      message.title = "";
    }
    if (object.description !== undefined && object.description !== null) {
      message.description = String(object.description);
    } else {
      message.description = "";
    }
    if (object.plan !== undefined && object.plan !== null) {
      message.plan = Plan.fromJSON(object.plan);
    } else {
      message.plan = undefined;
    }
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

  toJSON(message: UpgradeProposal): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    message.plan !== undefined &&
      (obj.plan = message.plan ? Plan.toJSON(message.plan) : undefined);
    message.upgraded_client_state !== undefined &&
      (obj.upgraded_client_state = message.upgraded_client_state
        ? Any.toJSON(message.upgraded_client_state)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<UpgradeProposal>): UpgradeProposal {
    const message = { ...baseUpgradeProposal } as UpgradeProposal;
    if (object.title !== undefined && object.title !== null) {
      message.title = object.title;
    } else {
      message.title = "";
    }
    if (object.description !== undefined && object.description !== null) {
      message.description = object.description;
    } else {
      message.description = "";
    }
    if (object.plan !== undefined && object.plan !== null) {
      message.plan = Plan.fromPartial(object.plan);
    } else {
      message.plan = undefined;
    }
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

const baseHeight: object = { revision_number: 0, revision_height: 0 };

export const Height = {
  encode(message: Height, writer: Writer = Writer.create()): Writer {
    if (message.revision_number !== 0) {
      writer.uint32(8).uint64(message.revision_number);
    }
    if (message.revision_height !== 0) {
      writer.uint32(16).uint64(message.revision_height);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Height {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseHeight } as Height;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.revision_number = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.revision_height = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Height {
    const message = { ...baseHeight } as Height;
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

  toJSON(message: Height): unknown {
    const obj: any = {};
    message.revision_number !== undefined &&
      (obj.revision_number = message.revision_number);
    message.revision_height !== undefined &&
      (obj.revision_height = message.revision_height);
    return obj;
  },

  fromPartial(object: DeepPartial<Height>): Height {
    const message = { ...baseHeight } as Height;
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

const baseParams: object = { allowed_clients: "" };

export const Params = {
  encode(message: Params, writer: Writer = Writer.create()): Writer {
    for (const v of message.allowed_clients) {
      writer.uint32(10).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Params {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseParams } as Params;
    message.allowed_clients = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.allowed_clients.push(reader.string());
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
    message.allowed_clients = [];
    if (
      object.allowed_clients !== undefined &&
      object.allowed_clients !== null
    ) {
      for (const e of object.allowed_clients) {
        message.allowed_clients.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: Params): unknown {
    const obj: any = {};
    if (message.allowed_clients) {
      obj.allowed_clients = message.allowed_clients.map((e) => e);
    } else {
      obj.allowed_clients = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<Params>): Params {
    const message = { ...baseParams } as Params;
    message.allowed_clients = [];
    if (
      object.allowed_clients !== undefined &&
      object.allowed_clients !== null
    ) {
      for (const e of object.allowed_clients) {
        message.allowed_clients.push(e);
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
