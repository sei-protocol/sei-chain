/* eslint-disable */
import { Metadata } from "../cosmos/bank/v1beta1/bank";
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "seiprotocol.seichain.dex";
const baseAssetIBCInfo = {
    sourceChannel: "",
    dstChannel: "",
    sourceDenom: "",
    sourceChainID: "",
};
export const AssetIBCInfo = {
    encode(message, writer = Writer.create()) {
        if (message.sourceChannel !== "") {
            writer.uint32(10).string(message.sourceChannel);
        }
        if (message.dstChannel !== "") {
            writer.uint32(18).string(message.dstChannel);
        }
        if (message.sourceDenom !== "") {
            writer.uint32(26).string(message.sourceDenom);
        }
        if (message.sourceChainID !== "") {
            writer.uint32(34).string(message.sourceChainID);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseAssetIBCInfo };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.sourceChannel = reader.string();
                    break;
                case 2:
                    message.dstChannel = reader.string();
                    break;
                case 3:
                    message.sourceDenom = reader.string();
                    break;
                case 4:
                    message.sourceChainID = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseAssetIBCInfo };
        if (object.sourceChannel !== undefined && object.sourceChannel !== null) {
            message.sourceChannel = String(object.sourceChannel);
        }
        else {
            message.sourceChannel = "";
        }
        if (object.dstChannel !== undefined && object.dstChannel !== null) {
            message.dstChannel = String(object.dstChannel);
        }
        else {
            message.dstChannel = "";
        }
        if (object.sourceDenom !== undefined && object.sourceDenom !== null) {
            message.sourceDenom = String(object.sourceDenom);
        }
        else {
            message.sourceDenom = "";
        }
        if (object.sourceChainID !== undefined && object.sourceChainID !== null) {
            message.sourceChainID = String(object.sourceChainID);
        }
        else {
            message.sourceChainID = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.sourceChannel !== undefined &&
            (obj.sourceChannel = message.sourceChannel);
        message.dstChannel !== undefined && (obj.dstChannel = message.dstChannel);
        message.sourceDenom !== undefined &&
            (obj.sourceDenom = message.sourceDenom);
        message.sourceChainID !== undefined &&
            (obj.sourceChainID = message.sourceChainID);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseAssetIBCInfo };
        if (object.sourceChannel !== undefined && object.sourceChannel !== null) {
            message.sourceChannel = object.sourceChannel;
        }
        else {
            message.sourceChannel = "";
        }
        if (object.dstChannel !== undefined && object.dstChannel !== null) {
            message.dstChannel = object.dstChannel;
        }
        else {
            message.dstChannel = "";
        }
        if (object.sourceDenom !== undefined && object.sourceDenom !== null) {
            message.sourceDenom = object.sourceDenom;
        }
        else {
            message.sourceDenom = "";
        }
        if (object.sourceChainID !== undefined && object.sourceChainID !== null) {
            message.sourceChainID = object.sourceChainID;
        }
        else {
            message.sourceChainID = "";
        }
        return message;
    },
};
const baseAssetMetadata = { typeAsset: "" };
export const AssetMetadata = {
    encode(message, writer = Writer.create()) {
        if (message.ibcInfo !== undefined) {
            AssetIBCInfo.encode(message.ibcInfo, writer.uint32(10).fork()).ldelim();
        }
        if (message.typeAsset !== "") {
            writer.uint32(18).string(message.typeAsset);
        }
        if (message.metadata !== undefined) {
            Metadata.encode(message.metadata, writer.uint32(26).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseAssetMetadata };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.ibcInfo = AssetIBCInfo.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.typeAsset = reader.string();
                    break;
                case 3:
                    message.metadata = Metadata.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseAssetMetadata };
        if (object.ibcInfo !== undefined && object.ibcInfo !== null) {
            message.ibcInfo = AssetIBCInfo.fromJSON(object.ibcInfo);
        }
        else {
            message.ibcInfo = undefined;
        }
        if (object.typeAsset !== undefined && object.typeAsset !== null) {
            message.typeAsset = String(object.typeAsset);
        }
        else {
            message.typeAsset = "";
        }
        if (object.metadata !== undefined && object.metadata !== null) {
            message.metadata = Metadata.fromJSON(object.metadata);
        }
        else {
            message.metadata = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.ibcInfo !== undefined &&
            (obj.ibcInfo = message.ibcInfo
                ? AssetIBCInfo.toJSON(message.ibcInfo)
                : undefined);
        message.typeAsset !== undefined && (obj.typeAsset = message.typeAsset);
        message.metadata !== undefined &&
            (obj.metadata = message.metadata
                ? Metadata.toJSON(message.metadata)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseAssetMetadata };
        if (object.ibcInfo !== undefined && object.ibcInfo !== null) {
            message.ibcInfo = AssetIBCInfo.fromPartial(object.ibcInfo);
        }
        else {
            message.ibcInfo = undefined;
        }
        if (object.typeAsset !== undefined && object.typeAsset !== null) {
            message.typeAsset = object.typeAsset;
        }
        else {
            message.typeAsset = "";
        }
        if (object.metadata !== undefined && object.metadata !== null) {
            message.metadata = Metadata.fromPartial(object.metadata);
        }
        else {
            message.metadata = undefined;
        }
        return message;
    },
};
