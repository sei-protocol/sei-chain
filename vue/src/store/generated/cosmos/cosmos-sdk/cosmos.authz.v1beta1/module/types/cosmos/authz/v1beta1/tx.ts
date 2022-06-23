/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Grant } from "../../../cosmos/authz/v1beta1/authz";
import { Any } from "../../../google/protobuf/any";

export const protobufPackage = "cosmos.authz.v1beta1";

/** Since: cosmos-sdk 0.43 */

/**
 * MsgGrant is a request type for Grant method. It declares authorization to the grantee
 * on behalf of the granter with the provided expiration time.
 */
export interface MsgGrant {
  granter: string;
  grantee: string;
  grant: Grant | undefined;
}

/** MsgExecResponse defines the Msg/MsgExecResponse response type. */
export interface MsgExecResponse {
  results: Uint8Array[];
}

/**
 * MsgExec attempts to execute the provided messages using
 * authorizations granted to the grantee. Each message should have only
 * one signer corresponding to the granter of the authorization.
 */
export interface MsgExec {
  grantee: string;
  /**
   * Authorization Msg requests to execute. Each msg must implement Authorization interface
   * The x/authz will try to find a grant matching (msg.signers[0], grantee, MsgTypeURL(msg))
   * triple and validate it.
   */
  msgs: Any[];
}

/** MsgGrantResponse defines the Msg/MsgGrant response type. */
export interface MsgGrantResponse {}

/**
 * MsgRevoke revokes any authorization with the provided sdk.Msg type on the
 * granter's account with that has been granted to the grantee.
 */
export interface MsgRevoke {
  granter: string;
  grantee: string;
  msgTypeUrl: string;
}

/** MsgRevokeResponse defines the Msg/MsgRevokeResponse response type. */
export interface MsgRevokeResponse {}

const baseMsgGrant: object = { granter: "", grantee: "" };

export const MsgGrant = {
  encode(message: MsgGrant, writer: Writer = Writer.create()): Writer {
    if (message.granter !== "") {
      writer.uint32(10).string(message.granter);
    }
    if (message.grantee !== "") {
      writer.uint32(18).string(message.grantee);
    }
    if (message.grant !== undefined) {
      Grant.encode(message.grant, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgGrant {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgGrant } as MsgGrant;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.granter = reader.string();
          break;
        case 2:
          message.grantee = reader.string();
          break;
        case 3:
          message.grant = Grant.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgGrant {
    const message = { ...baseMsgGrant } as MsgGrant;
    if (object.granter !== undefined && object.granter !== null) {
      message.granter = String(object.granter);
    } else {
      message.granter = "";
    }
    if (object.grantee !== undefined && object.grantee !== null) {
      message.grantee = String(object.grantee);
    } else {
      message.grantee = "";
    }
    if (object.grant !== undefined && object.grant !== null) {
      message.grant = Grant.fromJSON(object.grant);
    } else {
      message.grant = undefined;
    }
    return message;
  },

  toJSON(message: MsgGrant): unknown {
    const obj: any = {};
    message.granter !== undefined && (obj.granter = message.granter);
    message.grantee !== undefined && (obj.grantee = message.grantee);
    message.grant !== undefined &&
      (obj.grant = message.grant ? Grant.toJSON(message.grant) : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgGrant>): MsgGrant {
    const message = { ...baseMsgGrant } as MsgGrant;
    if (object.granter !== undefined && object.granter !== null) {
      message.granter = object.granter;
    } else {
      message.granter = "";
    }
    if (object.grantee !== undefined && object.grantee !== null) {
      message.grantee = object.grantee;
    } else {
      message.grantee = "";
    }
    if (object.grant !== undefined && object.grant !== null) {
      message.grant = Grant.fromPartial(object.grant);
    } else {
      message.grant = undefined;
    }
    return message;
  },
};

const baseMsgExecResponse: object = {};

export const MsgExecResponse = {
  encode(message: MsgExecResponse, writer: Writer = Writer.create()): Writer {
    for (const v of message.results) {
      writer.uint32(10).bytes(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgExecResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgExecResponse } as MsgExecResponse;
    message.results = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.results.push(reader.bytes());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgExecResponse {
    const message = { ...baseMsgExecResponse } as MsgExecResponse;
    message.results = [];
    if (object.results !== undefined && object.results !== null) {
      for (const e of object.results) {
        message.results.push(bytesFromBase64(e));
      }
    }
    return message;
  },

  toJSON(message: MsgExecResponse): unknown {
    const obj: any = {};
    if (message.results) {
      obj.results = message.results.map((e) =>
        base64FromBytes(e !== undefined ? e : new Uint8Array())
      );
    } else {
      obj.results = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<MsgExecResponse>): MsgExecResponse {
    const message = { ...baseMsgExecResponse } as MsgExecResponse;
    message.results = [];
    if (object.results !== undefined && object.results !== null) {
      for (const e of object.results) {
        message.results.push(e);
      }
    }
    return message;
  },
};

const baseMsgExec: object = { grantee: "" };

export const MsgExec = {
  encode(message: MsgExec, writer: Writer = Writer.create()): Writer {
    if (message.grantee !== "") {
      writer.uint32(10).string(message.grantee);
    }
    for (const v of message.msgs) {
      Any.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgExec {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgExec } as MsgExec;
    message.msgs = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.grantee = reader.string();
          break;
        case 2:
          message.msgs.push(Any.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgExec {
    const message = { ...baseMsgExec } as MsgExec;
    message.msgs = [];
    if (object.grantee !== undefined && object.grantee !== null) {
      message.grantee = String(object.grantee);
    } else {
      message.grantee = "";
    }
    if (object.msgs !== undefined && object.msgs !== null) {
      for (const e of object.msgs) {
        message.msgs.push(Any.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: MsgExec): unknown {
    const obj: any = {};
    message.grantee !== undefined && (obj.grantee = message.grantee);
    if (message.msgs) {
      obj.msgs = message.msgs.map((e) => (e ? Any.toJSON(e) : undefined));
    } else {
      obj.msgs = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<MsgExec>): MsgExec {
    const message = { ...baseMsgExec } as MsgExec;
    message.msgs = [];
    if (object.grantee !== undefined && object.grantee !== null) {
      message.grantee = object.grantee;
    } else {
      message.grantee = "";
    }
    if (object.msgs !== undefined && object.msgs !== null) {
      for (const e of object.msgs) {
        message.msgs.push(Any.fromPartial(e));
      }
    }
    return message;
  },
};

const baseMsgGrantResponse: object = {};

export const MsgGrantResponse = {
  encode(_: MsgGrantResponse, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgGrantResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgGrantResponse } as MsgGrantResponse;
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

  fromJSON(_: any): MsgGrantResponse {
    const message = { ...baseMsgGrantResponse } as MsgGrantResponse;
    return message;
  },

  toJSON(_: MsgGrantResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(_: DeepPartial<MsgGrantResponse>): MsgGrantResponse {
    const message = { ...baseMsgGrantResponse } as MsgGrantResponse;
    return message;
  },
};

const baseMsgRevoke: object = { granter: "", grantee: "", msgTypeUrl: "" };

export const MsgRevoke = {
  encode(message: MsgRevoke, writer: Writer = Writer.create()): Writer {
    if (message.granter !== "") {
      writer.uint32(10).string(message.granter);
    }
    if (message.grantee !== "") {
      writer.uint32(18).string(message.grantee);
    }
    if (message.msgTypeUrl !== "") {
      writer.uint32(26).string(message.msgTypeUrl);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgRevoke {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgRevoke } as MsgRevoke;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.granter = reader.string();
          break;
        case 2:
          message.grantee = reader.string();
          break;
        case 3:
          message.msgTypeUrl = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgRevoke {
    const message = { ...baseMsgRevoke } as MsgRevoke;
    if (object.granter !== undefined && object.granter !== null) {
      message.granter = String(object.granter);
    } else {
      message.granter = "";
    }
    if (object.grantee !== undefined && object.grantee !== null) {
      message.grantee = String(object.grantee);
    } else {
      message.grantee = "";
    }
    if (object.msgTypeUrl !== undefined && object.msgTypeUrl !== null) {
      message.msgTypeUrl = String(object.msgTypeUrl);
    } else {
      message.msgTypeUrl = "";
    }
    return message;
  },

  toJSON(message: MsgRevoke): unknown {
    const obj: any = {};
    message.granter !== undefined && (obj.granter = message.granter);
    message.grantee !== undefined && (obj.grantee = message.grantee);
    message.msgTypeUrl !== undefined && (obj.msgTypeUrl = message.msgTypeUrl);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgRevoke>): MsgRevoke {
    const message = { ...baseMsgRevoke } as MsgRevoke;
    if (object.granter !== undefined && object.granter !== null) {
      message.granter = object.granter;
    } else {
      message.granter = "";
    }
    if (object.grantee !== undefined && object.grantee !== null) {
      message.grantee = object.grantee;
    } else {
      message.grantee = "";
    }
    if (object.msgTypeUrl !== undefined && object.msgTypeUrl !== null) {
      message.msgTypeUrl = object.msgTypeUrl;
    } else {
      message.msgTypeUrl = "";
    }
    return message;
  },
};

const baseMsgRevokeResponse: object = {};

export const MsgRevokeResponse = {
  encode(_: MsgRevokeResponse, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgRevokeResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgRevokeResponse } as MsgRevokeResponse;
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

  fromJSON(_: any): MsgRevokeResponse {
    const message = { ...baseMsgRevokeResponse } as MsgRevokeResponse;
    return message;
  },

  toJSON(_: MsgRevokeResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(_: DeepPartial<MsgRevokeResponse>): MsgRevokeResponse {
    const message = { ...baseMsgRevokeResponse } as MsgRevokeResponse;
    return message;
  },
};

/** Msg defines the authz Msg service. */
export interface Msg {
  /**
   * Grant grants the provided authorization to the grantee on the granter's
   * account with the provided expiration time. If there is already a grant
   * for the given (granter, grantee, Authorization) triple, then the grant
   * will be overwritten.
   */
  Grant(request: MsgGrant): Promise<MsgGrantResponse>;
  /**
   * Exec attempts to execute the provided messages using
   * authorizations granted to the grantee. Each message should have only
   * one signer corresponding to the granter of the authorization.
   */
  Exec(request: MsgExec): Promise<MsgExecResponse>;
  /**
   * Revoke revokes any authorization corresponding to the provided method name on the
   * granter's account that has been granted to the grantee.
   */
  Revoke(request: MsgRevoke): Promise<MsgRevokeResponse>;
}

export class MsgClientImpl implements Msg {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  Grant(request: MsgGrant): Promise<MsgGrantResponse> {
    const data = MsgGrant.encode(request).finish();
    const promise = this.rpc.request("cosmos.authz.v1beta1.Msg", "Grant", data);
    return promise.then((data) => MsgGrantResponse.decode(new Reader(data)));
  }

  Exec(request: MsgExec): Promise<MsgExecResponse> {
    const data = MsgExec.encode(request).finish();
    const promise = this.rpc.request("cosmos.authz.v1beta1.Msg", "Exec", data);
    return promise.then((data) => MsgExecResponse.decode(new Reader(data)));
  }

  Revoke(request: MsgRevoke): Promise<MsgRevokeResponse> {
    const data = MsgRevoke.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.authz.v1beta1.Msg",
      "Revoke",
      data
    );
    return promise.then((data) => MsgRevokeResponse.decode(new Reader(data)));
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
