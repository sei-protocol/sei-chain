/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";

export const protobufPackage = "cosmos.crisis.v1beta1";

/** MsgVerifyInvariant represents a message to verify a particular invariance. */
export interface MsgVerifyInvariant {
  sender: string;
  invariantModuleName: string;
  invariantRoute: string;
}

/** MsgVerifyInvariantResponse defines the Msg/VerifyInvariant response type. */
export interface MsgVerifyInvariantResponse {}

const baseMsgVerifyInvariant: object = {
  sender: "",
  invariantModuleName: "",
  invariantRoute: "",
};

export const MsgVerifyInvariant = {
  encode(
    message: MsgVerifyInvariant,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.sender !== "") {
      writer.uint32(10).string(message.sender);
    }
    if (message.invariantModuleName !== "") {
      writer.uint32(18).string(message.invariantModuleName);
    }
    if (message.invariantRoute !== "") {
      writer.uint32(26).string(message.invariantRoute);
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
          message.invariantModuleName = reader.string();
          break;
        case 3:
          message.invariantRoute = reader.string();
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
      object.invariantModuleName !== undefined &&
      object.invariantModuleName !== null
    ) {
      message.invariantModuleName = String(object.invariantModuleName);
    } else {
      message.invariantModuleName = "";
    }
    if (object.invariantRoute !== undefined && object.invariantRoute !== null) {
      message.invariantRoute = String(object.invariantRoute);
    } else {
      message.invariantRoute = "";
    }
    return message;
  },

  toJSON(message: MsgVerifyInvariant): unknown {
    const obj: any = {};
    message.sender !== undefined && (obj.sender = message.sender);
    message.invariantModuleName !== undefined &&
      (obj.invariantModuleName = message.invariantModuleName);
    message.invariantRoute !== undefined &&
      (obj.invariantRoute = message.invariantRoute);
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
      object.invariantModuleName !== undefined &&
      object.invariantModuleName !== null
    ) {
      message.invariantModuleName = object.invariantModuleName;
    } else {
      message.invariantModuleName = "";
    }
    if (object.invariantRoute !== undefined && object.invariantRoute !== null) {
      message.invariantRoute = object.invariantRoute;
    } else {
      message.invariantRoute = "";
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
