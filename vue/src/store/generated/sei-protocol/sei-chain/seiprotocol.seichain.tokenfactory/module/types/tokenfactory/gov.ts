/* eslint-disable */
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.tokenfactory";

export interface AddCreatorsToDenomFeeWhitelistProposal {
  title: string;
  description: string;
  creatorList: string[];
}

const baseAddCreatorsToDenomFeeWhitelistProposal: object = {
  title: "",
  description: "",
  creatorList: "",
};

export const AddCreatorsToDenomFeeWhitelistProposal = {
  encode(
    message: AddCreatorsToDenomFeeWhitelistProposal,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    for (const v of message.creatorList) {
      writer.uint32(26).string(v!);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): AddCreatorsToDenomFeeWhitelistProposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseAddCreatorsToDenomFeeWhitelistProposal,
    } as AddCreatorsToDenomFeeWhitelistProposal;
    message.creatorList = [];
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
          message.creatorList.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): AddCreatorsToDenomFeeWhitelistProposal {
    const message = {
      ...baseAddCreatorsToDenomFeeWhitelistProposal,
    } as AddCreatorsToDenomFeeWhitelistProposal;
    message.creatorList = [];
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
    if (object.creatorList !== undefined && object.creatorList !== null) {
      for (const e of object.creatorList) {
        message.creatorList.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: AddCreatorsToDenomFeeWhitelistProposal): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    if (message.creatorList) {
      obj.creatorList = message.creatorList.map((e) => e);
    } else {
      obj.creatorList = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<AddCreatorsToDenomFeeWhitelistProposal>
  ): AddCreatorsToDenomFeeWhitelistProposal {
    const message = {
      ...baseAddCreatorsToDenomFeeWhitelistProposal,
    } as AddCreatorsToDenomFeeWhitelistProposal;
    message.creatorList = [];
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
    if (object.creatorList !== undefined && object.creatorList !== null) {
      for (const e of object.creatorList) {
        message.creatorList.push(e);
      }
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
