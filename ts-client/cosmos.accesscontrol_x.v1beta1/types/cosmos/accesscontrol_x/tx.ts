/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { WasmDependencyMapping } from "../../cosmos/accesscontrol/accesscontrol";

export const protobufPackage = "cosmos.accesscontrol_x.v1beta1";

export interface RegisterWasmDependencyJSONFile {
  wasmDependencyMapping: WasmDependencyMapping | undefined;
}

export interface MsgRegisterWasmDependency {
  fromAddress: string;
  wasmDependencyMapping: WasmDependencyMapping | undefined;
}

export interface MsgRegisterWasmDependencyResponse {}

const baseRegisterWasmDependencyJSONFile: object = {};

export const RegisterWasmDependencyJSONFile = {
  encode(
    message: RegisterWasmDependencyJSONFile,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.wasmDependencyMapping !== undefined) {
      WasmDependencyMapping.encode(
        message.wasmDependencyMapping,
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): RegisterWasmDependencyJSONFile {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseRegisterWasmDependencyJSONFile,
    } as RegisterWasmDependencyJSONFile;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.wasmDependencyMapping = WasmDependencyMapping.decode(
            reader,
            reader.uint32()
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): RegisterWasmDependencyJSONFile {
    const message = {
      ...baseRegisterWasmDependencyJSONFile,
    } as RegisterWasmDependencyJSONFile;
    if (
      object.wasmDependencyMapping !== undefined &&
      object.wasmDependencyMapping !== null
    ) {
      message.wasmDependencyMapping = WasmDependencyMapping.fromJSON(
        object.wasmDependencyMapping
      );
    } else {
      message.wasmDependencyMapping = undefined;
    }
    return message;
  },

  toJSON(message: RegisterWasmDependencyJSONFile): unknown {
    const obj: any = {};
    message.wasmDependencyMapping !== undefined &&
      (obj.wasmDependencyMapping = message.wasmDependencyMapping
        ? WasmDependencyMapping.toJSON(message.wasmDependencyMapping)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<RegisterWasmDependencyJSONFile>
  ): RegisterWasmDependencyJSONFile {
    const message = {
      ...baseRegisterWasmDependencyJSONFile,
    } as RegisterWasmDependencyJSONFile;
    if (
      object.wasmDependencyMapping !== undefined &&
      object.wasmDependencyMapping !== null
    ) {
      message.wasmDependencyMapping = WasmDependencyMapping.fromPartial(
        object.wasmDependencyMapping
      );
    } else {
      message.wasmDependencyMapping = undefined;
    }
    return message;
  },
};

const baseMsgRegisterWasmDependency: object = { fromAddress: "" };

export const MsgRegisterWasmDependency = {
  encode(
    message: MsgRegisterWasmDependency,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.fromAddress !== "") {
      writer.uint32(10).string(message.fromAddress);
    }
    if (message.wasmDependencyMapping !== undefined) {
      WasmDependencyMapping.encode(
        message.wasmDependencyMapping,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgRegisterWasmDependency {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgRegisterWasmDependency,
    } as MsgRegisterWasmDependency;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.fromAddress = reader.string();
          break;
        case 2:
          message.wasmDependencyMapping = WasmDependencyMapping.decode(
            reader,
            reader.uint32()
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgRegisterWasmDependency {
    const message = {
      ...baseMsgRegisterWasmDependency,
    } as MsgRegisterWasmDependency;
    if (object.fromAddress !== undefined && object.fromAddress !== null) {
      message.fromAddress = String(object.fromAddress);
    } else {
      message.fromAddress = "";
    }
    if (
      object.wasmDependencyMapping !== undefined &&
      object.wasmDependencyMapping !== null
    ) {
      message.wasmDependencyMapping = WasmDependencyMapping.fromJSON(
        object.wasmDependencyMapping
      );
    } else {
      message.wasmDependencyMapping = undefined;
    }
    return message;
  },

  toJSON(message: MsgRegisterWasmDependency): unknown {
    const obj: any = {};
    message.fromAddress !== undefined &&
      (obj.fromAddress = message.fromAddress);
    message.wasmDependencyMapping !== undefined &&
      (obj.wasmDependencyMapping = message.wasmDependencyMapping
        ? WasmDependencyMapping.toJSON(message.wasmDependencyMapping)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgRegisterWasmDependency>
  ): MsgRegisterWasmDependency {
    const message = {
      ...baseMsgRegisterWasmDependency,
    } as MsgRegisterWasmDependency;
    if (object.fromAddress !== undefined && object.fromAddress !== null) {
      message.fromAddress = object.fromAddress;
    } else {
      message.fromAddress = "";
    }
    if (
      object.wasmDependencyMapping !== undefined &&
      object.wasmDependencyMapping !== null
    ) {
      message.wasmDependencyMapping = WasmDependencyMapping.fromPartial(
        object.wasmDependencyMapping
      );
    } else {
      message.wasmDependencyMapping = undefined;
    }
    return message;
  },
};

const baseMsgRegisterWasmDependencyResponse: object = {};

export const MsgRegisterWasmDependencyResponse = {
  encode(
    _: MsgRegisterWasmDependencyResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgRegisterWasmDependencyResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgRegisterWasmDependencyResponse,
    } as MsgRegisterWasmDependencyResponse;
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

  fromJSON(_: any): MsgRegisterWasmDependencyResponse {
    const message = {
      ...baseMsgRegisterWasmDependencyResponse,
    } as MsgRegisterWasmDependencyResponse;
    return message;
  },

  toJSON(_: MsgRegisterWasmDependencyResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgRegisterWasmDependencyResponse>
  ): MsgRegisterWasmDependencyResponse {
    const message = {
      ...baseMsgRegisterWasmDependencyResponse,
    } as MsgRegisterWasmDependencyResponse;
    return message;
  },
};

export interface Msg {
  RegisterWasmDependency(
    request: MsgRegisterWasmDependency
  ): Promise<MsgRegisterWasmDependencyResponse>;
}

export class MsgClientImpl implements Msg {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  RegisterWasmDependency(
    request: MsgRegisterWasmDependency
  ): Promise<MsgRegisterWasmDependencyResponse> {
    const data = MsgRegisterWasmDependency.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.accesscontrol_x.v1beta1.Msg",
      "RegisterWasmDependency",
      data
    );
    return promise.then((data) =>
      MsgRegisterWasmDependencyResponse.decode(new Reader(data))
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
