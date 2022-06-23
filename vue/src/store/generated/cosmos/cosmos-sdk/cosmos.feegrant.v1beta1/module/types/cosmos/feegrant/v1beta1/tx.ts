/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Any } from "../../../google/protobuf/any";

export const protobufPackage = "cosmos.feegrant.v1beta1";

/** Since: cosmos-sdk 0.43 */

/**
 * MsgGrantAllowance adds permission for Grantee to spend up to Allowance
 * of fees from the account of Granter.
 */
export interface MsgGrantAllowance {
  /** granter is the address of the user granting an allowance of their funds. */
  granter: string;
  /** grantee is the address of the user being granted an allowance of another user's funds. */
  grantee: string;
  /** allowance can be any of basic and filtered fee allowance. */
  allowance: Any | undefined;
}

/** MsgGrantAllowanceResponse defines the Msg/GrantAllowanceResponse response type. */
export interface MsgGrantAllowanceResponse {}

/** MsgRevokeAllowance removes any existing Allowance from Granter to Grantee. */
export interface MsgRevokeAllowance {
  /** granter is the address of the user granting an allowance of their funds. */
  granter: string;
  /** grantee is the address of the user being granted an allowance of another user's funds. */
  grantee: string;
}

/** MsgRevokeAllowanceResponse defines the Msg/RevokeAllowanceResponse response type. */
export interface MsgRevokeAllowanceResponse {}

const baseMsgGrantAllowance: object = { granter: "", grantee: "" };

export const MsgGrantAllowance = {
  encode(message: MsgGrantAllowance, writer: Writer = Writer.create()): Writer {
    if (message.granter !== "") {
      writer.uint32(10).string(message.granter);
    }
    if (message.grantee !== "") {
      writer.uint32(18).string(message.grantee);
    }
    if (message.allowance !== undefined) {
      Any.encode(message.allowance, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgGrantAllowance {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgGrantAllowance } as MsgGrantAllowance;
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
          message.allowance = Any.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgGrantAllowance {
    const message = { ...baseMsgGrantAllowance } as MsgGrantAllowance;
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
    if (object.allowance !== undefined && object.allowance !== null) {
      message.allowance = Any.fromJSON(object.allowance);
    } else {
      message.allowance = undefined;
    }
    return message;
  },

  toJSON(message: MsgGrantAllowance): unknown {
    const obj: any = {};
    message.granter !== undefined && (obj.granter = message.granter);
    message.grantee !== undefined && (obj.grantee = message.grantee);
    message.allowance !== undefined &&
      (obj.allowance = message.allowance
        ? Any.toJSON(message.allowance)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgGrantAllowance>): MsgGrantAllowance {
    const message = { ...baseMsgGrantAllowance } as MsgGrantAllowance;
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
    if (object.allowance !== undefined && object.allowance !== null) {
      message.allowance = Any.fromPartial(object.allowance);
    } else {
      message.allowance = undefined;
    }
    return message;
  },
};

const baseMsgGrantAllowanceResponse: object = {};

export const MsgGrantAllowanceResponse = {
  encode(
    _: MsgGrantAllowanceResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgGrantAllowanceResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgGrantAllowanceResponse,
    } as MsgGrantAllowanceResponse;
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

  fromJSON(_: any): MsgGrantAllowanceResponse {
    const message = {
      ...baseMsgGrantAllowanceResponse,
    } as MsgGrantAllowanceResponse;
    return message;
  },

  toJSON(_: MsgGrantAllowanceResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgGrantAllowanceResponse>
  ): MsgGrantAllowanceResponse {
    const message = {
      ...baseMsgGrantAllowanceResponse,
    } as MsgGrantAllowanceResponse;
    return message;
  },
};

const baseMsgRevokeAllowance: object = { granter: "", grantee: "" };

export const MsgRevokeAllowance = {
  encode(
    message: MsgRevokeAllowance,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.granter !== "") {
      writer.uint32(10).string(message.granter);
    }
    if (message.grantee !== "") {
      writer.uint32(18).string(message.grantee);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgRevokeAllowance {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgRevokeAllowance } as MsgRevokeAllowance;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.granter = reader.string();
          break;
        case 2:
          message.grantee = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgRevokeAllowance {
    const message = { ...baseMsgRevokeAllowance } as MsgRevokeAllowance;
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
    return message;
  },

  toJSON(message: MsgRevokeAllowance): unknown {
    const obj: any = {};
    message.granter !== undefined && (obj.granter = message.granter);
    message.grantee !== undefined && (obj.grantee = message.grantee);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgRevokeAllowance>): MsgRevokeAllowance {
    const message = { ...baseMsgRevokeAllowance } as MsgRevokeAllowance;
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
    return message;
  },
};

const baseMsgRevokeAllowanceResponse: object = {};

export const MsgRevokeAllowanceResponse = {
  encode(
    _: MsgRevokeAllowanceResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgRevokeAllowanceResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgRevokeAllowanceResponse,
    } as MsgRevokeAllowanceResponse;
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

  fromJSON(_: any): MsgRevokeAllowanceResponse {
    const message = {
      ...baseMsgRevokeAllowanceResponse,
    } as MsgRevokeAllowanceResponse;
    return message;
  },

  toJSON(_: MsgRevokeAllowanceResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgRevokeAllowanceResponse>
  ): MsgRevokeAllowanceResponse {
    const message = {
      ...baseMsgRevokeAllowanceResponse,
    } as MsgRevokeAllowanceResponse;
    return message;
  },
};

/** Msg defines the feegrant msg service. */
export interface Msg {
  /**
   * GrantAllowance grants fee allowance to the grantee on the granter's
   * account with the provided expiration time.
   */
  GrantAllowance(
    request: MsgGrantAllowance
  ): Promise<MsgGrantAllowanceResponse>;
  /**
   * RevokeAllowance revokes any fee allowance of granter's account that
   * has been granted to the grantee.
   */
  RevokeAllowance(
    request: MsgRevokeAllowance
  ): Promise<MsgRevokeAllowanceResponse>;
}

export class MsgClientImpl implements Msg {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  GrantAllowance(
    request: MsgGrantAllowance
  ): Promise<MsgGrantAllowanceResponse> {
    const data = MsgGrantAllowance.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.feegrant.v1beta1.Msg",
      "GrantAllowance",
      data
    );
    return promise.then((data) =>
      MsgGrantAllowanceResponse.decode(new Reader(data))
    );
  }

  RevokeAllowance(
    request: MsgRevokeAllowance
  ): Promise<MsgRevokeAllowanceResponse> {
    const data = MsgRevokeAllowance.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.feegrant.v1beta1.Msg",
      "RevokeAllowance",
      data
    );
    return promise.then((data) =>
      MsgRevokeAllowanceResponse.decode(new Reader(data))
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
