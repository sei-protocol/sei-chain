/* eslint-disable */
import { AssetMetadata } from "../dex/asset_list";
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.dex";

/**
 * AddAssetMetadataProposal is a gov Content type for adding a new asset
 * to the dex module's asset list.
 */
export interface AddAssetMetadataProposal {
  title: string;
  description: string;
  assetList: AssetMetadata[];
}

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
