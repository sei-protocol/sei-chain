/* eslint-disable */
import {
  MessageDependencyMapping,
  WasmDependencyMapping,
} from "../../cosmos/accesscontrol/accesscontrol";
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "cosmos.accesscontrol.v1beta1";

export interface MsgUpdateResourceDependencyMappingProposal {
  title: string;
  description: string;
  messageDependencyMapping: MessageDependencyMapping[];
}

export interface MsgUpdateResourceDependencyMappingProposalJsonFile {
  title: string;
  description: string;
  deposit: string;
  messageDependencyMapping: MessageDependencyMapping[];
}

export interface MsgUpdateResourceDependencyMappingProposalResponse {}

export interface MsgUpdateWasmDependencyMappingProposal {
  title: string;
  description: string;
  contractAddress: string;
  wasmDependencyMapping: WasmDependencyMapping | undefined;
}

export interface MsgUpdateWasmDependencyMappingProposalJsonFile {
  title: string;
  description: string;
  deposit: string;
  contractAddress: string;
  wasmDependencyMapping: WasmDependencyMapping | undefined;
}

const baseMsgUpdateResourceDependencyMappingProposal: object = {
  title: "",
  description: "",
};

export const MsgUpdateResourceDependencyMappingProposal = {
  encode(
    message: MsgUpdateResourceDependencyMappingProposal,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    for (const v of message.messageDependencyMapping) {
      MessageDependencyMapping.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgUpdateResourceDependencyMappingProposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgUpdateResourceDependencyMappingProposal,
    } as MsgUpdateResourceDependencyMappingProposal;
    message.messageDependencyMapping = [];
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
          message.messageDependencyMapping.push(
            MessageDependencyMapping.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgUpdateResourceDependencyMappingProposal {
    const message = {
      ...baseMsgUpdateResourceDependencyMappingProposal,
    } as MsgUpdateResourceDependencyMappingProposal;
    message.messageDependencyMapping = [];
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
      object.messageDependencyMapping !== undefined &&
      object.messageDependencyMapping !== null
    ) {
      for (const e of object.messageDependencyMapping) {
        message.messageDependencyMapping.push(
          MessageDependencyMapping.fromJSON(e)
        );
      }
    }
    return message;
  },

  toJSON(message: MsgUpdateResourceDependencyMappingProposal): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    if (message.messageDependencyMapping) {
      obj.messageDependencyMapping = message.messageDependencyMapping.map((e) =>
        e ? MessageDependencyMapping.toJSON(e) : undefined
      );
    } else {
      obj.messageDependencyMapping = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgUpdateResourceDependencyMappingProposal>
  ): MsgUpdateResourceDependencyMappingProposal {
    const message = {
      ...baseMsgUpdateResourceDependencyMappingProposal,
    } as MsgUpdateResourceDependencyMappingProposal;
    message.messageDependencyMapping = [];
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
      object.messageDependencyMapping !== undefined &&
      object.messageDependencyMapping !== null
    ) {
      for (const e of object.messageDependencyMapping) {
        message.messageDependencyMapping.push(
          MessageDependencyMapping.fromPartial(e)
        );
      }
    }
    return message;
  },
};

const baseMsgUpdateResourceDependencyMappingProposalJsonFile: object = {
  title: "",
  description: "",
  deposit: "",
};

export const MsgUpdateResourceDependencyMappingProposalJsonFile = {
  encode(
    message: MsgUpdateResourceDependencyMappingProposalJsonFile,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    if (message.deposit !== "") {
      writer.uint32(26).string(message.deposit);
    }
    for (const v of message.messageDependencyMapping) {
      MessageDependencyMapping.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgUpdateResourceDependencyMappingProposalJsonFile {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgUpdateResourceDependencyMappingProposalJsonFile,
    } as MsgUpdateResourceDependencyMappingProposalJsonFile;
    message.messageDependencyMapping = [];
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
          message.deposit = reader.string();
          break;
        case 4:
          message.messageDependencyMapping.push(
            MessageDependencyMapping.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgUpdateResourceDependencyMappingProposalJsonFile {
    const message = {
      ...baseMsgUpdateResourceDependencyMappingProposalJsonFile,
    } as MsgUpdateResourceDependencyMappingProposalJsonFile;
    message.messageDependencyMapping = [];
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
    if (object.deposit !== undefined && object.deposit !== null) {
      message.deposit = String(object.deposit);
    } else {
      message.deposit = "";
    }
    if (
      object.messageDependencyMapping !== undefined &&
      object.messageDependencyMapping !== null
    ) {
      for (const e of object.messageDependencyMapping) {
        message.messageDependencyMapping.push(
          MessageDependencyMapping.fromJSON(e)
        );
      }
    }
    return message;
  },

  toJSON(message: MsgUpdateResourceDependencyMappingProposalJsonFile): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    message.deposit !== undefined && (obj.deposit = message.deposit);
    if (message.messageDependencyMapping) {
      obj.messageDependencyMapping = message.messageDependencyMapping.map((e) =>
        e ? MessageDependencyMapping.toJSON(e) : undefined
      );
    } else {
      obj.messageDependencyMapping = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgUpdateResourceDependencyMappingProposalJsonFile>
  ): MsgUpdateResourceDependencyMappingProposalJsonFile {
    const message = {
      ...baseMsgUpdateResourceDependencyMappingProposalJsonFile,
    } as MsgUpdateResourceDependencyMappingProposalJsonFile;
    message.messageDependencyMapping = [];
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
    if (object.deposit !== undefined && object.deposit !== null) {
      message.deposit = object.deposit;
    } else {
      message.deposit = "";
    }
    if (
      object.messageDependencyMapping !== undefined &&
      object.messageDependencyMapping !== null
    ) {
      for (const e of object.messageDependencyMapping) {
        message.messageDependencyMapping.push(
          MessageDependencyMapping.fromPartial(e)
        );
      }
    }
    return message;
  },
};

const baseMsgUpdateResourceDependencyMappingProposalResponse: object = {};

export const MsgUpdateResourceDependencyMappingProposalResponse = {
  encode(
    _: MsgUpdateResourceDependencyMappingProposalResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgUpdateResourceDependencyMappingProposalResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgUpdateResourceDependencyMappingProposalResponse,
    } as MsgUpdateResourceDependencyMappingProposalResponse;
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

  fromJSON(_: any): MsgUpdateResourceDependencyMappingProposalResponse {
    const message = {
      ...baseMsgUpdateResourceDependencyMappingProposalResponse,
    } as MsgUpdateResourceDependencyMappingProposalResponse;
    return message;
  },

  toJSON(_: MsgUpdateResourceDependencyMappingProposalResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgUpdateResourceDependencyMappingProposalResponse>
  ): MsgUpdateResourceDependencyMappingProposalResponse {
    const message = {
      ...baseMsgUpdateResourceDependencyMappingProposalResponse,
    } as MsgUpdateResourceDependencyMappingProposalResponse;
    return message;
  },
};

const baseMsgUpdateWasmDependencyMappingProposal: object = {
  title: "",
  description: "",
  contractAddress: "",
};

export const MsgUpdateWasmDependencyMappingProposal = {
  encode(
    message: MsgUpdateWasmDependencyMappingProposal,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    if (message.contractAddress !== "") {
      writer.uint32(26).string(message.contractAddress);
    }
    if (message.wasmDependencyMapping !== undefined) {
      WasmDependencyMapping.encode(
        message.wasmDependencyMapping,
        writer.uint32(34).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgUpdateWasmDependencyMappingProposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgUpdateWasmDependencyMappingProposal,
    } as MsgUpdateWasmDependencyMappingProposal;
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
          message.contractAddress = reader.string();
          break;
        case 4:
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

  fromJSON(object: any): MsgUpdateWasmDependencyMappingProposal {
    const message = {
      ...baseMsgUpdateWasmDependencyMappingProposal,
    } as MsgUpdateWasmDependencyMappingProposal;
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
      object.contractAddress !== undefined &&
      object.contractAddress !== null
    ) {
      message.contractAddress = String(object.contractAddress);
    } else {
      message.contractAddress = "";
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

  toJSON(message: MsgUpdateWasmDependencyMappingProposal): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    message.contractAddress !== undefined &&
      (obj.contractAddress = message.contractAddress);
    message.wasmDependencyMapping !== undefined &&
      (obj.wasmDependencyMapping = message.wasmDependencyMapping
        ? WasmDependencyMapping.toJSON(message.wasmDependencyMapping)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgUpdateWasmDependencyMappingProposal>
  ): MsgUpdateWasmDependencyMappingProposal {
    const message = {
      ...baseMsgUpdateWasmDependencyMappingProposal,
    } as MsgUpdateWasmDependencyMappingProposal;
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
      object.contractAddress !== undefined &&
      object.contractAddress !== null
    ) {
      message.contractAddress = object.contractAddress;
    } else {
      message.contractAddress = "";
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

const baseMsgUpdateWasmDependencyMappingProposalJsonFile: object = {
  title: "",
  description: "",
  deposit: "",
  contractAddress: "",
};

export const MsgUpdateWasmDependencyMappingProposalJsonFile = {
  encode(
    message: MsgUpdateWasmDependencyMappingProposalJsonFile,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    if (message.deposit !== "") {
      writer.uint32(26).string(message.deposit);
    }
    if (message.contractAddress !== "") {
      writer.uint32(34).string(message.contractAddress);
    }
    if (message.wasmDependencyMapping !== undefined) {
      WasmDependencyMapping.encode(
        message.wasmDependencyMapping,
        writer.uint32(42).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgUpdateWasmDependencyMappingProposalJsonFile {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgUpdateWasmDependencyMappingProposalJsonFile,
    } as MsgUpdateWasmDependencyMappingProposalJsonFile;
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
          message.deposit = reader.string();
          break;
        case 4:
          message.contractAddress = reader.string();
          break;
        case 5:
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

  fromJSON(object: any): MsgUpdateWasmDependencyMappingProposalJsonFile {
    const message = {
      ...baseMsgUpdateWasmDependencyMappingProposalJsonFile,
    } as MsgUpdateWasmDependencyMappingProposalJsonFile;
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
    if (object.deposit !== undefined && object.deposit !== null) {
      message.deposit = String(object.deposit);
    } else {
      message.deposit = "";
    }
    if (
      object.contractAddress !== undefined &&
      object.contractAddress !== null
    ) {
      message.contractAddress = String(object.contractAddress);
    } else {
      message.contractAddress = "";
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

  toJSON(message: MsgUpdateWasmDependencyMappingProposalJsonFile): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    message.deposit !== undefined && (obj.deposit = message.deposit);
    message.contractAddress !== undefined &&
      (obj.contractAddress = message.contractAddress);
    message.wasmDependencyMapping !== undefined &&
      (obj.wasmDependencyMapping = message.wasmDependencyMapping
        ? WasmDependencyMapping.toJSON(message.wasmDependencyMapping)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgUpdateWasmDependencyMappingProposalJsonFile>
  ): MsgUpdateWasmDependencyMappingProposalJsonFile {
    const message = {
      ...baseMsgUpdateWasmDependencyMappingProposalJsonFile,
    } as MsgUpdateWasmDependencyMappingProposalJsonFile;
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
    if (object.deposit !== undefined && object.deposit !== null) {
      message.deposit = object.deposit;
    } else {
      message.deposit = "";
    }
    if (
      object.contractAddress !== undefined &&
      object.contractAddress !== null
    ) {
      message.contractAddress = object.contractAddress;
    } else {
      message.contractAddress = "";
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
