/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Any } from "../../../../google/protobuf/any";

export const protobufPackage = "ibc.core.client.v1";

/** MsgCreateClient defines a message to create an IBC client */
export interface MsgCreateClient {
  /** light client state */
  client_state: Any | undefined;
  /**
   * consensus state associated with the client that corresponds to a given
   * height.
   */
  consensus_state: Any | undefined;
  /** signer address */
  signer: string;
}

/** MsgCreateClientResponse defines the Msg/CreateClient response type. */
export interface MsgCreateClientResponse {}

/**
 * MsgUpdateClient defines an sdk.Msg to update a IBC client state using
 * the given header.
 */
export interface MsgUpdateClient {
  /** client unique identifier */
  client_id: string;
  /** header to update the light client */
  header: Any | undefined;
  /** signer address */
  signer: string;
}

/** MsgUpdateClientResponse defines the Msg/UpdateClient response type. */
export interface MsgUpdateClientResponse {}

/**
 * MsgUpgradeClient defines an sdk.Msg to upgrade an IBC client to a new client
 * state
 */
export interface MsgUpgradeClient {
  /** client unique identifier */
  client_id: string;
  /** upgraded client state */
  client_state: Any | undefined;
  /**
   * upgraded consensus state, only contains enough information to serve as a
   * basis of trust in update logic
   */
  consensus_state: Any | undefined;
  /** proof that old chain committed to new client */
  proof_upgrade_client: Uint8Array;
  /** proof that old chain committed to new consensus state */
  proof_upgrade_consensus_state: Uint8Array;
  /** signer address */
  signer: string;
}

/** MsgUpgradeClientResponse defines the Msg/UpgradeClient response type. */
export interface MsgUpgradeClientResponse {}

/**
 * MsgSubmitMisbehaviour defines an sdk.Msg type that submits Evidence for
 * light client misbehaviour.
 */
export interface MsgSubmitMisbehaviour {
  /** client unique identifier */
  client_id: string;
  /** misbehaviour used for freezing the light client */
  misbehaviour: Any | undefined;
  /** signer address */
  signer: string;
}

/**
 * MsgSubmitMisbehaviourResponse defines the Msg/SubmitMisbehaviour response
 * type.
 */
export interface MsgSubmitMisbehaviourResponse {}

const baseMsgCreateClient: object = { signer: "" };

export const MsgCreateClient = {
  encode(message: MsgCreateClient, writer: Writer = Writer.create()): Writer {
    if (message.client_state !== undefined) {
      Any.encode(message.client_state, writer.uint32(10).fork()).ldelim();
    }
    if (message.consensus_state !== undefined) {
      Any.encode(message.consensus_state, writer.uint32(18).fork()).ldelim();
    }
    if (message.signer !== "") {
      writer.uint32(26).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgCreateClient {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgCreateClient } as MsgCreateClient;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_state = Any.decode(reader, reader.uint32());
          break;
        case 2:
          message.consensus_state = Any.decode(reader, reader.uint32());
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

  fromJSON(object: any): MsgCreateClient {
    const message = { ...baseMsgCreateClient } as MsgCreateClient;
    if (object.client_state !== undefined && object.client_state !== null) {
      message.client_state = Any.fromJSON(object.client_state);
    } else {
      message.client_state = undefined;
    }
    if (
      object.consensus_state !== undefined &&
      object.consensus_state !== null
    ) {
      message.consensus_state = Any.fromJSON(object.consensus_state);
    } else {
      message.consensus_state = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgCreateClient): unknown {
    const obj: any = {};
    message.client_state !== undefined &&
      (obj.client_state = message.client_state
        ? Any.toJSON(message.client_state)
        : undefined);
    message.consensus_state !== undefined &&
      (obj.consensus_state = message.consensus_state
        ? Any.toJSON(message.consensus_state)
        : undefined);
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgCreateClient>): MsgCreateClient {
    const message = { ...baseMsgCreateClient } as MsgCreateClient;
    if (object.client_state !== undefined && object.client_state !== null) {
      message.client_state = Any.fromPartial(object.client_state);
    } else {
      message.client_state = undefined;
    }
    if (
      object.consensus_state !== undefined &&
      object.consensus_state !== null
    ) {
      message.consensus_state = Any.fromPartial(object.consensus_state);
    } else {
      message.consensus_state = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgCreateClientResponse: object = {};

export const MsgCreateClientResponse = {
  encode(_: MsgCreateClientResponse, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgCreateClientResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgCreateClientResponse,
    } as MsgCreateClientResponse;
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

  fromJSON(_: any): MsgCreateClientResponse {
    const message = {
      ...baseMsgCreateClientResponse,
    } as MsgCreateClientResponse;
    return message;
  },

  toJSON(_: MsgCreateClientResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgCreateClientResponse>
  ): MsgCreateClientResponse {
    const message = {
      ...baseMsgCreateClientResponse,
    } as MsgCreateClientResponse;
    return message;
  },
};

const baseMsgUpdateClient: object = { client_id: "", signer: "" };

export const MsgUpdateClient = {
  encode(message: MsgUpdateClient, writer: Writer = Writer.create()): Writer {
    if (message.client_id !== "") {
      writer.uint32(10).string(message.client_id);
    }
    if (message.header !== undefined) {
      Any.encode(message.header, writer.uint32(18).fork()).ldelim();
    }
    if (message.signer !== "") {
      writer.uint32(26).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgUpdateClient {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgUpdateClient } as MsgUpdateClient;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_id = reader.string();
          break;
        case 2:
          message.header = Any.decode(reader, reader.uint32());
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

  fromJSON(object: any): MsgUpdateClient {
    const message = { ...baseMsgUpdateClient } as MsgUpdateClient;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
    }
    if (object.header !== undefined && object.header !== null) {
      message.header = Any.fromJSON(object.header);
    } else {
      message.header = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgUpdateClient): unknown {
    const obj: any = {};
    message.client_id !== undefined && (obj.client_id = message.client_id);
    message.header !== undefined &&
      (obj.header = message.header ? Any.toJSON(message.header) : undefined);
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgUpdateClient>): MsgUpdateClient {
    const message = { ...baseMsgUpdateClient } as MsgUpdateClient;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
    }
    if (object.header !== undefined && object.header !== null) {
      message.header = Any.fromPartial(object.header);
    } else {
      message.header = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgUpdateClientResponse: object = {};

export const MsgUpdateClientResponse = {
  encode(_: MsgUpdateClientResponse, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgUpdateClientResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgUpdateClientResponse,
    } as MsgUpdateClientResponse;
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

  fromJSON(_: any): MsgUpdateClientResponse {
    const message = {
      ...baseMsgUpdateClientResponse,
    } as MsgUpdateClientResponse;
    return message;
  },

  toJSON(_: MsgUpdateClientResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgUpdateClientResponse>
  ): MsgUpdateClientResponse {
    const message = {
      ...baseMsgUpdateClientResponse,
    } as MsgUpdateClientResponse;
    return message;
  },
};

const baseMsgUpgradeClient: object = { client_id: "", signer: "" };

export const MsgUpgradeClient = {
  encode(message: MsgUpgradeClient, writer: Writer = Writer.create()): Writer {
    if (message.client_id !== "") {
      writer.uint32(10).string(message.client_id);
    }
    if (message.client_state !== undefined) {
      Any.encode(message.client_state, writer.uint32(18).fork()).ldelim();
    }
    if (message.consensus_state !== undefined) {
      Any.encode(message.consensus_state, writer.uint32(26).fork()).ldelim();
    }
    if (message.proof_upgrade_client.length !== 0) {
      writer.uint32(34).bytes(message.proof_upgrade_client);
    }
    if (message.proof_upgrade_consensus_state.length !== 0) {
      writer.uint32(42).bytes(message.proof_upgrade_consensus_state);
    }
    if (message.signer !== "") {
      writer.uint32(50).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgUpgradeClient {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgUpgradeClient } as MsgUpgradeClient;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_id = reader.string();
          break;
        case 2:
          message.client_state = Any.decode(reader, reader.uint32());
          break;
        case 3:
          message.consensus_state = Any.decode(reader, reader.uint32());
          break;
        case 4:
          message.proof_upgrade_client = reader.bytes();
          break;
        case 5:
          message.proof_upgrade_consensus_state = reader.bytes();
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

  fromJSON(object: any): MsgUpgradeClient {
    const message = { ...baseMsgUpgradeClient } as MsgUpgradeClient;
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
    if (
      object.consensus_state !== undefined &&
      object.consensus_state !== null
    ) {
      message.consensus_state = Any.fromJSON(object.consensus_state);
    } else {
      message.consensus_state = undefined;
    }
    if (
      object.proof_upgrade_client !== undefined &&
      object.proof_upgrade_client !== null
    ) {
      message.proof_upgrade_client = bytesFromBase64(
        object.proof_upgrade_client
      );
    }
    if (
      object.proof_upgrade_consensus_state !== undefined &&
      object.proof_upgrade_consensus_state !== null
    ) {
      message.proof_upgrade_consensus_state = bytesFromBase64(
        object.proof_upgrade_consensus_state
      );
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgUpgradeClient): unknown {
    const obj: any = {};
    message.client_id !== undefined && (obj.client_id = message.client_id);
    message.client_state !== undefined &&
      (obj.client_state = message.client_state
        ? Any.toJSON(message.client_state)
        : undefined);
    message.consensus_state !== undefined &&
      (obj.consensus_state = message.consensus_state
        ? Any.toJSON(message.consensus_state)
        : undefined);
    message.proof_upgrade_client !== undefined &&
      (obj.proof_upgrade_client = base64FromBytes(
        message.proof_upgrade_client !== undefined
          ? message.proof_upgrade_client
          : new Uint8Array()
      ));
    message.proof_upgrade_consensus_state !== undefined &&
      (obj.proof_upgrade_consensus_state = base64FromBytes(
        message.proof_upgrade_consensus_state !== undefined
          ? message.proof_upgrade_consensus_state
          : new Uint8Array()
      ));
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgUpgradeClient>): MsgUpgradeClient {
    const message = { ...baseMsgUpgradeClient } as MsgUpgradeClient;
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
    if (
      object.consensus_state !== undefined &&
      object.consensus_state !== null
    ) {
      message.consensus_state = Any.fromPartial(object.consensus_state);
    } else {
      message.consensus_state = undefined;
    }
    if (
      object.proof_upgrade_client !== undefined &&
      object.proof_upgrade_client !== null
    ) {
      message.proof_upgrade_client = object.proof_upgrade_client;
    } else {
      message.proof_upgrade_client = new Uint8Array();
    }
    if (
      object.proof_upgrade_consensus_state !== undefined &&
      object.proof_upgrade_consensus_state !== null
    ) {
      message.proof_upgrade_consensus_state =
        object.proof_upgrade_consensus_state;
    } else {
      message.proof_upgrade_consensus_state = new Uint8Array();
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgUpgradeClientResponse: object = {};

export const MsgUpgradeClientResponse = {
  encode(
    _: MsgUpgradeClientResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgUpgradeClientResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgUpgradeClientResponse,
    } as MsgUpgradeClientResponse;
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

  fromJSON(_: any): MsgUpgradeClientResponse {
    const message = {
      ...baseMsgUpgradeClientResponse,
    } as MsgUpgradeClientResponse;
    return message;
  },

  toJSON(_: MsgUpgradeClientResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgUpgradeClientResponse>
  ): MsgUpgradeClientResponse {
    const message = {
      ...baseMsgUpgradeClientResponse,
    } as MsgUpgradeClientResponse;
    return message;
  },
};

const baseMsgSubmitMisbehaviour: object = { client_id: "", signer: "" };

export const MsgSubmitMisbehaviour = {
  encode(
    message: MsgSubmitMisbehaviour,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.client_id !== "") {
      writer.uint32(10).string(message.client_id);
    }
    if (message.misbehaviour !== undefined) {
      Any.encode(message.misbehaviour, writer.uint32(18).fork()).ldelim();
    }
    if (message.signer !== "") {
      writer.uint32(26).string(message.signer);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgSubmitMisbehaviour {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgSubmitMisbehaviour } as MsgSubmitMisbehaviour;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.client_id = reader.string();
          break;
        case 2:
          message.misbehaviour = Any.decode(reader, reader.uint32());
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

  fromJSON(object: any): MsgSubmitMisbehaviour {
    const message = { ...baseMsgSubmitMisbehaviour } as MsgSubmitMisbehaviour;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = String(object.client_id);
    } else {
      message.client_id = "";
    }
    if (object.misbehaviour !== undefined && object.misbehaviour !== null) {
      message.misbehaviour = Any.fromJSON(object.misbehaviour);
    } else {
      message.misbehaviour = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = String(object.signer);
    } else {
      message.signer = "";
    }
    return message;
  },

  toJSON(message: MsgSubmitMisbehaviour): unknown {
    const obj: any = {};
    message.client_id !== undefined && (obj.client_id = message.client_id);
    message.misbehaviour !== undefined &&
      (obj.misbehaviour = message.misbehaviour
        ? Any.toJSON(message.misbehaviour)
        : undefined);
    message.signer !== undefined && (obj.signer = message.signer);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgSubmitMisbehaviour>
  ): MsgSubmitMisbehaviour {
    const message = { ...baseMsgSubmitMisbehaviour } as MsgSubmitMisbehaviour;
    if (object.client_id !== undefined && object.client_id !== null) {
      message.client_id = object.client_id;
    } else {
      message.client_id = "";
    }
    if (object.misbehaviour !== undefined && object.misbehaviour !== null) {
      message.misbehaviour = Any.fromPartial(object.misbehaviour);
    } else {
      message.misbehaviour = undefined;
    }
    if (object.signer !== undefined && object.signer !== null) {
      message.signer = object.signer;
    } else {
      message.signer = "";
    }
    return message;
  },
};

const baseMsgSubmitMisbehaviourResponse: object = {};

export const MsgSubmitMisbehaviourResponse = {
  encode(
    _: MsgSubmitMisbehaviourResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgSubmitMisbehaviourResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgSubmitMisbehaviourResponse,
    } as MsgSubmitMisbehaviourResponse;
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

  fromJSON(_: any): MsgSubmitMisbehaviourResponse {
    const message = {
      ...baseMsgSubmitMisbehaviourResponse,
    } as MsgSubmitMisbehaviourResponse;
    return message;
  },

  toJSON(_: MsgSubmitMisbehaviourResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgSubmitMisbehaviourResponse>
  ): MsgSubmitMisbehaviourResponse {
    const message = {
      ...baseMsgSubmitMisbehaviourResponse,
    } as MsgSubmitMisbehaviourResponse;
    return message;
  },
};

/** Msg defines the ibc/client Msg service. */
export interface Msg {
  /** CreateClient defines a rpc handler method for MsgCreateClient. */
  CreateClient(request: MsgCreateClient): Promise<MsgCreateClientResponse>;
  /** UpdateClient defines a rpc handler method for MsgUpdateClient. */
  UpdateClient(request: MsgUpdateClient): Promise<MsgUpdateClientResponse>;
  /** UpgradeClient defines a rpc handler method for MsgUpgradeClient. */
  UpgradeClient(request: MsgUpgradeClient): Promise<MsgUpgradeClientResponse>;
  /** SubmitMisbehaviour defines a rpc handler method for MsgSubmitMisbehaviour. */
  SubmitMisbehaviour(
    request: MsgSubmitMisbehaviour
  ): Promise<MsgSubmitMisbehaviourResponse>;
}

export class MsgClientImpl implements Msg {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  CreateClient(request: MsgCreateClient): Promise<MsgCreateClientResponse> {
    const data = MsgCreateClient.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.client.v1.Msg",
      "CreateClient",
      data
    );
    return promise.then((data) =>
      MsgCreateClientResponse.decode(new Reader(data))
    );
  }

  UpdateClient(request: MsgUpdateClient): Promise<MsgUpdateClientResponse> {
    const data = MsgUpdateClient.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.client.v1.Msg",
      "UpdateClient",
      data
    );
    return promise.then((data) =>
      MsgUpdateClientResponse.decode(new Reader(data))
    );
  }

  UpgradeClient(request: MsgUpgradeClient): Promise<MsgUpgradeClientResponse> {
    const data = MsgUpgradeClient.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.client.v1.Msg",
      "UpgradeClient",
      data
    );
    return promise.then((data) =>
      MsgUpgradeClientResponse.decode(new Reader(data))
    );
  }

  SubmitMisbehaviour(
    request: MsgSubmitMisbehaviour
  ): Promise<MsgSubmitMisbehaviourResponse> {
    const data = MsgSubmitMisbehaviour.encode(request).finish();
    const promise = this.rpc.request(
      "ibc.core.client.v1.Msg",
      "SubmitMisbehaviour",
      data
    );
    return promise.then((data) =>
      MsgSubmitMisbehaviourResponse.decode(new Reader(data))
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
