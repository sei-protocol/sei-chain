/* eslint-disable */
import { BatchContractPair } from "../dex/pair";
import { TickSize } from "../dex/tick_size";
import { AssetMetadata } from "../dex/asset_list";
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.dex";

/**
 * RegisterPairsProposal is a gov Content type for adding a new whitelisted token
 * pair to the dex module. It must specify a list of contract addresses and their respective
 * token pairs to be registered.
 */
export interface RegisterPairsProposal {
  title: string;
  description: string;
  batchcontractpair: BatchContractPair[];
}

export interface UpdateTickSizeProposal {
  title: string;
  description: string;
  tickSizeList: TickSize[];
}

/**
 * AddAssetMetadataProposal is a gov Content type for adding a new asset
 * to the dex module's asset list.
 */
export interface AddAssetMetadataProposal {
  title: string;
  description: string;
  assetList: AssetMetadata[];
}

const baseRegisterPairsProposal: object = { title: "", description: "" };

export const RegisterPairsProposal = {
  encode(
    message: RegisterPairsProposal,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    for (const v of message.batchcontractpair) {
      BatchContractPair.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): RegisterPairsProposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseRegisterPairsProposal } as RegisterPairsProposal;
    message.batchcontractpair = [];
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
          message.batchcontractpair.push(
            BatchContractPair.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): RegisterPairsProposal {
    const message = { ...baseRegisterPairsProposal } as RegisterPairsProposal;
    message.batchcontractpair = [];
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
      object.batchcontractpair !== undefined &&
      object.batchcontractpair !== null
    ) {
      for (const e of object.batchcontractpair) {
        message.batchcontractpair.push(BatchContractPair.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: RegisterPairsProposal): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    if (message.batchcontractpair) {
      obj.batchcontractpair = message.batchcontractpair.map((e) =>
        e ? BatchContractPair.toJSON(e) : undefined
      );
    } else {
      obj.batchcontractpair = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<RegisterPairsProposal>
  ): RegisterPairsProposal {
    const message = { ...baseRegisterPairsProposal } as RegisterPairsProposal;
    message.batchcontractpair = [];
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
      object.batchcontractpair !== undefined &&
      object.batchcontractpair !== null
    ) {
      for (const e of object.batchcontractpair) {
        message.batchcontractpair.push(BatchContractPair.fromPartial(e));
      }
    }
    return message;
  },
};

const baseUpdateTickSizeProposal: object = { title: "", description: "" };

export const UpdateTickSizeProposal = {
  encode(
    message: UpdateTickSizeProposal,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    for (const v of message.tickSizeList) {
      TickSize.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): UpdateTickSizeProposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseUpdateTickSizeProposal } as UpdateTickSizeProposal;
    message.tickSizeList = [];
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
          message.tickSizeList.push(TickSize.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): UpdateTickSizeProposal {
    const message = { ...baseUpdateTickSizeProposal } as UpdateTickSizeProposal;
    message.tickSizeList = [];
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
    if (object.tickSizeList !== undefined && object.tickSizeList !== null) {
      for (const e of object.tickSizeList) {
        message.tickSizeList.push(TickSize.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: UpdateTickSizeProposal): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    if (message.tickSizeList) {
      obj.tickSizeList = message.tickSizeList.map((e) =>
        e ? TickSize.toJSON(e) : undefined
      );
    } else {
      obj.tickSizeList = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<UpdateTickSizeProposal>
  ): UpdateTickSizeProposal {
    const message = { ...baseUpdateTickSizeProposal } as UpdateTickSizeProposal;
    message.tickSizeList = [];
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
    if (object.tickSizeList !== undefined && object.tickSizeList !== null) {
      for (const e of object.tickSizeList) {
        message.tickSizeList.push(TickSize.fromPartial(e));
      }
    }
    return message;
  },
};

const baseAddAssetMetadataProposal: object = { title: "", description: "" };

export const AddAssetMetadataProposal = {
  encode(
    message: AddAssetMetadataProposal,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    for (const v of message.assetList) {
      AssetMetadata.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): AddAssetMetadataProposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseAddAssetMetadataProposal,
    } as AddAssetMetadataProposal;
    message.assetList = [];
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
          message.assetList.push(AssetMetadata.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): AddAssetMetadataProposal {
    const message = {
      ...baseAddAssetMetadataProposal,
    } as AddAssetMetadataProposal;
    message.assetList = [];
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
    if (object.assetList !== undefined && object.assetList !== null) {
      for (const e of object.assetList) {
        message.assetList.push(AssetMetadata.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: AddAssetMetadataProposal): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    if (message.assetList) {
      obj.assetList = message.assetList.map((e) =>
        e ? AssetMetadata.toJSON(e) : undefined
      );
    } else {
      obj.assetList = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<AddAssetMetadataProposal>
  ): AddAssetMetadataProposal {
    const message = {
      ...baseAddAssetMetadataProposal,
    } as AddAssetMetadataProposal;
    message.assetList = [];
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
    if (object.assetList !== undefined && object.assetList !== null) {
      for (const e of object.assetList) {
        message.assetList.push(AssetMetadata.fromPartial(e));
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
