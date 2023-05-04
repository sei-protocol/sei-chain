/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";

export const protobufPackage = "cosmos.crisis.v1beta1";

/** MsgVerifyInvariant represents a message to verify a particular invariance. */
export interface MsgVerifyInvariant {
  sender: string;
  invariant_module_name: string;
  invariant_route: string;
}

/** MsgVerifyInvariantResponse defines the Msg/VerifyInvariant response type. */
export interface MsgVerifyInvariantResponse {}

const baseMsgVerifyInvariant: object = {
  sender: "",
  invariant_module_name: "",
  invariant_route: "",
};

export const MsgVerifyInvariant = {
  encode(
    message: MsgVerifyInvariant,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.sender !== "") {
      writer.uint32(10).string(message.sender);
    }
    if (message.invariant_module_name !== "") {
      writer.uint32(18).string(message.invariant_module_name);
    }
    if (message.invariant_route !== "") {
      writer.uint32(26).string(message.invariant_route);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgVerifyInvariant {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgVerifyInvariant } as MsgVerifyInvariant;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.sender = reader.string();
          break;
        case 2:
          message.invariant_module_name = reader.string();
          break;
        case 3:
          message.invariant_route = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgVerifyInvariant {
    const message = { ...baseMsgVerifyInvariant } as MsgVerifyInvariant;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = String(object.sender);
    } else {
      message.sender = "";
    }
    if (
      object.invariant_module_name !== undefined &&
      object.invariant_module_name !== null
    ) {
      message.invariant_module_name = String(object.invariant_module_name);
    } else {
      message.invariant_module_name = "";
    }
    if (
      object.invariant_route !== undefined &&
      object.invariant_route !== null
    ) {
      message.invariant_route = String(object.invariant_route);
    } else {
      message.invariant_route = "";
    }
    return message;
  },

  toJSON(message: MsgVerifyInvariant): unknown {
    const obj: any = {};
    message.sender !== undefined && (obj.sender = message.sender);
    message.invariant_module_name !== undefined &&
      (obj.invariant_module_name = message.invariant_module_name);
    message.invariant_route !== undefined &&
      (obj.invariant_route = message.invariant_route);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgVerifyInvariant>): MsgVerifyInvariant {
    const message = { ...baseMsgVerifyInvariant } as MsgVerifyInvariant;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = object.sender;
    } else {
      message.sender = "";
    }
    if (
      object.invariant_module_name !== undefined &&
      object.invariant_module_name !== null
    ) {
      message.invariant_module_name = object.invariant_module_name;
    } else {
      message.invariant_module_name = "";
    }
    if (
      object.invariant_route !== undefined &&
      object.invariant_route !== null
    ) {
      message.invariant_route = object.invariant_route;
    } else {
      message.invariant_route = "";
    }
    return message;
  },
};

const baseMsgVerifyInvariantResponse: object = {};

export const MsgVerifyInvariantResponse = {
  encode(
    _: MsgVerifyInvariantResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgVerifyInvariantResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgVerifyInvariantResponse,
    } as MsgVerifyInvariantResponse;
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

  fromJSON(_: any): MsgVerifyInvariantResponse {
    const message = {
      ...baseMsgVerifyInvariantResponse,
    } as MsgVerifyInvariantResponse;
    return message;
  },

  toJSON(_: MsgVerifyInvariantResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgVerifyInvariantResponse>
  ): MsgVerifyInvariantResponse {
    const message = {
      ...baseMsgVerifyInvariantResponse,
    } as MsgVerifyInvariantResponse;
    return message;
  },
};

/** Msg defines the bank Msg service. */
export interface Msg {
  /** VerifyInvariant defines a method to verify a particular invariance. */
  VerifyInvariant(
    request: MsgVerifyInvariant
  ): Promise<MsgVerifyInvariantResponse>;
}

export class MsgClientImpl implements Msg {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  VerifyInvariant(
    request: MsgVerifyInvariant
  ): Promise<MsgVerifyInvariantResponse> {
    const data = MsgVerifyInvariant.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.crisis.v1beta1.Msg",
      "VerifyInvariant",
      data
    );
    return promise.then((data) =>
      MsgVerifyInvariantResponse.decode(new Reader(data))
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
