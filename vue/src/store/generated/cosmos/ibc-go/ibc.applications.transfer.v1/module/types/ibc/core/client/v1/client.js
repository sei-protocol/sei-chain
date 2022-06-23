/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Any } from "../../../../google/protobuf/any";
import { Plan } from "../../../../cosmos/upgrade/v1beta1/upgrade";
export const protobufPackage = "ibc.core.client.v1";
const baseIdentifiedClientState = { clientId: "" };
export const IdentifiedClientState = {
    encode(message, writer = Writer.create()) {
        if (message.clientId !== "") {
            writer.uint32(10).string(message.clientId);
        }
        if (message.clientState !== undefined) {
            Any.encode(message.clientState, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseIdentifiedClientState };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.clientId = reader.string();
                    break;
                case 2:
                    message.clientState = Any.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseIdentifiedClientState };
        if (object.clientId !== undefined && object.clientId !== null) {
            message.clientId = String(object.clientId);
        }
        else {
            message.clientId = "";
        }
        if (object.clientState !== undefined && object.clientState !== null) {
            message.clientState = Any.fromJSON(object.clientState);
        }
        else {
            message.clientState = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.clientId !== undefined && (obj.clientId = message.clientId);
        message.clientState !== undefined &&
            (obj.clientState = message.clientState
                ? Any.toJSON(message.clientState)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseIdentifiedClientState };
        if (object.clientId !== undefined && object.clientId !== null) {
            message.clientId = object.clientId;
        }
        else {
            message.clientId = "";
        }
        if (object.clientState !== undefined && object.clientState !== null) {
            message.clientState = Any.fromPartial(object.clientState);
        }
        else {
            message.clientState = undefined;
        }
        return message;
    },
};
const baseConsensusStateWithHeight = {};
export const ConsensusStateWithHeight = {
    encode(message, writer = Writer.create()) {
        if (message.height !== undefined) {
            Height.encode(message.height, writer.uint32(10).fork()).ldelim();
        }
        if (message.consensusState !== undefined) {
            Any.encode(message.consensusState, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseConsensusStateWithHeight,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.height = Height.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.consensusState = Any.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseConsensusStateWithHeight,
        };
        if (object.height !== undefined && object.height !== null) {
            message.height = Height.fromJSON(object.height);
        }
        else {
            message.height = undefined;
        }
        if (object.consensusState !== undefined && object.consensusState !== null) {
            message.consensusState = Any.fromJSON(object.consensusState);
        }
        else {
            message.consensusState = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.height !== undefined &&
            (obj.height = message.height ? Height.toJSON(message.height) : undefined);
        message.consensusState !== undefined &&
            (obj.consensusState = message.consensusState
                ? Any.toJSON(message.consensusState)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseConsensusStateWithHeight,
        };
        if (object.height !== undefined && object.height !== null) {
            message.height = Height.fromPartial(object.height);
        }
        else {
            message.height = undefined;
        }
        if (object.consensusState !== undefined && object.consensusState !== null) {
            message.consensusState = Any.fromPartial(object.consensusState);
        }
        else {
            message.consensusState = undefined;
        }
        return message;
    },
};
const baseClientConsensusStates = { clientId: "" };
export const ClientConsensusStates = {
    encode(message, writer = Writer.create()) {
        if (message.clientId !== "") {
            writer.uint32(10).string(message.clientId);
        }
        for (const v of message.consensusStates) {
            ConsensusStateWithHeight.encode(v, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseClientConsensusStates };
        message.consensusStates = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.clientId = reader.string();
                    break;
                case 2:
                    message.consensusStates.push(ConsensusStateWithHeight.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseClientConsensusStates };
        message.consensusStates = [];
        if (object.clientId !== undefined && object.clientId !== null) {
            message.clientId = String(object.clientId);
        }
        else {
            message.clientId = "";
        }
        if (object.consensusStates !== undefined &&
            object.consensusStates !== null) {
            for (const e of object.consensusStates) {
                message.consensusStates.push(ConsensusStateWithHeight.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.clientId !== undefined && (obj.clientId = message.clientId);
        if (message.consensusStates) {
            obj.consensusStates = message.consensusStates.map((e) => e ? ConsensusStateWithHeight.toJSON(e) : undefined);
        }
        else {
            obj.consensusStates = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseClientConsensusStates };
        message.consensusStates = [];
        if (object.clientId !== undefined && object.clientId !== null) {
            message.clientId = object.clientId;
        }
        else {
            message.clientId = "";
        }
        if (object.consensusStates !== undefined &&
            object.consensusStates !== null) {
            for (const e of object.consensusStates) {
                message.consensusStates.push(ConsensusStateWithHeight.fromPartial(e));
            }
        }
        return message;
    },
};
const baseClientUpdateProposal = {
    title: "",
    description: "",
    subjectClientId: "",
    substituteClientId: "",
};
export const ClientUpdateProposal = {
    encode(message, writer = Writer.create()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.subjectClientId !== "") {
            writer.uint32(26).string(message.subjectClientId);
        }
        if (message.substituteClientId !== "") {
            writer.uint32(34).string(message.substituteClientId);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseClientUpdateProposal };
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
                    message.subjectClientId = reader.string();
                    break;
                case 4:
                    message.substituteClientId = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseClientUpdateProposal };
        if (object.title !== undefined && object.title !== null) {
            message.title = String(object.title);
        }
        else {
            message.title = "";
        }
        if (object.description !== undefined && object.description !== null) {
            message.description = String(object.description);
        }
        else {
            message.description = "";
        }
        if (object.subjectClientId !== undefined &&
            object.subjectClientId !== null) {
            message.subjectClientId = String(object.subjectClientId);
        }
        else {
            message.subjectClientId = "";
        }
        if (object.substituteClientId !== undefined &&
            object.substituteClientId !== null) {
            message.substituteClientId = String(object.substituteClientId);
        }
        else {
            message.substituteClientId = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.title !== undefined && (obj.title = message.title);
        message.description !== undefined &&
            (obj.description = message.description);
        message.subjectClientId !== undefined &&
            (obj.subjectClientId = message.subjectClientId);
        message.substituteClientId !== undefined &&
            (obj.substituteClientId = message.substituteClientId);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseClientUpdateProposal };
        if (object.title !== undefined && object.title !== null) {
            message.title = object.title;
        }
        else {
            message.title = "";
        }
        if (object.description !== undefined && object.description !== null) {
            message.description = object.description;
        }
        else {
            message.description = "";
        }
        if (object.subjectClientId !== undefined &&
            object.subjectClientId !== null) {
            message.subjectClientId = object.subjectClientId;
        }
        else {
            message.subjectClientId = "";
        }
        if (object.substituteClientId !== undefined &&
            object.substituteClientId !== null) {
            message.substituteClientId = object.substituteClientId;
        }
        else {
            message.substituteClientId = "";
        }
        return message;
    },
};
const baseUpgradeProposal = { title: "", description: "" };
export const UpgradeProposal = {
    encode(message, writer = Writer.create()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.plan !== undefined) {
            Plan.encode(message.plan, writer.uint32(26).fork()).ldelim();
        }
        if (message.upgradedClientState !== undefined) {
            Any.encode(message.upgradedClientState, writer.uint32(34).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseUpgradeProposal };
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
                    message.plan = Plan.decode(reader, reader.uint32());
                    break;
                case 4:
                    message.upgradedClientState = Any.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseUpgradeProposal };
        if (object.title !== undefined && object.title !== null) {
            message.title = String(object.title);
        }
        else {
            message.title = "";
        }
        if (object.description !== undefined && object.description !== null) {
            message.description = String(object.description);
        }
        else {
            message.description = "";
        }
        if (object.plan !== undefined && object.plan !== null) {
            message.plan = Plan.fromJSON(object.plan);
        }
        else {
            message.plan = undefined;
        }
        if (object.upgradedClientState !== undefined &&
            object.upgradedClientState !== null) {
            message.upgradedClientState = Any.fromJSON(object.upgradedClientState);
        }
        else {
            message.upgradedClientState = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.title !== undefined && (obj.title = message.title);
        message.description !== undefined &&
            (obj.description = message.description);
        message.plan !== undefined &&
            (obj.plan = message.plan ? Plan.toJSON(message.plan) : undefined);
        message.upgradedClientState !== undefined &&
            (obj.upgradedClientState = message.upgradedClientState
                ? Any.toJSON(message.upgradedClientState)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseUpgradeProposal };
        if (object.title !== undefined && object.title !== null) {
            message.title = object.title;
        }
        else {
            message.title = "";
        }
        if (object.description !== undefined && object.description !== null) {
            message.description = object.description;
        }
        else {
            message.description = "";
        }
        if (object.plan !== undefined && object.plan !== null) {
            message.plan = Plan.fromPartial(object.plan);
        }
        else {
            message.plan = undefined;
        }
        if (object.upgradedClientState !== undefined &&
            object.upgradedClientState !== null) {
            message.upgradedClientState = Any.fromPartial(object.upgradedClientState);
        }
        else {
            message.upgradedClientState = undefined;
        }
        return message;
    },
};
const baseHeight = { revisionNumber: 0, revisionHeight: 0 };
export const Height = {
    encode(message, writer = Writer.create()) {
        if (message.revisionNumber !== 0) {
            writer.uint32(8).uint64(message.revisionNumber);
        }
        if (message.revisionHeight !== 0) {
            writer.uint32(16).uint64(message.revisionHeight);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseHeight };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.revisionNumber = longToNumber(reader.uint64());
                    break;
                case 2:
                    message.revisionHeight = longToNumber(reader.uint64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseHeight };
        if (object.revisionNumber !== undefined && object.revisionNumber !== null) {
            message.revisionNumber = Number(object.revisionNumber);
        }
        else {
            message.revisionNumber = 0;
        }
        if (object.revisionHeight !== undefined && object.revisionHeight !== null) {
            message.revisionHeight = Number(object.revisionHeight);
        }
        else {
            message.revisionHeight = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.revisionNumber !== undefined &&
            (obj.revisionNumber = message.revisionNumber);
        message.revisionHeight !== undefined &&
            (obj.revisionHeight = message.revisionHeight);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseHeight };
        if (object.revisionNumber !== undefined && object.revisionNumber !== null) {
            message.revisionNumber = object.revisionNumber;
        }
        else {
            message.revisionNumber = 0;
        }
        if (object.revisionHeight !== undefined && object.revisionHeight !== null) {
            message.revisionHeight = object.revisionHeight;
        }
        else {
            message.revisionHeight = 0;
        }
        return message;
    },
};
const baseParams = { allowedClients: "" };
export const Params = {
    encode(message, writer = Writer.create()) {
        for (const v of message.allowedClients) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseParams };
        message.allowedClients = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.allowedClients.push(reader.string());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseParams };
        message.allowedClients = [];
        if (object.allowedClients !== undefined && object.allowedClients !== null) {
            for (const e of object.allowedClients) {
                message.allowedClients.push(String(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.allowedClients) {
            obj.allowedClients = message.allowedClients.map((e) => e);
        }
        else {
            obj.allowedClients = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseParams };
        message.allowedClients = [];
        if (object.allowedClients !== undefined && object.allowedClients !== null) {
            for (const e of object.allowedClients) {
                message.allowedClients.push(e);
            }
        }
        return message;
    },
};
var globalThis = (() => {
    if (typeof globalThis !== "undefined")
        return globalThis;
    if (typeof self !== "undefined")
        return self;
    if (typeof window !== "undefined")
        return window;
    if (typeof global !== "undefined")
        return global;
    throw "Unable to locate global object";
})();
function longToNumber(long) {
    if (long.gt(Number.MAX_SAFE_INTEGER)) {
        throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
    }
    return long.toNumber();
}
if (util.Long !== Long) {
    util.Long = Long;
    configure();
}
