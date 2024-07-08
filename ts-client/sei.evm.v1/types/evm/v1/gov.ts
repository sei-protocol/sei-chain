/* eslint-disable */
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "sei.evm.v1";

export interface AddERCNativePointerProposal {
  title: string;
  description: string;
  token: string;
  pointer: string;
  version: number;
}

export interface AddERCCW20PointerProposal {
  title: string;
  description: string;
  pointee: string;
  pointer: string;
  version: number;
}

export interface AddERCCW721PointerProposal {
  title: string;
  description: string;
  pointee: string;
  pointer: string;
  version: number;
}

export interface AddCWERC20PointerProposal {
  title: string;
  description: string;
  pointee: string;
  pointer: string;
  version: number;
}

export interface AddCWERC721PointerProposal {
  title: string;
  description: string;
  pointee: string;
  pointer: string;
  version: number;
}

export interface AddERCNativePointerProposalV2 {
  title: string;
  description: string;
  token: string;
  name: string;
  symbol: string;
  decimals: number;
}

const baseAddERCNativePointerProposal: object = {
  title: "",
  description: "",
  token: "",
  pointer: "",
  version: 0,
};

export const AddERCNativePointerProposal = {
  encode(
    message: AddERCNativePointerProposal,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    if (message.token !== "") {
      writer.uint32(26).string(message.token);
    }
    if (message.pointer !== "") {
      writer.uint32(34).string(message.pointer);
    }
    if (message.version !== 0) {
      writer.uint32(40).uint32(message.version);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): AddERCNativePointerProposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseAddERCNativePointerProposal,
    } as AddERCNativePointerProposal;
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
          message.token = reader.string();
          break;
        case 4:
          message.pointer = reader.string();
          break;
        case 5:
          message.version = reader.uint32();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): AddERCNativePointerProposal {
    const message = {
      ...baseAddERCNativePointerProposal,
    } as AddERCNativePointerProposal;
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
    if (object.token !== undefined && object.token !== null) {
      message.token = String(object.token);
    } else {
      message.token = "";
    }
    if (object.pointer !== undefined && object.pointer !== null) {
      message.pointer = String(object.pointer);
    } else {
      message.pointer = "";
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = Number(object.version);
    } else {
      message.version = 0;
    }
    return message;
  },

  toJSON(message: AddERCNativePointerProposal): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    message.token !== undefined && (obj.token = message.token);
    message.pointer !== undefined && (obj.pointer = message.pointer);
    message.version !== undefined && (obj.version = message.version);
    return obj;
  },

  fromPartial(
    object: DeepPartial<AddERCNativePointerProposal>
  ): AddERCNativePointerProposal {
    const message = {
      ...baseAddERCNativePointerProposal,
    } as AddERCNativePointerProposal;
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
    if (object.token !== undefined && object.token !== null) {
      message.token = object.token;
    } else {
      message.token = "";
    }
    if (object.pointer !== undefined && object.pointer !== null) {
      message.pointer = object.pointer;
    } else {
      message.pointer = "";
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = object.version;
    } else {
      message.version = 0;
    }
    return message;
  },
};

const baseAddERCCW20PointerProposal: object = {
  title: "",
  description: "",
  pointee: "",
  pointer: "",
  version: 0,
};

export const AddERCCW20PointerProposal = {
  encode(
    message: AddERCCW20PointerProposal,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    if (message.pointee !== "") {
      writer.uint32(26).string(message.pointee);
    }
    if (message.pointer !== "") {
      writer.uint32(34).string(message.pointer);
    }
    if (message.version !== 0) {
      writer.uint32(40).uint32(message.version);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): AddERCCW20PointerProposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseAddERCCW20PointerProposal,
    } as AddERCCW20PointerProposal;
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
          message.pointee = reader.string();
          break;
        case 4:
          message.pointer = reader.string();
          break;
        case 5:
          message.version = reader.uint32();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): AddERCCW20PointerProposal {
    const message = {
      ...baseAddERCCW20PointerProposal,
    } as AddERCCW20PointerProposal;
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
    if (object.pointee !== undefined && object.pointee !== null) {
      message.pointee = String(object.pointee);
    } else {
      message.pointee = "";
    }
    if (object.pointer !== undefined && object.pointer !== null) {
      message.pointer = String(object.pointer);
    } else {
      message.pointer = "";
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = Number(object.version);
    } else {
      message.version = 0;
    }
    return message;
  },

  toJSON(message: AddERCCW20PointerProposal): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    message.pointee !== undefined && (obj.pointee = message.pointee);
    message.pointer !== undefined && (obj.pointer = message.pointer);
    message.version !== undefined && (obj.version = message.version);
    return obj;
  },

  fromPartial(
    object: DeepPartial<AddERCCW20PointerProposal>
  ): AddERCCW20PointerProposal {
    const message = {
      ...baseAddERCCW20PointerProposal,
    } as AddERCCW20PointerProposal;
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
    if (object.pointee !== undefined && object.pointee !== null) {
      message.pointee = object.pointee;
    } else {
      message.pointee = "";
    }
    if (object.pointer !== undefined && object.pointer !== null) {
      message.pointer = object.pointer;
    } else {
      message.pointer = "";
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = object.version;
    } else {
      message.version = 0;
    }
    return message;
  },
};

const baseAddERCCW721PointerProposal: object = {
  title: "",
  description: "",
  pointee: "",
  pointer: "",
  version: 0,
};

export const AddERCCW721PointerProposal = {
  encode(
    message: AddERCCW721PointerProposal,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    if (message.pointee !== "") {
      writer.uint32(26).string(message.pointee);
    }
    if (message.pointer !== "") {
      writer.uint32(34).string(message.pointer);
    }
    if (message.version !== 0) {
      writer.uint32(40).uint32(message.version);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): AddERCCW721PointerProposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseAddERCCW721PointerProposal,
    } as AddERCCW721PointerProposal;
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
          message.pointee = reader.string();
          break;
        case 4:
          message.pointer = reader.string();
          break;
        case 5:
          message.version = reader.uint32();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): AddERCCW721PointerProposal {
    const message = {
      ...baseAddERCCW721PointerProposal,
    } as AddERCCW721PointerProposal;
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
    if (object.pointee !== undefined && object.pointee !== null) {
      message.pointee = String(object.pointee);
    } else {
      message.pointee = "";
    }
    if (object.pointer !== undefined && object.pointer !== null) {
      message.pointer = String(object.pointer);
    } else {
      message.pointer = "";
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = Number(object.version);
    } else {
      message.version = 0;
    }
    return message;
  },

  toJSON(message: AddERCCW721PointerProposal): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    message.pointee !== undefined && (obj.pointee = message.pointee);
    message.pointer !== undefined && (obj.pointer = message.pointer);
    message.version !== undefined && (obj.version = message.version);
    return obj;
  },

  fromPartial(
    object: DeepPartial<AddERCCW721PointerProposal>
  ): AddERCCW721PointerProposal {
    const message = {
      ...baseAddERCCW721PointerProposal,
    } as AddERCCW721PointerProposal;
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
    if (object.pointee !== undefined && object.pointee !== null) {
      message.pointee = object.pointee;
    } else {
      message.pointee = "";
    }
    if (object.pointer !== undefined && object.pointer !== null) {
      message.pointer = object.pointer;
    } else {
      message.pointer = "";
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = object.version;
    } else {
      message.version = 0;
    }
    return message;
  },
};

const baseAddCWERC20PointerProposal: object = {
  title: "",
  description: "",
  pointee: "",
  pointer: "",
  version: 0,
};

export const AddCWERC20PointerProposal = {
  encode(
    message: AddCWERC20PointerProposal,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    if (message.pointee !== "") {
      writer.uint32(26).string(message.pointee);
    }
    if (message.pointer !== "") {
      writer.uint32(34).string(message.pointer);
    }
    if (message.version !== 0) {
      writer.uint32(40).uint32(message.version);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): AddCWERC20PointerProposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseAddCWERC20PointerProposal,
    } as AddCWERC20PointerProposal;
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
          message.pointee = reader.string();
          break;
        case 4:
          message.pointer = reader.string();
          break;
        case 5:
          message.version = reader.uint32();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): AddCWERC20PointerProposal {
    const message = {
      ...baseAddCWERC20PointerProposal,
    } as AddCWERC20PointerProposal;
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
    if (object.pointee !== undefined && object.pointee !== null) {
      message.pointee = String(object.pointee);
    } else {
      message.pointee = "";
    }
    if (object.pointer !== undefined && object.pointer !== null) {
      message.pointer = String(object.pointer);
    } else {
      message.pointer = "";
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = Number(object.version);
    } else {
      message.version = 0;
    }
    return message;
  },

  toJSON(message: AddCWERC20PointerProposal): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    message.pointee !== undefined && (obj.pointee = message.pointee);
    message.pointer !== undefined && (obj.pointer = message.pointer);
    message.version !== undefined && (obj.version = message.version);
    return obj;
  },

  fromPartial(
    object: DeepPartial<AddCWERC20PointerProposal>
  ): AddCWERC20PointerProposal {
    const message = {
      ...baseAddCWERC20PointerProposal,
    } as AddCWERC20PointerProposal;
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
    if (object.pointee !== undefined && object.pointee !== null) {
      message.pointee = object.pointee;
    } else {
      message.pointee = "";
    }
    if (object.pointer !== undefined && object.pointer !== null) {
      message.pointer = object.pointer;
    } else {
      message.pointer = "";
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = object.version;
    } else {
      message.version = 0;
    }
    return message;
  },
};

const baseAddCWERC721PointerProposal: object = {
  title: "",
  description: "",
  pointee: "",
  pointer: "",
  version: 0,
};

export const AddCWERC721PointerProposal = {
  encode(
    message: AddCWERC721PointerProposal,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    if (message.pointee !== "") {
      writer.uint32(26).string(message.pointee);
    }
    if (message.pointer !== "") {
      writer.uint32(34).string(message.pointer);
    }
    if (message.version !== 0) {
      writer.uint32(40).uint32(message.version);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): AddCWERC721PointerProposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseAddCWERC721PointerProposal,
    } as AddCWERC721PointerProposal;
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
          message.pointee = reader.string();
          break;
        case 4:
          message.pointer = reader.string();
          break;
        case 5:
          message.version = reader.uint32();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): AddCWERC721PointerProposal {
    const message = {
      ...baseAddCWERC721PointerProposal,
    } as AddCWERC721PointerProposal;
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
    if (object.pointee !== undefined && object.pointee !== null) {
      message.pointee = String(object.pointee);
    } else {
      message.pointee = "";
    }
    if (object.pointer !== undefined && object.pointer !== null) {
      message.pointer = String(object.pointer);
    } else {
      message.pointer = "";
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = Number(object.version);
    } else {
      message.version = 0;
    }
    return message;
  },

  toJSON(message: AddCWERC721PointerProposal): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    message.pointee !== undefined && (obj.pointee = message.pointee);
    message.pointer !== undefined && (obj.pointer = message.pointer);
    message.version !== undefined && (obj.version = message.version);
    return obj;
  },

  fromPartial(
    object: DeepPartial<AddCWERC721PointerProposal>
  ): AddCWERC721PointerProposal {
    const message = {
      ...baseAddCWERC721PointerProposal,
    } as AddCWERC721PointerProposal;
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
    if (object.pointee !== undefined && object.pointee !== null) {
      message.pointee = object.pointee;
    } else {
      message.pointee = "";
    }
    if (object.pointer !== undefined && object.pointer !== null) {
      message.pointer = object.pointer;
    } else {
      message.pointer = "";
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = object.version;
    } else {
      message.version = 0;
    }
    return message;
  },
};

const baseAddERCNativePointerProposalV2: object = {
  title: "",
  description: "",
  token: "",
  name: "",
  symbol: "",
  decimals: 0,
};

export const AddERCNativePointerProposalV2 = {
  encode(
    message: AddERCNativePointerProposalV2,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    if (message.token !== "") {
      writer.uint32(26).string(message.token);
    }
    if (message.name !== "") {
      writer.uint32(34).string(message.name);
    }
    if (message.symbol !== "") {
      writer.uint32(42).string(message.symbol);
    }
    if (message.decimals !== 0) {
      writer.uint32(48).uint32(message.decimals);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): AddERCNativePointerProposalV2 {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseAddERCNativePointerProposalV2,
    } as AddERCNativePointerProposalV2;
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
          message.token = reader.string();
          break;
        case 4:
          message.name = reader.string();
          break;
        case 5:
          message.symbol = reader.string();
          break;
        case 6:
          message.decimals = reader.uint32();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): AddERCNativePointerProposalV2 {
    const message = {
      ...baseAddERCNativePointerProposalV2,
    } as AddERCNativePointerProposalV2;
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
    if (object.token !== undefined && object.token !== null) {
      message.token = String(object.token);
    } else {
      message.token = "";
    }
    if (object.name !== undefined && object.name !== null) {
      message.name = String(object.name);
    } else {
      message.name = "";
    }
    if (object.symbol !== undefined && object.symbol !== null) {
      message.symbol = String(object.symbol);
    } else {
      message.symbol = "";
    }
    if (object.decimals !== undefined && object.decimals !== null) {
      message.decimals = Number(object.decimals);
    } else {
      message.decimals = 0;
    }
    return message;
  },

  toJSON(message: AddERCNativePointerProposalV2): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    message.token !== undefined && (obj.token = message.token);
    message.name !== undefined && (obj.name = message.name);
    message.symbol !== undefined && (obj.symbol = message.symbol);
    message.decimals !== undefined && (obj.decimals = message.decimals);
    return obj;
  },

  fromPartial(
    object: DeepPartial<AddERCNativePointerProposalV2>
  ): AddERCNativePointerProposalV2 {
    const message = {
      ...baseAddERCNativePointerProposalV2,
    } as AddERCNativePointerProposalV2;
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
    if (object.token !== undefined && object.token !== null) {
      message.token = object.token;
    } else {
      message.token = "";
    }
    if (object.name !== undefined && object.name !== null) {
      message.name = object.name;
    } else {
      message.name = "";
    }
    if (object.symbol !== undefined && object.symbol !== null) {
      message.symbol = object.symbol;
    } else {
      message.symbol = "";
    }
    if (object.decimals !== undefined && object.decimals !== null) {
      message.decimals = object.decimals;
    } else {
      message.decimals = 0;
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
