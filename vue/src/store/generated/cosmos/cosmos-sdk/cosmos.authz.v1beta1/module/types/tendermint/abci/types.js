/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import { Timestamp } from "../../google/protobuf/timestamp";
import * as Long from "long";
import { Header } from "../../tendermint/types/types";
import { ProofOps } from "../../tendermint/crypto/proof";
import { EvidenceParams, ValidatorParams, VersionParams, } from "../../tendermint/types/params";
import { PublicKey } from "../../tendermint/crypto/keys";
export const protobufPackage = "tendermint.abci";
export var CheckTxType;
(function (CheckTxType) {
    CheckTxType[CheckTxType["NEW"] = 0] = "NEW";
    CheckTxType[CheckTxType["RECHECK"] = 1] = "RECHECK";
    CheckTxType[CheckTxType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(CheckTxType || (CheckTxType = {}));
export function checkTxTypeFromJSON(object) {
    switch (object) {
        case 0:
        case "NEW":
            return CheckTxType.NEW;
        case 1:
        case "RECHECK":
            return CheckTxType.RECHECK;
        case -1:
        case "UNRECOGNIZED":
        default:
            return CheckTxType.UNRECOGNIZED;
    }
}
export function checkTxTypeToJSON(object) {
    switch (object) {
        case CheckTxType.NEW:
            return "NEW";
        case CheckTxType.RECHECK:
            return "RECHECK";
        default:
            return "UNKNOWN";
    }
}
export var EvidenceType;
(function (EvidenceType) {
    EvidenceType[EvidenceType["UNKNOWN"] = 0] = "UNKNOWN";
    EvidenceType[EvidenceType["DUPLICATE_VOTE"] = 1] = "DUPLICATE_VOTE";
    EvidenceType[EvidenceType["LIGHT_CLIENT_ATTACK"] = 2] = "LIGHT_CLIENT_ATTACK";
    EvidenceType[EvidenceType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(EvidenceType || (EvidenceType = {}));
export function evidenceTypeFromJSON(object) {
    switch (object) {
        case 0:
        case "UNKNOWN":
            return EvidenceType.UNKNOWN;
        case 1:
        case "DUPLICATE_VOTE":
            return EvidenceType.DUPLICATE_VOTE;
        case 2:
        case "LIGHT_CLIENT_ATTACK":
            return EvidenceType.LIGHT_CLIENT_ATTACK;
        case -1:
        case "UNRECOGNIZED":
        default:
            return EvidenceType.UNRECOGNIZED;
    }
}
export function evidenceTypeToJSON(object) {
    switch (object) {
        case EvidenceType.UNKNOWN:
            return "UNKNOWN";
        case EvidenceType.DUPLICATE_VOTE:
            return "DUPLICATE_VOTE";
        case EvidenceType.LIGHT_CLIENT_ATTACK:
            return "LIGHT_CLIENT_ATTACK";
        default:
            return "UNKNOWN";
    }
}
export var ResponseOfferSnapshot_Result;
(function (ResponseOfferSnapshot_Result) {
    /** UNKNOWN - Unknown result, abort all snapshot restoration */
    ResponseOfferSnapshot_Result[ResponseOfferSnapshot_Result["UNKNOWN"] = 0] = "UNKNOWN";
    /** ACCEPT - Snapshot accepted, apply chunks */
    ResponseOfferSnapshot_Result[ResponseOfferSnapshot_Result["ACCEPT"] = 1] = "ACCEPT";
    /** ABORT - Abort all snapshot restoration */
    ResponseOfferSnapshot_Result[ResponseOfferSnapshot_Result["ABORT"] = 2] = "ABORT";
    /** REJECT - Reject this specific snapshot, try others */
    ResponseOfferSnapshot_Result[ResponseOfferSnapshot_Result["REJECT"] = 3] = "REJECT";
    /** REJECT_FORMAT - Reject all snapshots of this format, try others */
    ResponseOfferSnapshot_Result[ResponseOfferSnapshot_Result["REJECT_FORMAT"] = 4] = "REJECT_FORMAT";
    /** REJECT_SENDER - Reject all snapshots from the sender(s), try others */
    ResponseOfferSnapshot_Result[ResponseOfferSnapshot_Result["REJECT_SENDER"] = 5] = "REJECT_SENDER";
    ResponseOfferSnapshot_Result[ResponseOfferSnapshot_Result["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(ResponseOfferSnapshot_Result || (ResponseOfferSnapshot_Result = {}));
export function responseOfferSnapshot_ResultFromJSON(object) {
    switch (object) {
        case 0:
        case "UNKNOWN":
            return ResponseOfferSnapshot_Result.UNKNOWN;
        case 1:
        case "ACCEPT":
            return ResponseOfferSnapshot_Result.ACCEPT;
        case 2:
        case "ABORT":
            return ResponseOfferSnapshot_Result.ABORT;
        case 3:
        case "REJECT":
            return ResponseOfferSnapshot_Result.REJECT;
        case 4:
        case "REJECT_FORMAT":
            return ResponseOfferSnapshot_Result.REJECT_FORMAT;
        case 5:
        case "REJECT_SENDER":
            return ResponseOfferSnapshot_Result.REJECT_SENDER;
        case -1:
        case "UNRECOGNIZED":
        default:
            return ResponseOfferSnapshot_Result.UNRECOGNIZED;
    }
}
export function responseOfferSnapshot_ResultToJSON(object) {
    switch (object) {
        case ResponseOfferSnapshot_Result.UNKNOWN:
            return "UNKNOWN";
        case ResponseOfferSnapshot_Result.ACCEPT:
            return "ACCEPT";
        case ResponseOfferSnapshot_Result.ABORT:
            return "ABORT";
        case ResponseOfferSnapshot_Result.REJECT:
            return "REJECT";
        case ResponseOfferSnapshot_Result.REJECT_FORMAT:
            return "REJECT_FORMAT";
        case ResponseOfferSnapshot_Result.REJECT_SENDER:
            return "REJECT_SENDER";
        default:
            return "UNKNOWN";
    }
}
export var ResponseApplySnapshotChunk_Result;
(function (ResponseApplySnapshotChunk_Result) {
    /** UNKNOWN - Unknown result, abort all snapshot restoration */
    ResponseApplySnapshotChunk_Result[ResponseApplySnapshotChunk_Result["UNKNOWN"] = 0] = "UNKNOWN";
    /** ACCEPT - Chunk successfully accepted */
    ResponseApplySnapshotChunk_Result[ResponseApplySnapshotChunk_Result["ACCEPT"] = 1] = "ACCEPT";
    /** ABORT - Abort all snapshot restoration */
    ResponseApplySnapshotChunk_Result[ResponseApplySnapshotChunk_Result["ABORT"] = 2] = "ABORT";
    /** RETRY - Retry chunk (combine with refetch and reject) */
    ResponseApplySnapshotChunk_Result[ResponseApplySnapshotChunk_Result["RETRY"] = 3] = "RETRY";
    /** RETRY_SNAPSHOT - Retry snapshot (combine with refetch and reject) */
    ResponseApplySnapshotChunk_Result[ResponseApplySnapshotChunk_Result["RETRY_SNAPSHOT"] = 4] = "RETRY_SNAPSHOT";
    /** REJECT_SNAPSHOT - Reject this snapshot, try others */
    ResponseApplySnapshotChunk_Result[ResponseApplySnapshotChunk_Result["REJECT_SNAPSHOT"] = 5] = "REJECT_SNAPSHOT";
    ResponseApplySnapshotChunk_Result[ResponseApplySnapshotChunk_Result["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(ResponseApplySnapshotChunk_Result || (ResponseApplySnapshotChunk_Result = {}));
export function responseApplySnapshotChunk_ResultFromJSON(object) {
    switch (object) {
        case 0:
        case "UNKNOWN":
            return ResponseApplySnapshotChunk_Result.UNKNOWN;
        case 1:
        case "ACCEPT":
            return ResponseApplySnapshotChunk_Result.ACCEPT;
        case 2:
        case "ABORT":
            return ResponseApplySnapshotChunk_Result.ABORT;
        case 3:
        case "RETRY":
            return ResponseApplySnapshotChunk_Result.RETRY;
        case 4:
        case "RETRY_SNAPSHOT":
            return ResponseApplySnapshotChunk_Result.RETRY_SNAPSHOT;
        case 5:
        case "REJECT_SNAPSHOT":
            return ResponseApplySnapshotChunk_Result.REJECT_SNAPSHOT;
        case -1:
        case "UNRECOGNIZED":
        default:
            return ResponseApplySnapshotChunk_Result.UNRECOGNIZED;
    }
}
export function responseApplySnapshotChunk_ResultToJSON(object) {
    switch (object) {
        case ResponseApplySnapshotChunk_Result.UNKNOWN:
            return "UNKNOWN";
        case ResponseApplySnapshotChunk_Result.ACCEPT:
            return "ACCEPT";
        case ResponseApplySnapshotChunk_Result.ABORT:
            return "ABORT";
        case ResponseApplySnapshotChunk_Result.RETRY:
            return "RETRY";
        case ResponseApplySnapshotChunk_Result.RETRY_SNAPSHOT:
            return "RETRY_SNAPSHOT";
        case ResponseApplySnapshotChunk_Result.REJECT_SNAPSHOT:
            return "REJECT_SNAPSHOT";
        default:
            return "UNKNOWN";
    }
}
const baseRequest = {};
export const Request = {
    encode(message, writer = Writer.create()) {
        if (message.echo !== undefined) {
            RequestEcho.encode(message.echo, writer.uint32(10).fork()).ldelim();
        }
        if (message.flush !== undefined) {
            RequestFlush.encode(message.flush, writer.uint32(18).fork()).ldelim();
        }
        if (message.info !== undefined) {
            RequestInfo.encode(message.info, writer.uint32(26).fork()).ldelim();
        }
        if (message.setOption !== undefined) {
            RequestSetOption.encode(message.setOption, writer.uint32(34).fork()).ldelim();
        }
        if (message.initChain !== undefined) {
            RequestInitChain.encode(message.initChain, writer.uint32(42).fork()).ldelim();
        }
        if (message.query !== undefined) {
            RequestQuery.encode(message.query, writer.uint32(50).fork()).ldelim();
        }
        if (message.beginBlock !== undefined) {
            RequestBeginBlock.encode(message.beginBlock, writer.uint32(58).fork()).ldelim();
        }
        if (message.checkTx !== undefined) {
            RequestCheckTx.encode(message.checkTx, writer.uint32(66).fork()).ldelim();
        }
        if (message.deliverTx !== undefined) {
            RequestDeliverTx.encode(message.deliverTx, writer.uint32(74).fork()).ldelim();
        }
        if (message.endBlock !== undefined) {
            RequestEndBlock.encode(message.endBlock, writer.uint32(82).fork()).ldelim();
        }
        if (message.commit !== undefined) {
            RequestCommit.encode(message.commit, writer.uint32(90).fork()).ldelim();
        }
        if (message.listSnapshots !== undefined) {
            RequestListSnapshots.encode(message.listSnapshots, writer.uint32(98).fork()).ldelim();
        }
        if (message.offerSnapshot !== undefined) {
            RequestOfferSnapshot.encode(message.offerSnapshot, writer.uint32(106).fork()).ldelim();
        }
        if (message.loadSnapshotChunk !== undefined) {
            RequestLoadSnapshotChunk.encode(message.loadSnapshotChunk, writer.uint32(114).fork()).ldelim();
        }
        if (message.applySnapshotChunk !== undefined) {
            RequestApplySnapshotChunk.encode(message.applySnapshotChunk, writer.uint32(122).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseRequest };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.echo = RequestEcho.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.flush = RequestFlush.decode(reader, reader.uint32());
                    break;
                case 3:
                    message.info = RequestInfo.decode(reader, reader.uint32());
                    break;
                case 4:
                    message.setOption = RequestSetOption.decode(reader, reader.uint32());
                    break;
                case 5:
                    message.initChain = RequestInitChain.decode(reader, reader.uint32());
                    break;
                case 6:
                    message.query = RequestQuery.decode(reader, reader.uint32());
                    break;
                case 7:
                    message.beginBlock = RequestBeginBlock.decode(reader, reader.uint32());
                    break;
                case 8:
                    message.checkTx = RequestCheckTx.decode(reader, reader.uint32());
                    break;
                case 9:
                    message.deliverTx = RequestDeliverTx.decode(reader, reader.uint32());
                    break;
                case 10:
                    message.endBlock = RequestEndBlock.decode(reader, reader.uint32());
                    break;
                case 11:
                    message.commit = RequestCommit.decode(reader, reader.uint32());
                    break;
                case 12:
                    message.listSnapshots = RequestListSnapshots.decode(reader, reader.uint32());
                    break;
                case 13:
                    message.offerSnapshot = RequestOfferSnapshot.decode(reader, reader.uint32());
                    break;
                case 14:
                    message.loadSnapshotChunk = RequestLoadSnapshotChunk.decode(reader, reader.uint32());
                    break;
                case 15:
                    message.applySnapshotChunk = RequestApplySnapshotChunk.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseRequest };
        if (object.echo !== undefined && object.echo !== null) {
            message.echo = RequestEcho.fromJSON(object.echo);
        }
        else {
            message.echo = undefined;
        }
        if (object.flush !== undefined && object.flush !== null) {
            message.flush = RequestFlush.fromJSON(object.flush);
        }
        else {
            message.flush = undefined;
        }
        if (object.info !== undefined && object.info !== null) {
            message.info = RequestInfo.fromJSON(object.info);
        }
        else {
            message.info = undefined;
        }
        if (object.setOption !== undefined && object.setOption !== null) {
            message.setOption = RequestSetOption.fromJSON(object.setOption);
        }
        else {
            message.setOption = undefined;
        }
        if (object.initChain !== undefined && object.initChain !== null) {
            message.initChain = RequestInitChain.fromJSON(object.initChain);
        }
        else {
            message.initChain = undefined;
        }
        if (object.query !== undefined && object.query !== null) {
            message.query = RequestQuery.fromJSON(object.query);
        }
        else {
            message.query = undefined;
        }
        if (object.beginBlock !== undefined && object.beginBlock !== null) {
            message.beginBlock = RequestBeginBlock.fromJSON(object.beginBlock);
        }
        else {
            message.beginBlock = undefined;
        }
        if (object.checkTx !== undefined && object.checkTx !== null) {
            message.checkTx = RequestCheckTx.fromJSON(object.checkTx);
        }
        else {
            message.checkTx = undefined;
        }
        if (object.deliverTx !== undefined && object.deliverTx !== null) {
            message.deliverTx = RequestDeliverTx.fromJSON(object.deliverTx);
        }
        else {
            message.deliverTx = undefined;
        }
        if (object.endBlock !== undefined && object.endBlock !== null) {
            message.endBlock = RequestEndBlock.fromJSON(object.endBlock);
        }
        else {
            message.endBlock = undefined;
        }
        if (object.commit !== undefined && object.commit !== null) {
            message.commit = RequestCommit.fromJSON(object.commit);
        }
        else {
            message.commit = undefined;
        }
        if (object.listSnapshots !== undefined && object.listSnapshots !== null) {
            message.listSnapshots = RequestListSnapshots.fromJSON(object.listSnapshots);
        }
        else {
            message.listSnapshots = undefined;
        }
        if (object.offerSnapshot !== undefined && object.offerSnapshot !== null) {
            message.offerSnapshot = RequestOfferSnapshot.fromJSON(object.offerSnapshot);
        }
        else {
            message.offerSnapshot = undefined;
        }
        if (object.loadSnapshotChunk !== undefined &&
            object.loadSnapshotChunk !== null) {
            message.loadSnapshotChunk = RequestLoadSnapshotChunk.fromJSON(object.loadSnapshotChunk);
        }
        else {
            message.loadSnapshotChunk = undefined;
        }
        if (object.applySnapshotChunk !== undefined &&
            object.applySnapshotChunk !== null) {
            message.applySnapshotChunk = RequestApplySnapshotChunk.fromJSON(object.applySnapshotChunk);
        }
        else {
            message.applySnapshotChunk = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.echo !== undefined &&
            (obj.echo = message.echo ? RequestEcho.toJSON(message.echo) : undefined);
        message.flush !== undefined &&
            (obj.flush = message.flush
                ? RequestFlush.toJSON(message.flush)
                : undefined);
        message.info !== undefined &&
            (obj.info = message.info ? RequestInfo.toJSON(message.info) : undefined);
        message.setOption !== undefined &&
            (obj.setOption = message.setOption
                ? RequestSetOption.toJSON(message.setOption)
                : undefined);
        message.initChain !== undefined &&
            (obj.initChain = message.initChain
                ? RequestInitChain.toJSON(message.initChain)
                : undefined);
        message.query !== undefined &&
            (obj.query = message.query
                ? RequestQuery.toJSON(message.query)
                : undefined);
        message.beginBlock !== undefined &&
            (obj.beginBlock = message.beginBlock
                ? RequestBeginBlock.toJSON(message.beginBlock)
                : undefined);
        message.checkTx !== undefined &&
            (obj.checkTx = message.checkTx
                ? RequestCheckTx.toJSON(message.checkTx)
                : undefined);
        message.deliverTx !== undefined &&
            (obj.deliverTx = message.deliverTx
                ? RequestDeliverTx.toJSON(message.deliverTx)
                : undefined);
        message.endBlock !== undefined &&
            (obj.endBlock = message.endBlock
                ? RequestEndBlock.toJSON(message.endBlock)
                : undefined);
        message.commit !== undefined &&
            (obj.commit = message.commit
                ? RequestCommit.toJSON(message.commit)
                : undefined);
        message.listSnapshots !== undefined &&
            (obj.listSnapshots = message.listSnapshots
                ? RequestListSnapshots.toJSON(message.listSnapshots)
                : undefined);
        message.offerSnapshot !== undefined &&
            (obj.offerSnapshot = message.offerSnapshot
                ? RequestOfferSnapshot.toJSON(message.offerSnapshot)
                : undefined);
        message.loadSnapshotChunk !== undefined &&
            (obj.loadSnapshotChunk = message.loadSnapshotChunk
                ? RequestLoadSnapshotChunk.toJSON(message.loadSnapshotChunk)
                : undefined);
        message.applySnapshotChunk !== undefined &&
            (obj.applySnapshotChunk = message.applySnapshotChunk
                ? RequestApplySnapshotChunk.toJSON(message.applySnapshotChunk)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseRequest };
        if (object.echo !== undefined && object.echo !== null) {
            message.echo = RequestEcho.fromPartial(object.echo);
        }
        else {
            message.echo = undefined;
        }
        if (object.flush !== undefined && object.flush !== null) {
            message.flush = RequestFlush.fromPartial(object.flush);
        }
        else {
            message.flush = undefined;
        }
        if (object.info !== undefined && object.info !== null) {
            message.info = RequestInfo.fromPartial(object.info);
        }
        else {
            message.info = undefined;
        }
        if (object.setOption !== undefined && object.setOption !== null) {
            message.setOption = RequestSetOption.fromPartial(object.setOption);
        }
        else {
            message.setOption = undefined;
        }
        if (object.initChain !== undefined && object.initChain !== null) {
            message.initChain = RequestInitChain.fromPartial(object.initChain);
        }
        else {
            message.initChain = undefined;
        }
        if (object.query !== undefined && object.query !== null) {
            message.query = RequestQuery.fromPartial(object.query);
        }
        else {
            message.query = undefined;
        }
        if (object.beginBlock !== undefined && object.beginBlock !== null) {
            message.beginBlock = RequestBeginBlock.fromPartial(object.beginBlock);
        }
        else {
            message.beginBlock = undefined;
        }
        if (object.checkTx !== undefined && object.checkTx !== null) {
            message.checkTx = RequestCheckTx.fromPartial(object.checkTx);
        }
        else {
            message.checkTx = undefined;
        }
        if (object.deliverTx !== undefined && object.deliverTx !== null) {
            message.deliverTx = RequestDeliverTx.fromPartial(object.deliverTx);
        }
        else {
            message.deliverTx = undefined;
        }
        if (object.endBlock !== undefined && object.endBlock !== null) {
            message.endBlock = RequestEndBlock.fromPartial(object.endBlock);
        }
        else {
            message.endBlock = undefined;
        }
        if (object.commit !== undefined && object.commit !== null) {
            message.commit = RequestCommit.fromPartial(object.commit);
        }
        else {
            message.commit = undefined;
        }
        if (object.listSnapshots !== undefined && object.listSnapshots !== null) {
            message.listSnapshots = RequestListSnapshots.fromPartial(object.listSnapshots);
        }
        else {
            message.listSnapshots = undefined;
        }
        if (object.offerSnapshot !== undefined && object.offerSnapshot !== null) {
            message.offerSnapshot = RequestOfferSnapshot.fromPartial(object.offerSnapshot);
        }
        else {
            message.offerSnapshot = undefined;
        }
        if (object.loadSnapshotChunk !== undefined &&
            object.loadSnapshotChunk !== null) {
            message.loadSnapshotChunk = RequestLoadSnapshotChunk.fromPartial(object.loadSnapshotChunk);
        }
        else {
            message.loadSnapshotChunk = undefined;
        }
        if (object.applySnapshotChunk !== undefined &&
            object.applySnapshotChunk !== null) {
            message.applySnapshotChunk = RequestApplySnapshotChunk.fromPartial(object.applySnapshotChunk);
        }
        else {
            message.applySnapshotChunk = undefined;
        }
        return message;
    },
};
const baseRequestEcho = { message: "" };
export const RequestEcho = {
    encode(message, writer = Writer.create()) {
        if (message.message !== "") {
            writer.uint32(10).string(message.message);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseRequestEcho };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.message = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseRequestEcho };
        if (object.message !== undefined && object.message !== null) {
            message.message = String(object.message);
        }
        else {
            message.message = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.message !== undefined && (obj.message = message.message);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseRequestEcho };
        if (object.message !== undefined && object.message !== null) {
            message.message = object.message;
        }
        else {
            message.message = "";
        }
        return message;
    },
};
const baseRequestFlush = {};
export const RequestFlush = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseRequestFlush };
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
    fromJSON(_) {
        const message = { ...baseRequestFlush };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = { ...baseRequestFlush };
        return message;
    },
};
const baseRequestInfo = { version: "", blockVersion: 0, p2pVersion: 0 };
export const RequestInfo = {
    encode(message, writer = Writer.create()) {
        if (message.version !== "") {
            writer.uint32(10).string(message.version);
        }
        if (message.blockVersion !== 0) {
            writer.uint32(16).uint64(message.blockVersion);
        }
        if (message.p2pVersion !== 0) {
            writer.uint32(24).uint64(message.p2pVersion);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseRequestInfo };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.version = reader.string();
                    break;
                case 2:
                    message.blockVersion = longToNumber(reader.uint64());
                    break;
                case 3:
                    message.p2pVersion = longToNumber(reader.uint64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseRequestInfo };
        if (object.version !== undefined && object.version !== null) {
            message.version = String(object.version);
        }
        else {
            message.version = "";
        }
        if (object.blockVersion !== undefined && object.blockVersion !== null) {
            message.blockVersion = Number(object.blockVersion);
        }
        else {
            message.blockVersion = 0;
        }
        if (object.p2pVersion !== undefined && object.p2pVersion !== null) {
            message.p2pVersion = Number(object.p2pVersion);
        }
        else {
            message.p2pVersion = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.version !== undefined && (obj.version = message.version);
        message.blockVersion !== undefined &&
            (obj.blockVersion = message.blockVersion);
        message.p2pVersion !== undefined && (obj.p2pVersion = message.p2pVersion);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseRequestInfo };
        if (object.version !== undefined && object.version !== null) {
            message.version = object.version;
        }
        else {
            message.version = "";
        }
        if (object.blockVersion !== undefined && object.blockVersion !== null) {
            message.blockVersion = object.blockVersion;
        }
        else {
            message.blockVersion = 0;
        }
        if (object.p2pVersion !== undefined && object.p2pVersion !== null) {
            message.p2pVersion = object.p2pVersion;
        }
        else {
            message.p2pVersion = 0;
        }
        return message;
    },
};
const baseRequestSetOption = { key: "", value: "" };
export const RequestSetOption = {
    encode(message, writer = Writer.create()) {
        if (message.key !== "") {
            writer.uint32(10).string(message.key);
        }
        if (message.value !== "") {
            writer.uint32(18).string(message.value);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseRequestSetOption };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.key = reader.string();
                    break;
                case 2:
                    message.value = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseRequestSetOption };
        if (object.key !== undefined && object.key !== null) {
            message.key = String(object.key);
        }
        else {
            message.key = "";
        }
        if (object.value !== undefined && object.value !== null) {
            message.value = String(object.value);
        }
        else {
            message.value = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.key !== undefined && (obj.key = message.key);
        message.value !== undefined && (obj.value = message.value);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseRequestSetOption };
        if (object.key !== undefined && object.key !== null) {
            message.key = object.key;
        }
        else {
            message.key = "";
        }
        if (object.value !== undefined && object.value !== null) {
            message.value = object.value;
        }
        else {
            message.value = "";
        }
        return message;
    },
};
const baseRequestInitChain = { chainId: "", initialHeight: 0 };
export const RequestInitChain = {
    encode(message, writer = Writer.create()) {
        if (message.time !== undefined) {
            Timestamp.encode(toTimestamp(message.time), writer.uint32(10).fork()).ldelim();
        }
        if (message.chainId !== "") {
            writer.uint32(18).string(message.chainId);
        }
        if (message.consensusParams !== undefined) {
            ConsensusParams.encode(message.consensusParams, writer.uint32(26).fork()).ldelim();
        }
        for (const v of message.validators) {
            ValidatorUpdate.encode(v, writer.uint32(34).fork()).ldelim();
        }
        if (message.appStateBytes.length !== 0) {
            writer.uint32(42).bytes(message.appStateBytes);
        }
        if (message.initialHeight !== 0) {
            writer.uint32(48).int64(message.initialHeight);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseRequestInitChain };
        message.validators = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.time = fromTimestamp(Timestamp.decode(reader, reader.uint32()));
                    break;
                case 2:
                    message.chainId = reader.string();
                    break;
                case 3:
                    message.consensusParams = ConsensusParams.decode(reader, reader.uint32());
                    break;
                case 4:
                    message.validators.push(ValidatorUpdate.decode(reader, reader.uint32()));
                    break;
                case 5:
                    message.appStateBytes = reader.bytes();
                    break;
                case 6:
                    message.initialHeight = longToNumber(reader.int64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseRequestInitChain };
        message.validators = [];
        if (object.time !== undefined && object.time !== null) {
            message.time = fromJsonTimestamp(object.time);
        }
        else {
            message.time = undefined;
        }
        if (object.chainId !== undefined && object.chainId !== null) {
            message.chainId = String(object.chainId);
        }
        else {
            message.chainId = "";
        }
        if (object.consensusParams !== undefined &&
            object.consensusParams !== null) {
            message.consensusParams = ConsensusParams.fromJSON(object.consensusParams);
        }
        else {
            message.consensusParams = undefined;
        }
        if (object.validators !== undefined && object.validators !== null) {
            for (const e of object.validators) {
                message.validators.push(ValidatorUpdate.fromJSON(e));
            }
        }
        if (object.appStateBytes !== undefined && object.appStateBytes !== null) {
            message.appStateBytes = bytesFromBase64(object.appStateBytes);
        }
        if (object.initialHeight !== undefined && object.initialHeight !== null) {
            message.initialHeight = Number(object.initialHeight);
        }
        else {
            message.initialHeight = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.time !== undefined &&
            (obj.time =
                message.time !== undefined ? message.time.toISOString() : null);
        message.chainId !== undefined && (obj.chainId = message.chainId);
        message.consensusParams !== undefined &&
            (obj.consensusParams = message.consensusParams
                ? ConsensusParams.toJSON(message.consensusParams)
                : undefined);
        if (message.validators) {
            obj.validators = message.validators.map((e) => e ? ValidatorUpdate.toJSON(e) : undefined);
        }
        else {
            obj.validators = [];
        }
        message.appStateBytes !== undefined &&
            (obj.appStateBytes = base64FromBytes(message.appStateBytes !== undefined
                ? message.appStateBytes
                : new Uint8Array()));
        message.initialHeight !== undefined &&
            (obj.initialHeight = message.initialHeight);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseRequestInitChain };
        message.validators = [];
        if (object.time !== undefined && object.time !== null) {
            message.time = object.time;
        }
        else {
            message.time = undefined;
        }
        if (object.chainId !== undefined && object.chainId !== null) {
            message.chainId = object.chainId;
        }
        else {
            message.chainId = "";
        }
        if (object.consensusParams !== undefined &&
            object.consensusParams !== null) {
            message.consensusParams = ConsensusParams.fromPartial(object.consensusParams);
        }
        else {
            message.consensusParams = undefined;
        }
        if (object.validators !== undefined && object.validators !== null) {
            for (const e of object.validators) {
                message.validators.push(ValidatorUpdate.fromPartial(e));
            }
        }
        if (object.appStateBytes !== undefined && object.appStateBytes !== null) {
            message.appStateBytes = object.appStateBytes;
        }
        else {
            message.appStateBytes = new Uint8Array();
        }
        if (object.initialHeight !== undefined && object.initialHeight !== null) {
            message.initialHeight = object.initialHeight;
        }
        else {
            message.initialHeight = 0;
        }
        return message;
    },
};
const baseRequestQuery = { path: "", height: 0, prove: false };
export const RequestQuery = {
    encode(message, writer = Writer.create()) {
        if (message.data.length !== 0) {
            writer.uint32(10).bytes(message.data);
        }
        if (message.path !== "") {
            writer.uint32(18).string(message.path);
        }
        if (message.height !== 0) {
            writer.uint32(24).int64(message.height);
        }
        if (message.prove === true) {
            writer.uint32(32).bool(message.prove);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseRequestQuery };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.data = reader.bytes();
                    break;
                case 2:
                    message.path = reader.string();
                    break;
                case 3:
                    message.height = longToNumber(reader.int64());
                    break;
                case 4:
                    message.prove = reader.bool();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseRequestQuery };
        if (object.data !== undefined && object.data !== null) {
            message.data = bytesFromBase64(object.data);
        }
        if (object.path !== undefined && object.path !== null) {
            message.path = String(object.path);
        }
        else {
            message.path = "";
        }
        if (object.height !== undefined && object.height !== null) {
            message.height = Number(object.height);
        }
        else {
            message.height = 0;
        }
        if (object.prove !== undefined && object.prove !== null) {
            message.prove = Boolean(object.prove);
        }
        else {
            message.prove = false;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.data !== undefined &&
            (obj.data = base64FromBytes(message.data !== undefined ? message.data : new Uint8Array()));
        message.path !== undefined && (obj.path = message.path);
        message.height !== undefined && (obj.height = message.height);
        message.prove !== undefined && (obj.prove = message.prove);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseRequestQuery };
        if (object.data !== undefined && object.data !== null) {
            message.data = object.data;
        }
        else {
            message.data = new Uint8Array();
        }
        if (object.path !== undefined && object.path !== null) {
            message.path = object.path;
        }
        else {
            message.path = "";
        }
        if (object.height !== undefined && object.height !== null) {
            message.height = object.height;
        }
        else {
            message.height = 0;
        }
        if (object.prove !== undefined && object.prove !== null) {
            message.prove = object.prove;
        }
        else {
            message.prove = false;
        }
        return message;
    },
};
const baseRequestBeginBlock = {};
export const RequestBeginBlock = {
    encode(message, writer = Writer.create()) {
        if (message.hash.length !== 0) {
            writer.uint32(10).bytes(message.hash);
        }
        if (message.header !== undefined) {
            Header.encode(message.header, writer.uint32(18).fork()).ldelim();
        }
        if (message.lastCommitInfo !== undefined) {
            LastCommitInfo.encode(message.lastCommitInfo, writer.uint32(26).fork()).ldelim();
        }
        for (const v of message.byzantineValidators) {
            Evidence.encode(v, writer.uint32(34).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseRequestBeginBlock };
        message.byzantineValidators = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.hash = reader.bytes();
                    break;
                case 2:
                    message.header = Header.decode(reader, reader.uint32());
                    break;
                case 3:
                    message.lastCommitInfo = LastCommitInfo.decode(reader, reader.uint32());
                    break;
                case 4:
                    message.byzantineValidators.push(Evidence.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseRequestBeginBlock };
        message.byzantineValidators = [];
        if (object.hash !== undefined && object.hash !== null) {
            message.hash = bytesFromBase64(object.hash);
        }
        if (object.header !== undefined && object.header !== null) {
            message.header = Header.fromJSON(object.header);
        }
        else {
            message.header = undefined;
        }
        if (object.lastCommitInfo !== undefined && object.lastCommitInfo !== null) {
            message.lastCommitInfo = LastCommitInfo.fromJSON(object.lastCommitInfo);
        }
        else {
            message.lastCommitInfo = undefined;
        }
        if (object.byzantineValidators !== undefined &&
            object.byzantineValidators !== null) {
            for (const e of object.byzantineValidators) {
                message.byzantineValidators.push(Evidence.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.hash !== undefined &&
            (obj.hash = base64FromBytes(message.hash !== undefined ? message.hash : new Uint8Array()));
        message.header !== undefined &&
            (obj.header = message.header ? Header.toJSON(message.header) : undefined);
        message.lastCommitInfo !== undefined &&
            (obj.lastCommitInfo = message.lastCommitInfo
                ? LastCommitInfo.toJSON(message.lastCommitInfo)
                : undefined);
        if (message.byzantineValidators) {
            obj.byzantineValidators = message.byzantineValidators.map((e) => e ? Evidence.toJSON(e) : undefined);
        }
        else {
            obj.byzantineValidators = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseRequestBeginBlock };
        message.byzantineValidators = [];
        if (object.hash !== undefined && object.hash !== null) {
            message.hash = object.hash;
        }
        else {
            message.hash = new Uint8Array();
        }
        if (object.header !== undefined && object.header !== null) {
            message.header = Header.fromPartial(object.header);
        }
        else {
            message.header = undefined;
        }
        if (object.lastCommitInfo !== undefined && object.lastCommitInfo !== null) {
            message.lastCommitInfo = LastCommitInfo.fromPartial(object.lastCommitInfo);
        }
        else {
            message.lastCommitInfo = undefined;
        }
        if (object.byzantineValidators !== undefined &&
            object.byzantineValidators !== null) {
            for (const e of object.byzantineValidators) {
                message.byzantineValidators.push(Evidence.fromPartial(e));
            }
        }
        return message;
    },
};
const baseRequestCheckTx = { type: 0 };
export const RequestCheckTx = {
    encode(message, writer = Writer.create()) {
        if (message.tx.length !== 0) {
            writer.uint32(10).bytes(message.tx);
        }
        if (message.type !== 0) {
            writer.uint32(16).int32(message.type);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseRequestCheckTx };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.tx = reader.bytes();
                    break;
                case 2:
                    message.type = reader.int32();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseRequestCheckTx };
        if (object.tx !== undefined && object.tx !== null) {
            message.tx = bytesFromBase64(object.tx);
        }
        if (object.type !== undefined && object.type !== null) {
            message.type = checkTxTypeFromJSON(object.type);
        }
        else {
            message.type = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.tx !== undefined &&
            (obj.tx = base64FromBytes(message.tx !== undefined ? message.tx : new Uint8Array()));
        message.type !== undefined && (obj.type = checkTxTypeToJSON(message.type));
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseRequestCheckTx };
        if (object.tx !== undefined && object.tx !== null) {
            message.tx = object.tx;
        }
        else {
            message.tx = new Uint8Array();
        }
        if (object.type !== undefined && object.type !== null) {
            message.type = object.type;
        }
        else {
            message.type = 0;
        }
        return message;
    },
};
const baseRequestDeliverTx = {};
export const RequestDeliverTx = {
    encode(message, writer = Writer.create()) {
        if (message.tx.length !== 0) {
            writer.uint32(10).bytes(message.tx);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseRequestDeliverTx };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.tx = reader.bytes();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseRequestDeliverTx };
        if (object.tx !== undefined && object.tx !== null) {
            message.tx = bytesFromBase64(object.tx);
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.tx !== undefined &&
            (obj.tx = base64FromBytes(message.tx !== undefined ? message.tx : new Uint8Array()));
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseRequestDeliverTx };
        if (object.tx !== undefined && object.tx !== null) {
            message.tx = object.tx;
        }
        else {
            message.tx = new Uint8Array();
        }
        return message;
    },
};
const baseRequestEndBlock = { height: 0 };
export const RequestEndBlock = {
    encode(message, writer = Writer.create()) {
        if (message.height !== 0) {
            writer.uint32(8).int64(message.height);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseRequestEndBlock };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.height = longToNumber(reader.int64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseRequestEndBlock };
        if (object.height !== undefined && object.height !== null) {
            message.height = Number(object.height);
        }
        else {
            message.height = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.height !== undefined && (obj.height = message.height);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseRequestEndBlock };
        if (object.height !== undefined && object.height !== null) {
            message.height = object.height;
        }
        else {
            message.height = 0;
        }
        return message;
    },
};
const baseRequestCommit = {};
export const RequestCommit = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseRequestCommit };
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
    fromJSON(_) {
        const message = { ...baseRequestCommit };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = { ...baseRequestCommit };
        return message;
    },
};
const baseRequestListSnapshots = {};
export const RequestListSnapshots = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseRequestListSnapshots };
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
    fromJSON(_) {
        const message = { ...baseRequestListSnapshots };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = { ...baseRequestListSnapshots };
        return message;
    },
};
const baseRequestOfferSnapshot = {};
export const RequestOfferSnapshot = {
    encode(message, writer = Writer.create()) {
        if (message.snapshot !== undefined) {
            Snapshot.encode(message.snapshot, writer.uint32(10).fork()).ldelim();
        }
        if (message.appHash.length !== 0) {
            writer.uint32(18).bytes(message.appHash);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseRequestOfferSnapshot };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.snapshot = Snapshot.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.appHash = reader.bytes();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseRequestOfferSnapshot };
        if (object.snapshot !== undefined && object.snapshot !== null) {
            message.snapshot = Snapshot.fromJSON(object.snapshot);
        }
        else {
            message.snapshot = undefined;
        }
        if (object.appHash !== undefined && object.appHash !== null) {
            message.appHash = bytesFromBase64(object.appHash);
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.snapshot !== undefined &&
            (obj.snapshot = message.snapshot
                ? Snapshot.toJSON(message.snapshot)
                : undefined);
        message.appHash !== undefined &&
            (obj.appHash = base64FromBytes(message.appHash !== undefined ? message.appHash : new Uint8Array()));
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseRequestOfferSnapshot };
        if (object.snapshot !== undefined && object.snapshot !== null) {
            message.snapshot = Snapshot.fromPartial(object.snapshot);
        }
        else {
            message.snapshot = undefined;
        }
        if (object.appHash !== undefined && object.appHash !== null) {
            message.appHash = object.appHash;
        }
        else {
            message.appHash = new Uint8Array();
        }
        return message;
    },
};
const baseRequestLoadSnapshotChunk = { height: 0, format: 0, chunk: 0 };
export const RequestLoadSnapshotChunk = {
    encode(message, writer = Writer.create()) {
        if (message.height !== 0) {
            writer.uint32(8).uint64(message.height);
        }
        if (message.format !== 0) {
            writer.uint32(16).uint32(message.format);
        }
        if (message.chunk !== 0) {
            writer.uint32(24).uint32(message.chunk);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseRequestLoadSnapshotChunk,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.height = longToNumber(reader.uint64());
                    break;
                case 2:
                    message.format = reader.uint32();
                    break;
                case 3:
                    message.chunk = reader.uint32();
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
            ...baseRequestLoadSnapshotChunk,
        };
        if (object.height !== undefined && object.height !== null) {
            message.height = Number(object.height);
        }
        else {
            message.height = 0;
        }
        if (object.format !== undefined && object.format !== null) {
            message.format = Number(object.format);
        }
        else {
            message.format = 0;
        }
        if (object.chunk !== undefined && object.chunk !== null) {
            message.chunk = Number(object.chunk);
        }
        else {
            message.chunk = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.height !== undefined && (obj.height = message.height);
        message.format !== undefined && (obj.format = message.format);
        message.chunk !== undefined && (obj.chunk = message.chunk);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseRequestLoadSnapshotChunk,
        };
        if (object.height !== undefined && object.height !== null) {
            message.height = object.height;
        }
        else {
            message.height = 0;
        }
        if (object.format !== undefined && object.format !== null) {
            message.format = object.format;
        }
        else {
            message.format = 0;
        }
        if (object.chunk !== undefined && object.chunk !== null) {
            message.chunk = object.chunk;
        }
        else {
            message.chunk = 0;
        }
        return message;
    },
};
const baseRequestApplySnapshotChunk = { index: 0, sender: "" };
export const RequestApplySnapshotChunk = {
    encode(message, writer = Writer.create()) {
        if (message.index !== 0) {
            writer.uint32(8).uint32(message.index);
        }
        if (message.chunk.length !== 0) {
            writer.uint32(18).bytes(message.chunk);
        }
        if (message.sender !== "") {
            writer.uint32(26).string(message.sender);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseRequestApplySnapshotChunk,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.index = reader.uint32();
                    break;
                case 2:
                    message.chunk = reader.bytes();
                    break;
                case 3:
                    message.sender = reader.string();
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
            ...baseRequestApplySnapshotChunk,
        };
        if (object.index !== undefined && object.index !== null) {
            message.index = Number(object.index);
        }
        else {
            message.index = 0;
        }
        if (object.chunk !== undefined && object.chunk !== null) {
            message.chunk = bytesFromBase64(object.chunk);
        }
        if (object.sender !== undefined && object.sender !== null) {
            message.sender = String(object.sender);
        }
        else {
            message.sender = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.index !== undefined && (obj.index = message.index);
        message.chunk !== undefined &&
            (obj.chunk = base64FromBytes(message.chunk !== undefined ? message.chunk : new Uint8Array()));
        message.sender !== undefined && (obj.sender = message.sender);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseRequestApplySnapshotChunk,
        };
        if (object.index !== undefined && object.index !== null) {
            message.index = object.index;
        }
        else {
            message.index = 0;
        }
        if (object.chunk !== undefined && object.chunk !== null) {
            message.chunk = object.chunk;
        }
        else {
            message.chunk = new Uint8Array();
        }
        if (object.sender !== undefined && object.sender !== null) {
            message.sender = object.sender;
        }
        else {
            message.sender = "";
        }
        return message;
    },
};
const baseResponse = {};
export const Response = {
    encode(message, writer = Writer.create()) {
        if (message.exception !== undefined) {
            ResponseException.encode(message.exception, writer.uint32(10).fork()).ldelim();
        }
        if (message.echo !== undefined) {
            ResponseEcho.encode(message.echo, writer.uint32(18).fork()).ldelim();
        }
        if (message.flush !== undefined) {
            ResponseFlush.encode(message.flush, writer.uint32(26).fork()).ldelim();
        }
        if (message.info !== undefined) {
            ResponseInfo.encode(message.info, writer.uint32(34).fork()).ldelim();
        }
        if (message.setOption !== undefined) {
            ResponseSetOption.encode(message.setOption, writer.uint32(42).fork()).ldelim();
        }
        if (message.initChain !== undefined) {
            ResponseInitChain.encode(message.initChain, writer.uint32(50).fork()).ldelim();
        }
        if (message.query !== undefined) {
            ResponseQuery.encode(message.query, writer.uint32(58).fork()).ldelim();
        }
        if (message.beginBlock !== undefined) {
            ResponseBeginBlock.encode(message.beginBlock, writer.uint32(66).fork()).ldelim();
        }
        if (message.checkTx !== undefined) {
            ResponseCheckTx.encode(message.checkTx, writer.uint32(74).fork()).ldelim();
        }
        if (message.deliverTx !== undefined) {
            ResponseDeliverTx.encode(message.deliverTx, writer.uint32(82).fork()).ldelim();
        }
        if (message.endBlock !== undefined) {
            ResponseEndBlock.encode(message.endBlock, writer.uint32(90).fork()).ldelim();
        }
        if (message.commit !== undefined) {
            ResponseCommit.encode(message.commit, writer.uint32(98).fork()).ldelim();
        }
        if (message.listSnapshots !== undefined) {
            ResponseListSnapshots.encode(message.listSnapshots, writer.uint32(106).fork()).ldelim();
        }
        if (message.offerSnapshot !== undefined) {
            ResponseOfferSnapshot.encode(message.offerSnapshot, writer.uint32(114).fork()).ldelim();
        }
        if (message.loadSnapshotChunk !== undefined) {
            ResponseLoadSnapshotChunk.encode(message.loadSnapshotChunk, writer.uint32(122).fork()).ldelim();
        }
        if (message.applySnapshotChunk !== undefined) {
            ResponseApplySnapshotChunk.encode(message.applySnapshotChunk, writer.uint32(130).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseResponse };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.exception = ResponseException.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.echo = ResponseEcho.decode(reader, reader.uint32());
                    break;
                case 3:
                    message.flush = ResponseFlush.decode(reader, reader.uint32());
                    break;
                case 4:
                    message.info = ResponseInfo.decode(reader, reader.uint32());
                    break;
                case 5:
                    message.setOption = ResponseSetOption.decode(reader, reader.uint32());
                    break;
                case 6:
                    message.initChain = ResponseInitChain.decode(reader, reader.uint32());
                    break;
                case 7:
                    message.query = ResponseQuery.decode(reader, reader.uint32());
                    break;
                case 8:
                    message.beginBlock = ResponseBeginBlock.decode(reader, reader.uint32());
                    break;
                case 9:
                    message.checkTx = ResponseCheckTx.decode(reader, reader.uint32());
                    break;
                case 10:
                    message.deliverTx = ResponseDeliverTx.decode(reader, reader.uint32());
                    break;
                case 11:
                    message.endBlock = ResponseEndBlock.decode(reader, reader.uint32());
                    break;
                case 12:
                    message.commit = ResponseCommit.decode(reader, reader.uint32());
                    break;
                case 13:
                    message.listSnapshots = ResponseListSnapshots.decode(reader, reader.uint32());
                    break;
                case 14:
                    message.offerSnapshot = ResponseOfferSnapshot.decode(reader, reader.uint32());
                    break;
                case 15:
                    message.loadSnapshotChunk = ResponseLoadSnapshotChunk.decode(reader, reader.uint32());
                    break;
                case 16:
                    message.applySnapshotChunk = ResponseApplySnapshotChunk.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseResponse };
        if (object.exception !== undefined && object.exception !== null) {
            message.exception = ResponseException.fromJSON(object.exception);
        }
        else {
            message.exception = undefined;
        }
        if (object.echo !== undefined && object.echo !== null) {
            message.echo = ResponseEcho.fromJSON(object.echo);
        }
        else {
            message.echo = undefined;
        }
        if (object.flush !== undefined && object.flush !== null) {
            message.flush = ResponseFlush.fromJSON(object.flush);
        }
        else {
            message.flush = undefined;
        }
        if (object.info !== undefined && object.info !== null) {
            message.info = ResponseInfo.fromJSON(object.info);
        }
        else {
            message.info = undefined;
        }
        if (object.setOption !== undefined && object.setOption !== null) {
            message.setOption = ResponseSetOption.fromJSON(object.setOption);
        }
        else {
            message.setOption = undefined;
        }
        if (object.initChain !== undefined && object.initChain !== null) {
            message.initChain = ResponseInitChain.fromJSON(object.initChain);
        }
        else {
            message.initChain = undefined;
        }
        if (object.query !== undefined && object.query !== null) {
            message.query = ResponseQuery.fromJSON(object.query);
        }
        else {
            message.query = undefined;
        }
        if (object.beginBlock !== undefined && object.beginBlock !== null) {
            message.beginBlock = ResponseBeginBlock.fromJSON(object.beginBlock);
        }
        else {
            message.beginBlock = undefined;
        }
        if (object.checkTx !== undefined && object.checkTx !== null) {
            message.checkTx = ResponseCheckTx.fromJSON(object.checkTx);
        }
        else {
            message.checkTx = undefined;
        }
        if (object.deliverTx !== undefined && object.deliverTx !== null) {
            message.deliverTx = ResponseDeliverTx.fromJSON(object.deliverTx);
        }
        else {
            message.deliverTx = undefined;
        }
        if (object.endBlock !== undefined && object.endBlock !== null) {
            message.endBlock = ResponseEndBlock.fromJSON(object.endBlock);
        }
        else {
            message.endBlock = undefined;
        }
        if (object.commit !== undefined && object.commit !== null) {
            message.commit = ResponseCommit.fromJSON(object.commit);
        }
        else {
            message.commit = undefined;
        }
        if (object.listSnapshots !== undefined && object.listSnapshots !== null) {
            message.listSnapshots = ResponseListSnapshots.fromJSON(object.listSnapshots);
        }
        else {
            message.listSnapshots = undefined;
        }
        if (object.offerSnapshot !== undefined && object.offerSnapshot !== null) {
            message.offerSnapshot = ResponseOfferSnapshot.fromJSON(object.offerSnapshot);
        }
        else {
            message.offerSnapshot = undefined;
        }
        if (object.loadSnapshotChunk !== undefined &&
            object.loadSnapshotChunk !== null) {
            message.loadSnapshotChunk = ResponseLoadSnapshotChunk.fromJSON(object.loadSnapshotChunk);
        }
        else {
            message.loadSnapshotChunk = undefined;
        }
        if (object.applySnapshotChunk !== undefined &&
            object.applySnapshotChunk !== null) {
            message.applySnapshotChunk = ResponseApplySnapshotChunk.fromJSON(object.applySnapshotChunk);
        }
        else {
            message.applySnapshotChunk = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.exception !== undefined &&
            (obj.exception = message.exception
                ? ResponseException.toJSON(message.exception)
                : undefined);
        message.echo !== undefined &&
            (obj.echo = message.echo ? ResponseEcho.toJSON(message.echo) : undefined);
        message.flush !== undefined &&
            (obj.flush = message.flush
                ? ResponseFlush.toJSON(message.flush)
                : undefined);
        message.info !== undefined &&
            (obj.info = message.info ? ResponseInfo.toJSON(message.info) : undefined);
        message.setOption !== undefined &&
            (obj.setOption = message.setOption
                ? ResponseSetOption.toJSON(message.setOption)
                : undefined);
        message.initChain !== undefined &&
            (obj.initChain = message.initChain
                ? ResponseInitChain.toJSON(message.initChain)
                : undefined);
        message.query !== undefined &&
            (obj.query = message.query
                ? ResponseQuery.toJSON(message.query)
                : undefined);
        message.beginBlock !== undefined &&
            (obj.beginBlock = message.beginBlock
                ? ResponseBeginBlock.toJSON(message.beginBlock)
                : undefined);
        message.checkTx !== undefined &&
            (obj.checkTx = message.checkTx
                ? ResponseCheckTx.toJSON(message.checkTx)
                : undefined);
        message.deliverTx !== undefined &&
            (obj.deliverTx = message.deliverTx
                ? ResponseDeliverTx.toJSON(message.deliverTx)
                : undefined);
        message.endBlock !== undefined &&
            (obj.endBlock = message.endBlock
                ? ResponseEndBlock.toJSON(message.endBlock)
                : undefined);
        message.commit !== undefined &&
            (obj.commit = message.commit
                ? ResponseCommit.toJSON(message.commit)
                : undefined);
        message.listSnapshots !== undefined &&
            (obj.listSnapshots = message.listSnapshots
                ? ResponseListSnapshots.toJSON(message.listSnapshots)
                : undefined);
        message.offerSnapshot !== undefined &&
            (obj.offerSnapshot = message.offerSnapshot
                ? ResponseOfferSnapshot.toJSON(message.offerSnapshot)
                : undefined);
        message.loadSnapshotChunk !== undefined &&
            (obj.loadSnapshotChunk = message.loadSnapshotChunk
                ? ResponseLoadSnapshotChunk.toJSON(message.loadSnapshotChunk)
                : undefined);
        message.applySnapshotChunk !== undefined &&
            (obj.applySnapshotChunk = message.applySnapshotChunk
                ? ResponseApplySnapshotChunk.toJSON(message.applySnapshotChunk)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseResponse };
        if (object.exception !== undefined && object.exception !== null) {
            message.exception = ResponseException.fromPartial(object.exception);
        }
        else {
            message.exception = undefined;
        }
        if (object.echo !== undefined && object.echo !== null) {
            message.echo = ResponseEcho.fromPartial(object.echo);
        }
        else {
            message.echo = undefined;
        }
        if (object.flush !== undefined && object.flush !== null) {
            message.flush = ResponseFlush.fromPartial(object.flush);
        }
        else {
            message.flush = undefined;
        }
        if (object.info !== undefined && object.info !== null) {
            message.info = ResponseInfo.fromPartial(object.info);
        }
        else {
            message.info = undefined;
        }
        if (object.setOption !== undefined && object.setOption !== null) {
            message.setOption = ResponseSetOption.fromPartial(object.setOption);
        }
        else {
            message.setOption = undefined;
        }
        if (object.initChain !== undefined && object.initChain !== null) {
            message.initChain = ResponseInitChain.fromPartial(object.initChain);
        }
        else {
            message.initChain = undefined;
        }
        if (object.query !== undefined && object.query !== null) {
            message.query = ResponseQuery.fromPartial(object.query);
        }
        else {
            message.query = undefined;
        }
        if (object.beginBlock !== undefined && object.beginBlock !== null) {
            message.beginBlock = ResponseBeginBlock.fromPartial(object.beginBlock);
        }
        else {
            message.beginBlock = undefined;
        }
        if (object.checkTx !== undefined && object.checkTx !== null) {
            message.checkTx = ResponseCheckTx.fromPartial(object.checkTx);
        }
        else {
            message.checkTx = undefined;
        }
        if (object.deliverTx !== undefined && object.deliverTx !== null) {
            message.deliverTx = ResponseDeliverTx.fromPartial(object.deliverTx);
        }
        else {
            message.deliverTx = undefined;
        }
        if (object.endBlock !== undefined && object.endBlock !== null) {
            message.endBlock = ResponseEndBlock.fromPartial(object.endBlock);
        }
        else {
            message.endBlock = undefined;
        }
        if (object.commit !== undefined && object.commit !== null) {
            message.commit = ResponseCommit.fromPartial(object.commit);
        }
        else {
            message.commit = undefined;
        }
        if (object.listSnapshots !== undefined && object.listSnapshots !== null) {
            message.listSnapshots = ResponseListSnapshots.fromPartial(object.listSnapshots);
        }
        else {
            message.listSnapshots = undefined;
        }
        if (object.offerSnapshot !== undefined && object.offerSnapshot !== null) {
            message.offerSnapshot = ResponseOfferSnapshot.fromPartial(object.offerSnapshot);
        }
        else {
            message.offerSnapshot = undefined;
        }
        if (object.loadSnapshotChunk !== undefined &&
            object.loadSnapshotChunk !== null) {
            message.loadSnapshotChunk = ResponseLoadSnapshotChunk.fromPartial(object.loadSnapshotChunk);
        }
        else {
            message.loadSnapshotChunk = undefined;
        }
        if (object.applySnapshotChunk !== undefined &&
            object.applySnapshotChunk !== null) {
            message.applySnapshotChunk = ResponseApplySnapshotChunk.fromPartial(object.applySnapshotChunk);
        }
        else {
            message.applySnapshotChunk = undefined;
        }
        return message;
    },
};
const baseResponseException = { error: "" };
export const ResponseException = {
    encode(message, writer = Writer.create()) {
        if (message.error !== "") {
            writer.uint32(10).string(message.error);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseResponseException };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.error = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseResponseException };
        if (object.error !== undefined && object.error !== null) {
            message.error = String(object.error);
        }
        else {
            message.error = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.error !== undefined && (obj.error = message.error);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseResponseException };
        if (object.error !== undefined && object.error !== null) {
            message.error = object.error;
        }
        else {
            message.error = "";
        }
        return message;
    },
};
const baseResponseEcho = { message: "" };
export const ResponseEcho = {
    encode(message, writer = Writer.create()) {
        if (message.message !== "") {
            writer.uint32(10).string(message.message);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseResponseEcho };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.message = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseResponseEcho };
        if (object.message !== undefined && object.message !== null) {
            message.message = String(object.message);
        }
        else {
            message.message = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.message !== undefined && (obj.message = message.message);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseResponseEcho };
        if (object.message !== undefined && object.message !== null) {
            message.message = object.message;
        }
        else {
            message.message = "";
        }
        return message;
    },
};
const baseResponseFlush = {};
export const ResponseFlush = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseResponseFlush };
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
    fromJSON(_) {
        const message = { ...baseResponseFlush };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = { ...baseResponseFlush };
        return message;
    },
};
const baseResponseInfo = {
    data: "",
    version: "",
    appVersion: 0,
    lastBlockHeight: 0,
};
export const ResponseInfo = {
    encode(message, writer = Writer.create()) {
        if (message.data !== "") {
            writer.uint32(10).string(message.data);
        }
        if (message.version !== "") {
            writer.uint32(18).string(message.version);
        }
        if (message.appVersion !== 0) {
            writer.uint32(24).uint64(message.appVersion);
        }
        if (message.lastBlockHeight !== 0) {
            writer.uint32(32).int64(message.lastBlockHeight);
        }
        if (message.lastBlockAppHash.length !== 0) {
            writer.uint32(42).bytes(message.lastBlockAppHash);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseResponseInfo };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.data = reader.string();
                    break;
                case 2:
                    message.version = reader.string();
                    break;
                case 3:
                    message.appVersion = longToNumber(reader.uint64());
                    break;
                case 4:
                    message.lastBlockHeight = longToNumber(reader.int64());
                    break;
                case 5:
                    message.lastBlockAppHash = reader.bytes();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseResponseInfo };
        if (object.data !== undefined && object.data !== null) {
            message.data = String(object.data);
        }
        else {
            message.data = "";
        }
        if (object.version !== undefined && object.version !== null) {
            message.version = String(object.version);
        }
        else {
            message.version = "";
        }
        if (object.appVersion !== undefined && object.appVersion !== null) {
            message.appVersion = Number(object.appVersion);
        }
        else {
            message.appVersion = 0;
        }
        if (object.lastBlockHeight !== undefined &&
            object.lastBlockHeight !== null) {
            message.lastBlockHeight = Number(object.lastBlockHeight);
        }
        else {
            message.lastBlockHeight = 0;
        }
        if (object.lastBlockAppHash !== undefined &&
            object.lastBlockAppHash !== null) {
            message.lastBlockAppHash = bytesFromBase64(object.lastBlockAppHash);
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.data !== undefined && (obj.data = message.data);
        message.version !== undefined && (obj.version = message.version);
        message.appVersion !== undefined && (obj.appVersion = message.appVersion);
        message.lastBlockHeight !== undefined &&
            (obj.lastBlockHeight = message.lastBlockHeight);
        message.lastBlockAppHash !== undefined &&
            (obj.lastBlockAppHash = base64FromBytes(message.lastBlockAppHash !== undefined
                ? message.lastBlockAppHash
                : new Uint8Array()));
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseResponseInfo };
        if (object.data !== undefined && object.data !== null) {
            message.data = object.data;
        }
        else {
            message.data = "";
        }
        if (object.version !== undefined && object.version !== null) {
            message.version = object.version;
        }
        else {
            message.version = "";
        }
        if (object.appVersion !== undefined && object.appVersion !== null) {
            message.appVersion = object.appVersion;
        }
        else {
            message.appVersion = 0;
        }
        if (object.lastBlockHeight !== undefined &&
            object.lastBlockHeight !== null) {
            message.lastBlockHeight = object.lastBlockHeight;
        }
        else {
            message.lastBlockHeight = 0;
        }
        if (object.lastBlockAppHash !== undefined &&
            object.lastBlockAppHash !== null) {
            message.lastBlockAppHash = object.lastBlockAppHash;
        }
        else {
            message.lastBlockAppHash = new Uint8Array();
        }
        return message;
    },
};
const baseResponseSetOption = { code: 0, log: "", info: "" };
export const ResponseSetOption = {
    encode(message, writer = Writer.create()) {
        if (message.code !== 0) {
            writer.uint32(8).uint32(message.code);
        }
        if (message.log !== "") {
            writer.uint32(26).string(message.log);
        }
        if (message.info !== "") {
            writer.uint32(34).string(message.info);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseResponseSetOption };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.code = reader.uint32();
                    break;
                case 3:
                    message.log = reader.string();
                    break;
                case 4:
                    message.info = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseResponseSetOption };
        if (object.code !== undefined && object.code !== null) {
            message.code = Number(object.code);
        }
        else {
            message.code = 0;
        }
        if (object.log !== undefined && object.log !== null) {
            message.log = String(object.log);
        }
        else {
            message.log = "";
        }
        if (object.info !== undefined && object.info !== null) {
            message.info = String(object.info);
        }
        else {
            message.info = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.code !== undefined && (obj.code = message.code);
        message.log !== undefined && (obj.log = message.log);
        message.info !== undefined && (obj.info = message.info);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseResponseSetOption };
        if (object.code !== undefined && object.code !== null) {
            message.code = object.code;
        }
        else {
            message.code = 0;
        }
        if (object.log !== undefined && object.log !== null) {
            message.log = object.log;
        }
        else {
            message.log = "";
        }
        if (object.info !== undefined && object.info !== null) {
            message.info = object.info;
        }
        else {
            message.info = "";
        }
        return message;
    },
};
const baseResponseInitChain = {};
export const ResponseInitChain = {
    encode(message, writer = Writer.create()) {
        if (message.consensusParams !== undefined) {
            ConsensusParams.encode(message.consensusParams, writer.uint32(10).fork()).ldelim();
        }
        for (const v of message.validators) {
            ValidatorUpdate.encode(v, writer.uint32(18).fork()).ldelim();
        }
        if (message.appHash.length !== 0) {
            writer.uint32(26).bytes(message.appHash);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseResponseInitChain };
        message.validators = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.consensusParams = ConsensusParams.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.validators.push(ValidatorUpdate.decode(reader, reader.uint32()));
                    break;
                case 3:
                    message.appHash = reader.bytes();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseResponseInitChain };
        message.validators = [];
        if (object.consensusParams !== undefined &&
            object.consensusParams !== null) {
            message.consensusParams = ConsensusParams.fromJSON(object.consensusParams);
        }
        else {
            message.consensusParams = undefined;
        }
        if (object.validators !== undefined && object.validators !== null) {
            for (const e of object.validators) {
                message.validators.push(ValidatorUpdate.fromJSON(e));
            }
        }
        if (object.appHash !== undefined && object.appHash !== null) {
            message.appHash = bytesFromBase64(object.appHash);
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.consensusParams !== undefined &&
            (obj.consensusParams = message.consensusParams
                ? ConsensusParams.toJSON(message.consensusParams)
                : undefined);
        if (message.validators) {
            obj.validators = message.validators.map((e) => e ? ValidatorUpdate.toJSON(e) : undefined);
        }
        else {
            obj.validators = [];
        }
        message.appHash !== undefined &&
            (obj.appHash = base64FromBytes(message.appHash !== undefined ? message.appHash : new Uint8Array()));
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseResponseInitChain };
        message.validators = [];
        if (object.consensusParams !== undefined &&
            object.consensusParams !== null) {
            message.consensusParams = ConsensusParams.fromPartial(object.consensusParams);
        }
        else {
            message.consensusParams = undefined;
        }
        if (object.validators !== undefined && object.validators !== null) {
            for (const e of object.validators) {
                message.validators.push(ValidatorUpdate.fromPartial(e));
            }
        }
        if (object.appHash !== undefined && object.appHash !== null) {
            message.appHash = object.appHash;
        }
        else {
            message.appHash = new Uint8Array();
        }
        return message;
    },
};
const baseResponseQuery = {
    code: 0,
    log: "",
    info: "",
    index: 0,
    height: 0,
    codespace: "",
};
export const ResponseQuery = {
    encode(message, writer = Writer.create()) {
        if (message.code !== 0) {
            writer.uint32(8).uint32(message.code);
        }
        if (message.log !== "") {
            writer.uint32(26).string(message.log);
        }
        if (message.info !== "") {
            writer.uint32(34).string(message.info);
        }
        if (message.index !== 0) {
            writer.uint32(40).int64(message.index);
        }
        if (message.key.length !== 0) {
            writer.uint32(50).bytes(message.key);
        }
        if (message.value.length !== 0) {
            writer.uint32(58).bytes(message.value);
        }
        if (message.proofOps !== undefined) {
            ProofOps.encode(message.proofOps, writer.uint32(66).fork()).ldelim();
        }
        if (message.height !== 0) {
            writer.uint32(72).int64(message.height);
        }
        if (message.codespace !== "") {
            writer.uint32(82).string(message.codespace);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseResponseQuery };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.code = reader.uint32();
                    break;
                case 3:
                    message.log = reader.string();
                    break;
                case 4:
                    message.info = reader.string();
                    break;
                case 5:
                    message.index = longToNumber(reader.int64());
                    break;
                case 6:
                    message.key = reader.bytes();
                    break;
                case 7:
                    message.value = reader.bytes();
                    break;
                case 8:
                    message.proofOps = ProofOps.decode(reader, reader.uint32());
                    break;
                case 9:
                    message.height = longToNumber(reader.int64());
                    break;
                case 10:
                    message.codespace = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseResponseQuery };
        if (object.code !== undefined && object.code !== null) {
            message.code = Number(object.code);
        }
        else {
            message.code = 0;
        }
        if (object.log !== undefined && object.log !== null) {
            message.log = String(object.log);
        }
        else {
            message.log = "";
        }
        if (object.info !== undefined && object.info !== null) {
            message.info = String(object.info);
        }
        else {
            message.info = "";
        }
        if (object.index !== undefined && object.index !== null) {
            message.index = Number(object.index);
        }
        else {
            message.index = 0;
        }
        if (object.key !== undefined && object.key !== null) {
            message.key = bytesFromBase64(object.key);
        }
        if (object.value !== undefined && object.value !== null) {
            message.value = bytesFromBase64(object.value);
        }
        if (object.proofOps !== undefined && object.proofOps !== null) {
            message.proofOps = ProofOps.fromJSON(object.proofOps);
        }
        else {
            message.proofOps = undefined;
        }
        if (object.height !== undefined && object.height !== null) {
            message.height = Number(object.height);
        }
        else {
            message.height = 0;
        }
        if (object.codespace !== undefined && object.codespace !== null) {
            message.codespace = String(object.codespace);
        }
        else {
            message.codespace = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.code !== undefined && (obj.code = message.code);
        message.log !== undefined && (obj.log = message.log);
        message.info !== undefined && (obj.info = message.info);
        message.index !== undefined && (obj.index = message.index);
        message.key !== undefined &&
            (obj.key = base64FromBytes(message.key !== undefined ? message.key : new Uint8Array()));
        message.value !== undefined &&
            (obj.value = base64FromBytes(message.value !== undefined ? message.value : new Uint8Array()));
        message.proofOps !== undefined &&
            (obj.proofOps = message.proofOps
                ? ProofOps.toJSON(message.proofOps)
                : undefined);
        message.height !== undefined && (obj.height = message.height);
        message.codespace !== undefined && (obj.codespace = message.codespace);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseResponseQuery };
        if (object.code !== undefined && object.code !== null) {
            message.code = object.code;
        }
        else {
            message.code = 0;
        }
        if (object.log !== undefined && object.log !== null) {
            message.log = object.log;
        }
        else {
            message.log = "";
        }
        if (object.info !== undefined && object.info !== null) {
            message.info = object.info;
        }
        else {
            message.info = "";
        }
        if (object.index !== undefined && object.index !== null) {
            message.index = object.index;
        }
        else {
            message.index = 0;
        }
        if (object.key !== undefined && object.key !== null) {
            message.key = object.key;
        }
        else {
            message.key = new Uint8Array();
        }
        if (object.value !== undefined && object.value !== null) {
            message.value = object.value;
        }
        else {
            message.value = new Uint8Array();
        }
        if (object.proofOps !== undefined && object.proofOps !== null) {
            message.proofOps = ProofOps.fromPartial(object.proofOps);
        }
        else {
            message.proofOps = undefined;
        }
        if (object.height !== undefined && object.height !== null) {
            message.height = object.height;
        }
        else {
            message.height = 0;
        }
        if (object.codespace !== undefined && object.codespace !== null) {
            message.codespace = object.codespace;
        }
        else {
            message.codespace = "";
        }
        return message;
    },
};
const baseResponseBeginBlock = {};
export const ResponseBeginBlock = {
    encode(message, writer = Writer.create()) {
        for (const v of message.events) {
            Event.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseResponseBeginBlock };
        message.events = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.events.push(Event.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseResponseBeginBlock };
        message.events = [];
        if (object.events !== undefined && object.events !== null) {
            for (const e of object.events) {
                message.events.push(Event.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.events) {
            obj.events = message.events.map((e) => (e ? Event.toJSON(e) : undefined));
        }
        else {
            obj.events = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseResponseBeginBlock };
        message.events = [];
        if (object.events !== undefined && object.events !== null) {
            for (const e of object.events) {
                message.events.push(Event.fromPartial(e));
            }
        }
        return message;
    },
};
const baseResponseCheckTx = {
    code: 0,
    log: "",
    info: "",
    gasWanted: 0,
    gasUsed: 0,
    codespace: "",
};
export const ResponseCheckTx = {
    encode(message, writer = Writer.create()) {
        if (message.code !== 0) {
            writer.uint32(8).uint32(message.code);
        }
        if (message.data.length !== 0) {
            writer.uint32(18).bytes(message.data);
        }
        if (message.log !== "") {
            writer.uint32(26).string(message.log);
        }
        if (message.info !== "") {
            writer.uint32(34).string(message.info);
        }
        if (message.gasWanted !== 0) {
            writer.uint32(40).int64(message.gasWanted);
        }
        if (message.gasUsed !== 0) {
            writer.uint32(48).int64(message.gasUsed);
        }
        for (const v of message.events) {
            Event.encode(v, writer.uint32(58).fork()).ldelim();
        }
        if (message.codespace !== "") {
            writer.uint32(66).string(message.codespace);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseResponseCheckTx };
        message.events = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.code = reader.uint32();
                    break;
                case 2:
                    message.data = reader.bytes();
                    break;
                case 3:
                    message.log = reader.string();
                    break;
                case 4:
                    message.info = reader.string();
                    break;
                case 5:
                    message.gasWanted = longToNumber(reader.int64());
                    break;
                case 6:
                    message.gasUsed = longToNumber(reader.int64());
                    break;
                case 7:
                    message.events.push(Event.decode(reader, reader.uint32()));
                    break;
                case 8:
                    message.codespace = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseResponseCheckTx };
        message.events = [];
        if (object.code !== undefined && object.code !== null) {
            message.code = Number(object.code);
        }
        else {
            message.code = 0;
        }
        if (object.data !== undefined && object.data !== null) {
            message.data = bytesFromBase64(object.data);
        }
        if (object.log !== undefined && object.log !== null) {
            message.log = String(object.log);
        }
        else {
            message.log = "";
        }
        if (object.info !== undefined && object.info !== null) {
            message.info = String(object.info);
        }
        else {
            message.info = "";
        }
        if (object.gasWanted !== undefined && object.gasWanted !== null) {
            message.gasWanted = Number(object.gasWanted);
        }
        else {
            message.gasWanted = 0;
        }
        if (object.gasUsed !== undefined && object.gasUsed !== null) {
            message.gasUsed = Number(object.gasUsed);
        }
        else {
            message.gasUsed = 0;
        }
        if (object.events !== undefined && object.events !== null) {
            for (const e of object.events) {
                message.events.push(Event.fromJSON(e));
            }
        }
        if (object.codespace !== undefined && object.codespace !== null) {
            message.codespace = String(object.codespace);
        }
        else {
            message.codespace = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.code !== undefined && (obj.code = message.code);
        message.data !== undefined &&
            (obj.data = base64FromBytes(message.data !== undefined ? message.data : new Uint8Array()));
        message.log !== undefined && (obj.log = message.log);
        message.info !== undefined && (obj.info = message.info);
        message.gasWanted !== undefined && (obj.gasWanted = message.gasWanted);
        message.gasUsed !== undefined && (obj.gasUsed = message.gasUsed);
        if (message.events) {
            obj.events = message.events.map((e) => (e ? Event.toJSON(e) : undefined));
        }
        else {
            obj.events = [];
        }
        message.codespace !== undefined && (obj.codespace = message.codespace);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseResponseCheckTx };
        message.events = [];
        if (object.code !== undefined && object.code !== null) {
            message.code = object.code;
        }
        else {
            message.code = 0;
        }
        if (object.data !== undefined && object.data !== null) {
            message.data = object.data;
        }
        else {
            message.data = new Uint8Array();
        }
        if (object.log !== undefined && object.log !== null) {
            message.log = object.log;
        }
        else {
            message.log = "";
        }
        if (object.info !== undefined && object.info !== null) {
            message.info = object.info;
        }
        else {
            message.info = "";
        }
        if (object.gasWanted !== undefined && object.gasWanted !== null) {
            message.gasWanted = object.gasWanted;
        }
        else {
            message.gasWanted = 0;
        }
        if (object.gasUsed !== undefined && object.gasUsed !== null) {
            message.gasUsed = object.gasUsed;
        }
        else {
            message.gasUsed = 0;
        }
        if (object.events !== undefined && object.events !== null) {
            for (const e of object.events) {
                message.events.push(Event.fromPartial(e));
            }
        }
        if (object.codespace !== undefined && object.codespace !== null) {
            message.codespace = object.codespace;
        }
        else {
            message.codespace = "";
        }
        return message;
    },
};
const baseResponseDeliverTx = {
    code: 0,
    log: "",
    info: "",
    gasWanted: 0,
    gasUsed: 0,
    codespace: "",
};
export const ResponseDeliverTx = {
    encode(message, writer = Writer.create()) {
        if (message.code !== 0) {
            writer.uint32(8).uint32(message.code);
        }
        if (message.data.length !== 0) {
            writer.uint32(18).bytes(message.data);
        }
        if (message.log !== "") {
            writer.uint32(26).string(message.log);
        }
        if (message.info !== "") {
            writer.uint32(34).string(message.info);
        }
        if (message.gasWanted !== 0) {
            writer.uint32(40).int64(message.gasWanted);
        }
        if (message.gasUsed !== 0) {
            writer.uint32(48).int64(message.gasUsed);
        }
        for (const v of message.events) {
            Event.encode(v, writer.uint32(58).fork()).ldelim();
        }
        if (message.codespace !== "") {
            writer.uint32(66).string(message.codespace);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseResponseDeliverTx };
        message.events = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.code = reader.uint32();
                    break;
                case 2:
                    message.data = reader.bytes();
                    break;
                case 3:
                    message.log = reader.string();
                    break;
                case 4:
                    message.info = reader.string();
                    break;
                case 5:
                    message.gasWanted = longToNumber(reader.int64());
                    break;
                case 6:
                    message.gasUsed = longToNumber(reader.int64());
                    break;
                case 7:
                    message.events.push(Event.decode(reader, reader.uint32()));
                    break;
                case 8:
                    message.codespace = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseResponseDeliverTx };
        message.events = [];
        if (object.code !== undefined && object.code !== null) {
            message.code = Number(object.code);
        }
        else {
            message.code = 0;
        }
        if (object.data !== undefined && object.data !== null) {
            message.data = bytesFromBase64(object.data);
        }
        if (object.log !== undefined && object.log !== null) {
            message.log = String(object.log);
        }
        else {
            message.log = "";
        }
        if (object.info !== undefined && object.info !== null) {
            message.info = String(object.info);
        }
        else {
            message.info = "";
        }
        if (object.gasWanted !== undefined && object.gasWanted !== null) {
            message.gasWanted = Number(object.gasWanted);
        }
        else {
            message.gasWanted = 0;
        }
        if (object.gasUsed !== undefined && object.gasUsed !== null) {
            message.gasUsed = Number(object.gasUsed);
        }
        else {
            message.gasUsed = 0;
        }
        if (object.events !== undefined && object.events !== null) {
            for (const e of object.events) {
                message.events.push(Event.fromJSON(e));
            }
        }
        if (object.codespace !== undefined && object.codespace !== null) {
            message.codespace = String(object.codespace);
        }
        else {
            message.codespace = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.code !== undefined && (obj.code = message.code);
        message.data !== undefined &&
            (obj.data = base64FromBytes(message.data !== undefined ? message.data : new Uint8Array()));
        message.log !== undefined && (obj.log = message.log);
        message.info !== undefined && (obj.info = message.info);
        message.gasWanted !== undefined && (obj.gasWanted = message.gasWanted);
        message.gasUsed !== undefined && (obj.gasUsed = message.gasUsed);
        if (message.events) {
            obj.events = message.events.map((e) => (e ? Event.toJSON(e) : undefined));
        }
        else {
            obj.events = [];
        }
        message.codespace !== undefined && (obj.codespace = message.codespace);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseResponseDeliverTx };
        message.events = [];
        if (object.code !== undefined && object.code !== null) {
            message.code = object.code;
        }
        else {
            message.code = 0;
        }
        if (object.data !== undefined && object.data !== null) {
            message.data = object.data;
        }
        else {
            message.data = new Uint8Array();
        }
        if (object.log !== undefined && object.log !== null) {
            message.log = object.log;
        }
        else {
            message.log = "";
        }
        if (object.info !== undefined && object.info !== null) {
            message.info = object.info;
        }
        else {
            message.info = "";
        }
        if (object.gasWanted !== undefined && object.gasWanted !== null) {
            message.gasWanted = object.gasWanted;
        }
        else {
            message.gasWanted = 0;
        }
        if (object.gasUsed !== undefined && object.gasUsed !== null) {
            message.gasUsed = object.gasUsed;
        }
        else {
            message.gasUsed = 0;
        }
        if (object.events !== undefined && object.events !== null) {
            for (const e of object.events) {
                message.events.push(Event.fromPartial(e));
            }
        }
        if (object.codespace !== undefined && object.codespace !== null) {
            message.codespace = object.codespace;
        }
        else {
            message.codespace = "";
        }
        return message;
    },
};
const baseResponseEndBlock = {};
export const ResponseEndBlock = {
    encode(message, writer = Writer.create()) {
        for (const v of message.validatorUpdates) {
            ValidatorUpdate.encode(v, writer.uint32(10).fork()).ldelim();
        }
        if (message.consensusParamUpdates !== undefined) {
            ConsensusParams.encode(message.consensusParamUpdates, writer.uint32(18).fork()).ldelim();
        }
        for (const v of message.events) {
            Event.encode(v, writer.uint32(26).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseResponseEndBlock };
        message.validatorUpdates = [];
        message.events = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.validatorUpdates.push(ValidatorUpdate.decode(reader, reader.uint32()));
                    break;
                case 2:
                    message.consensusParamUpdates = ConsensusParams.decode(reader, reader.uint32());
                    break;
                case 3:
                    message.events.push(Event.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseResponseEndBlock };
        message.validatorUpdates = [];
        message.events = [];
        if (object.validatorUpdates !== undefined &&
            object.validatorUpdates !== null) {
            for (const e of object.validatorUpdates) {
                message.validatorUpdates.push(ValidatorUpdate.fromJSON(e));
            }
        }
        if (object.consensusParamUpdates !== undefined &&
            object.consensusParamUpdates !== null) {
            message.consensusParamUpdates = ConsensusParams.fromJSON(object.consensusParamUpdates);
        }
        else {
            message.consensusParamUpdates = undefined;
        }
        if (object.events !== undefined && object.events !== null) {
            for (const e of object.events) {
                message.events.push(Event.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.validatorUpdates) {
            obj.validatorUpdates = message.validatorUpdates.map((e) => e ? ValidatorUpdate.toJSON(e) : undefined);
        }
        else {
            obj.validatorUpdates = [];
        }
        message.consensusParamUpdates !== undefined &&
            (obj.consensusParamUpdates = message.consensusParamUpdates
                ? ConsensusParams.toJSON(message.consensusParamUpdates)
                : undefined);
        if (message.events) {
            obj.events = message.events.map((e) => (e ? Event.toJSON(e) : undefined));
        }
        else {
            obj.events = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseResponseEndBlock };
        message.validatorUpdates = [];
        message.events = [];
        if (object.validatorUpdates !== undefined &&
            object.validatorUpdates !== null) {
            for (const e of object.validatorUpdates) {
                message.validatorUpdates.push(ValidatorUpdate.fromPartial(e));
            }
        }
        if (object.consensusParamUpdates !== undefined &&
            object.consensusParamUpdates !== null) {
            message.consensusParamUpdates = ConsensusParams.fromPartial(object.consensusParamUpdates);
        }
        else {
            message.consensusParamUpdates = undefined;
        }
        if (object.events !== undefined && object.events !== null) {
            for (const e of object.events) {
                message.events.push(Event.fromPartial(e));
            }
        }
        return message;
    },
};
const baseResponseCommit = { retainHeight: 0 };
export const ResponseCommit = {
    encode(message, writer = Writer.create()) {
        if (message.data.length !== 0) {
            writer.uint32(18).bytes(message.data);
        }
        if (message.retainHeight !== 0) {
            writer.uint32(24).int64(message.retainHeight);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseResponseCommit };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 2:
                    message.data = reader.bytes();
                    break;
                case 3:
                    message.retainHeight = longToNumber(reader.int64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseResponseCommit };
        if (object.data !== undefined && object.data !== null) {
            message.data = bytesFromBase64(object.data);
        }
        if (object.retainHeight !== undefined && object.retainHeight !== null) {
            message.retainHeight = Number(object.retainHeight);
        }
        else {
            message.retainHeight = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.data !== undefined &&
            (obj.data = base64FromBytes(message.data !== undefined ? message.data : new Uint8Array()));
        message.retainHeight !== undefined &&
            (obj.retainHeight = message.retainHeight);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseResponseCommit };
        if (object.data !== undefined && object.data !== null) {
            message.data = object.data;
        }
        else {
            message.data = new Uint8Array();
        }
        if (object.retainHeight !== undefined && object.retainHeight !== null) {
            message.retainHeight = object.retainHeight;
        }
        else {
            message.retainHeight = 0;
        }
        return message;
    },
};
const baseResponseListSnapshots = {};
export const ResponseListSnapshots = {
    encode(message, writer = Writer.create()) {
        for (const v of message.snapshots) {
            Snapshot.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseResponseListSnapshots };
        message.snapshots = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.snapshots.push(Snapshot.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseResponseListSnapshots };
        message.snapshots = [];
        if (object.snapshots !== undefined && object.snapshots !== null) {
            for (const e of object.snapshots) {
                message.snapshots.push(Snapshot.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.snapshots) {
            obj.snapshots = message.snapshots.map((e) => e ? Snapshot.toJSON(e) : undefined);
        }
        else {
            obj.snapshots = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseResponseListSnapshots };
        message.snapshots = [];
        if (object.snapshots !== undefined && object.snapshots !== null) {
            for (const e of object.snapshots) {
                message.snapshots.push(Snapshot.fromPartial(e));
            }
        }
        return message;
    },
};
const baseResponseOfferSnapshot = { result: 0 };
export const ResponseOfferSnapshot = {
    encode(message, writer = Writer.create()) {
        if (message.result !== 0) {
            writer.uint32(8).int32(message.result);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseResponseOfferSnapshot };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.result = reader.int32();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseResponseOfferSnapshot };
        if (object.result !== undefined && object.result !== null) {
            message.result = responseOfferSnapshot_ResultFromJSON(object.result);
        }
        else {
            message.result = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.result !== undefined &&
            (obj.result = responseOfferSnapshot_ResultToJSON(message.result));
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseResponseOfferSnapshot };
        if (object.result !== undefined && object.result !== null) {
            message.result = object.result;
        }
        else {
            message.result = 0;
        }
        return message;
    },
};
const baseResponseLoadSnapshotChunk = {};
export const ResponseLoadSnapshotChunk = {
    encode(message, writer = Writer.create()) {
        if (message.chunk.length !== 0) {
            writer.uint32(10).bytes(message.chunk);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseResponseLoadSnapshotChunk,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.chunk = reader.bytes();
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
            ...baseResponseLoadSnapshotChunk,
        };
        if (object.chunk !== undefined && object.chunk !== null) {
            message.chunk = bytesFromBase64(object.chunk);
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.chunk !== undefined &&
            (obj.chunk = base64FromBytes(message.chunk !== undefined ? message.chunk : new Uint8Array()));
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseResponseLoadSnapshotChunk,
        };
        if (object.chunk !== undefined && object.chunk !== null) {
            message.chunk = object.chunk;
        }
        else {
            message.chunk = new Uint8Array();
        }
        return message;
    },
};
const baseResponseApplySnapshotChunk = {
    result: 0,
    refetchChunks: 0,
    rejectSenders: "",
};
export const ResponseApplySnapshotChunk = {
    encode(message, writer = Writer.create()) {
        if (message.result !== 0) {
            writer.uint32(8).int32(message.result);
        }
        writer.uint32(18).fork();
        for (const v of message.refetchChunks) {
            writer.uint32(v);
        }
        writer.ldelim();
        for (const v of message.rejectSenders) {
            writer.uint32(26).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseResponseApplySnapshotChunk,
        };
        message.refetchChunks = [];
        message.rejectSenders = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.result = reader.int32();
                    break;
                case 2:
                    if ((tag & 7) === 2) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.refetchChunks.push(reader.uint32());
                        }
                    }
                    else {
                        message.refetchChunks.push(reader.uint32());
                    }
                    break;
                case 3:
                    message.rejectSenders.push(reader.string());
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
            ...baseResponseApplySnapshotChunk,
        };
        message.refetchChunks = [];
        message.rejectSenders = [];
        if (object.result !== undefined && object.result !== null) {
            message.result = responseApplySnapshotChunk_ResultFromJSON(object.result);
        }
        else {
            message.result = 0;
        }
        if (object.refetchChunks !== undefined && object.refetchChunks !== null) {
            for (const e of object.refetchChunks) {
                message.refetchChunks.push(Number(e));
            }
        }
        if (object.rejectSenders !== undefined && object.rejectSenders !== null) {
            for (const e of object.rejectSenders) {
                message.rejectSenders.push(String(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.result !== undefined &&
            (obj.result = responseApplySnapshotChunk_ResultToJSON(message.result));
        if (message.refetchChunks) {
            obj.refetchChunks = message.refetchChunks.map((e) => e);
        }
        else {
            obj.refetchChunks = [];
        }
        if (message.rejectSenders) {
            obj.rejectSenders = message.rejectSenders.map((e) => e);
        }
        else {
            obj.rejectSenders = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseResponseApplySnapshotChunk,
        };
        message.refetchChunks = [];
        message.rejectSenders = [];
        if (object.result !== undefined && object.result !== null) {
            message.result = object.result;
        }
        else {
            message.result = 0;
        }
        if (object.refetchChunks !== undefined && object.refetchChunks !== null) {
            for (const e of object.refetchChunks) {
                message.refetchChunks.push(e);
            }
        }
        if (object.rejectSenders !== undefined && object.rejectSenders !== null) {
            for (const e of object.rejectSenders) {
                message.rejectSenders.push(e);
            }
        }
        return message;
    },
};
const baseConsensusParams = {};
export const ConsensusParams = {
    encode(message, writer = Writer.create()) {
        if (message.block !== undefined) {
            BlockParams.encode(message.block, writer.uint32(10).fork()).ldelim();
        }
        if (message.evidence !== undefined) {
            EvidenceParams.encode(message.evidence, writer.uint32(18).fork()).ldelim();
        }
        if (message.validator !== undefined) {
            ValidatorParams.encode(message.validator, writer.uint32(26).fork()).ldelim();
        }
        if (message.version !== undefined) {
            VersionParams.encode(message.version, writer.uint32(34).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseConsensusParams };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.block = BlockParams.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.evidence = EvidenceParams.decode(reader, reader.uint32());
                    break;
                case 3:
                    message.validator = ValidatorParams.decode(reader, reader.uint32());
                    break;
                case 4:
                    message.version = VersionParams.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseConsensusParams };
        if (object.block !== undefined && object.block !== null) {
            message.block = BlockParams.fromJSON(object.block);
        }
        else {
            message.block = undefined;
        }
        if (object.evidence !== undefined && object.evidence !== null) {
            message.evidence = EvidenceParams.fromJSON(object.evidence);
        }
        else {
            message.evidence = undefined;
        }
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = ValidatorParams.fromJSON(object.validator);
        }
        else {
            message.validator = undefined;
        }
        if (object.version !== undefined && object.version !== null) {
            message.version = VersionParams.fromJSON(object.version);
        }
        else {
            message.version = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.block !== undefined &&
            (obj.block = message.block
                ? BlockParams.toJSON(message.block)
                : undefined);
        message.evidence !== undefined &&
            (obj.evidence = message.evidence
                ? EvidenceParams.toJSON(message.evidence)
                : undefined);
        message.validator !== undefined &&
            (obj.validator = message.validator
                ? ValidatorParams.toJSON(message.validator)
                : undefined);
        message.version !== undefined &&
            (obj.version = message.version
                ? VersionParams.toJSON(message.version)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseConsensusParams };
        if (object.block !== undefined && object.block !== null) {
            message.block = BlockParams.fromPartial(object.block);
        }
        else {
            message.block = undefined;
        }
        if (object.evidence !== undefined && object.evidence !== null) {
            message.evidence = EvidenceParams.fromPartial(object.evidence);
        }
        else {
            message.evidence = undefined;
        }
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = ValidatorParams.fromPartial(object.validator);
        }
        else {
            message.validator = undefined;
        }
        if (object.version !== undefined && object.version !== null) {
            message.version = VersionParams.fromPartial(object.version);
        }
        else {
            message.version = undefined;
        }
        return message;
    },
};
const baseBlockParams = { maxBytes: 0, maxGas: 0 };
export const BlockParams = {
    encode(message, writer = Writer.create()) {
        if (message.maxBytes !== 0) {
            writer.uint32(8).int64(message.maxBytes);
        }
        if (message.maxGas !== 0) {
            writer.uint32(16).int64(message.maxGas);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseBlockParams };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.maxBytes = longToNumber(reader.int64());
                    break;
                case 2:
                    message.maxGas = longToNumber(reader.int64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseBlockParams };
        if (object.maxBytes !== undefined && object.maxBytes !== null) {
            message.maxBytes = Number(object.maxBytes);
        }
        else {
            message.maxBytes = 0;
        }
        if (object.maxGas !== undefined && object.maxGas !== null) {
            message.maxGas = Number(object.maxGas);
        }
        else {
            message.maxGas = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.maxBytes !== undefined && (obj.maxBytes = message.maxBytes);
        message.maxGas !== undefined && (obj.maxGas = message.maxGas);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseBlockParams };
        if (object.maxBytes !== undefined && object.maxBytes !== null) {
            message.maxBytes = object.maxBytes;
        }
        else {
            message.maxBytes = 0;
        }
        if (object.maxGas !== undefined && object.maxGas !== null) {
            message.maxGas = object.maxGas;
        }
        else {
            message.maxGas = 0;
        }
        return message;
    },
};
const baseLastCommitInfo = { round: 0 };
export const LastCommitInfo = {
    encode(message, writer = Writer.create()) {
        if (message.round !== 0) {
            writer.uint32(8).int32(message.round);
        }
        for (const v of message.votes) {
            VoteInfo.encode(v, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseLastCommitInfo };
        message.votes = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.round = reader.int32();
                    break;
                case 2:
                    message.votes.push(VoteInfo.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseLastCommitInfo };
        message.votes = [];
        if (object.round !== undefined && object.round !== null) {
            message.round = Number(object.round);
        }
        else {
            message.round = 0;
        }
        if (object.votes !== undefined && object.votes !== null) {
            for (const e of object.votes) {
                message.votes.push(VoteInfo.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.round !== undefined && (obj.round = message.round);
        if (message.votes) {
            obj.votes = message.votes.map((e) => e ? VoteInfo.toJSON(e) : undefined);
        }
        else {
            obj.votes = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseLastCommitInfo };
        message.votes = [];
        if (object.round !== undefined && object.round !== null) {
            message.round = object.round;
        }
        else {
            message.round = 0;
        }
        if (object.votes !== undefined && object.votes !== null) {
            for (const e of object.votes) {
                message.votes.push(VoteInfo.fromPartial(e));
            }
        }
        return message;
    },
};
const baseEvent = { type: "" };
export const Event = {
    encode(message, writer = Writer.create()) {
        if (message.type !== "") {
            writer.uint32(10).string(message.type);
        }
        for (const v of message.attributes) {
            EventAttribute.encode(v, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseEvent };
        message.attributes = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.type = reader.string();
                    break;
                case 2:
                    message.attributes.push(EventAttribute.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseEvent };
        message.attributes = [];
        if (object.type !== undefined && object.type !== null) {
            message.type = String(object.type);
        }
        else {
            message.type = "";
        }
        if (object.attributes !== undefined && object.attributes !== null) {
            for (const e of object.attributes) {
                message.attributes.push(EventAttribute.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.type !== undefined && (obj.type = message.type);
        if (message.attributes) {
            obj.attributes = message.attributes.map((e) => e ? EventAttribute.toJSON(e) : undefined);
        }
        else {
            obj.attributes = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseEvent };
        message.attributes = [];
        if (object.type !== undefined && object.type !== null) {
            message.type = object.type;
        }
        else {
            message.type = "";
        }
        if (object.attributes !== undefined && object.attributes !== null) {
            for (const e of object.attributes) {
                message.attributes.push(EventAttribute.fromPartial(e));
            }
        }
        return message;
    },
};
const baseEventAttribute = { index: false };
export const EventAttribute = {
    encode(message, writer = Writer.create()) {
        if (message.key.length !== 0) {
            writer.uint32(10).bytes(message.key);
        }
        if (message.value.length !== 0) {
            writer.uint32(18).bytes(message.value);
        }
        if (message.index === true) {
            writer.uint32(24).bool(message.index);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseEventAttribute };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.key = reader.bytes();
                    break;
                case 2:
                    message.value = reader.bytes();
                    break;
                case 3:
                    message.index = reader.bool();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseEventAttribute };
        if (object.key !== undefined && object.key !== null) {
            message.key = bytesFromBase64(object.key);
        }
        if (object.value !== undefined && object.value !== null) {
            message.value = bytesFromBase64(object.value);
        }
        if (object.index !== undefined && object.index !== null) {
            message.index = Boolean(object.index);
        }
        else {
            message.index = false;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.key !== undefined &&
            (obj.key = base64FromBytes(message.key !== undefined ? message.key : new Uint8Array()));
        message.value !== undefined &&
            (obj.value = base64FromBytes(message.value !== undefined ? message.value : new Uint8Array()));
        message.index !== undefined && (obj.index = message.index);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseEventAttribute };
        if (object.key !== undefined && object.key !== null) {
            message.key = object.key;
        }
        else {
            message.key = new Uint8Array();
        }
        if (object.value !== undefined && object.value !== null) {
            message.value = object.value;
        }
        else {
            message.value = new Uint8Array();
        }
        if (object.index !== undefined && object.index !== null) {
            message.index = object.index;
        }
        else {
            message.index = false;
        }
        return message;
    },
};
const baseTxResult = { height: 0, index: 0 };
export const TxResult = {
    encode(message, writer = Writer.create()) {
        if (message.height !== 0) {
            writer.uint32(8).int64(message.height);
        }
        if (message.index !== 0) {
            writer.uint32(16).uint32(message.index);
        }
        if (message.tx.length !== 0) {
            writer.uint32(26).bytes(message.tx);
        }
        if (message.result !== undefined) {
            ResponseDeliverTx.encode(message.result, writer.uint32(34).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseTxResult };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.height = longToNumber(reader.int64());
                    break;
                case 2:
                    message.index = reader.uint32();
                    break;
                case 3:
                    message.tx = reader.bytes();
                    break;
                case 4:
                    message.result = ResponseDeliverTx.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseTxResult };
        if (object.height !== undefined && object.height !== null) {
            message.height = Number(object.height);
        }
        else {
            message.height = 0;
        }
        if (object.index !== undefined && object.index !== null) {
            message.index = Number(object.index);
        }
        else {
            message.index = 0;
        }
        if (object.tx !== undefined && object.tx !== null) {
            message.tx = bytesFromBase64(object.tx);
        }
        if (object.result !== undefined && object.result !== null) {
            message.result = ResponseDeliverTx.fromJSON(object.result);
        }
        else {
            message.result = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.height !== undefined && (obj.height = message.height);
        message.index !== undefined && (obj.index = message.index);
        message.tx !== undefined &&
            (obj.tx = base64FromBytes(message.tx !== undefined ? message.tx : new Uint8Array()));
        message.result !== undefined &&
            (obj.result = message.result
                ? ResponseDeliverTx.toJSON(message.result)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseTxResult };
        if (object.height !== undefined && object.height !== null) {
            message.height = object.height;
        }
        else {
            message.height = 0;
        }
        if (object.index !== undefined && object.index !== null) {
            message.index = object.index;
        }
        else {
            message.index = 0;
        }
        if (object.tx !== undefined && object.tx !== null) {
            message.tx = object.tx;
        }
        else {
            message.tx = new Uint8Array();
        }
        if (object.result !== undefined && object.result !== null) {
            message.result = ResponseDeliverTx.fromPartial(object.result);
        }
        else {
            message.result = undefined;
        }
        return message;
    },
};
const baseValidator = { power: 0 };
export const Validator = {
    encode(message, writer = Writer.create()) {
        if (message.address.length !== 0) {
            writer.uint32(10).bytes(message.address);
        }
        if (message.power !== 0) {
            writer.uint32(24).int64(message.power);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseValidator };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.address = reader.bytes();
                    break;
                case 3:
                    message.power = longToNumber(reader.int64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseValidator };
        if (object.address !== undefined && object.address !== null) {
            message.address = bytesFromBase64(object.address);
        }
        if (object.power !== undefined && object.power !== null) {
            message.power = Number(object.power);
        }
        else {
            message.power = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.address !== undefined &&
            (obj.address = base64FromBytes(message.address !== undefined ? message.address : new Uint8Array()));
        message.power !== undefined && (obj.power = message.power);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseValidator };
        if (object.address !== undefined && object.address !== null) {
            message.address = object.address;
        }
        else {
            message.address = new Uint8Array();
        }
        if (object.power !== undefined && object.power !== null) {
            message.power = object.power;
        }
        else {
            message.power = 0;
        }
        return message;
    },
};
const baseValidatorUpdate = { power: 0 };
export const ValidatorUpdate = {
    encode(message, writer = Writer.create()) {
        if (message.pubKey !== undefined) {
            PublicKey.encode(message.pubKey, writer.uint32(10).fork()).ldelim();
        }
        if (message.power !== 0) {
            writer.uint32(16).int64(message.power);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseValidatorUpdate };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.pubKey = PublicKey.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.power = longToNumber(reader.int64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseValidatorUpdate };
        if (object.pubKey !== undefined && object.pubKey !== null) {
            message.pubKey = PublicKey.fromJSON(object.pubKey);
        }
        else {
            message.pubKey = undefined;
        }
        if (object.power !== undefined && object.power !== null) {
            message.power = Number(object.power);
        }
        else {
            message.power = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.pubKey !== undefined &&
            (obj.pubKey = message.pubKey
                ? PublicKey.toJSON(message.pubKey)
                : undefined);
        message.power !== undefined && (obj.power = message.power);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseValidatorUpdate };
        if (object.pubKey !== undefined && object.pubKey !== null) {
            message.pubKey = PublicKey.fromPartial(object.pubKey);
        }
        else {
            message.pubKey = undefined;
        }
        if (object.power !== undefined && object.power !== null) {
            message.power = object.power;
        }
        else {
            message.power = 0;
        }
        return message;
    },
};
const baseVoteInfo = { signedLastBlock: false };
export const VoteInfo = {
    encode(message, writer = Writer.create()) {
        if (message.validator !== undefined) {
            Validator.encode(message.validator, writer.uint32(10).fork()).ldelim();
        }
        if (message.signedLastBlock === true) {
            writer.uint32(16).bool(message.signedLastBlock);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseVoteInfo };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.validator = Validator.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.signedLastBlock = reader.bool();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseVoteInfo };
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = Validator.fromJSON(object.validator);
        }
        else {
            message.validator = undefined;
        }
        if (object.signedLastBlock !== undefined &&
            object.signedLastBlock !== null) {
            message.signedLastBlock = Boolean(object.signedLastBlock);
        }
        else {
            message.signedLastBlock = false;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.validator !== undefined &&
            (obj.validator = message.validator
                ? Validator.toJSON(message.validator)
                : undefined);
        message.signedLastBlock !== undefined &&
            (obj.signedLastBlock = message.signedLastBlock);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseVoteInfo };
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = Validator.fromPartial(object.validator);
        }
        else {
            message.validator = undefined;
        }
        if (object.signedLastBlock !== undefined &&
            object.signedLastBlock !== null) {
            message.signedLastBlock = object.signedLastBlock;
        }
        else {
            message.signedLastBlock = false;
        }
        return message;
    },
};
const baseEvidence = { type: 0, height: 0, totalVotingPower: 0 };
export const Evidence = {
    encode(message, writer = Writer.create()) {
        if (message.type !== 0) {
            writer.uint32(8).int32(message.type);
        }
        if (message.validator !== undefined) {
            Validator.encode(message.validator, writer.uint32(18).fork()).ldelim();
        }
        if (message.height !== 0) {
            writer.uint32(24).int64(message.height);
        }
        if (message.time !== undefined) {
            Timestamp.encode(toTimestamp(message.time), writer.uint32(34).fork()).ldelim();
        }
        if (message.totalVotingPower !== 0) {
            writer.uint32(40).int64(message.totalVotingPower);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseEvidence };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.type = reader.int32();
                    break;
                case 2:
                    message.validator = Validator.decode(reader, reader.uint32());
                    break;
                case 3:
                    message.height = longToNumber(reader.int64());
                    break;
                case 4:
                    message.time = fromTimestamp(Timestamp.decode(reader, reader.uint32()));
                    break;
                case 5:
                    message.totalVotingPower = longToNumber(reader.int64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseEvidence };
        if (object.type !== undefined && object.type !== null) {
            message.type = evidenceTypeFromJSON(object.type);
        }
        else {
            message.type = 0;
        }
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = Validator.fromJSON(object.validator);
        }
        else {
            message.validator = undefined;
        }
        if (object.height !== undefined && object.height !== null) {
            message.height = Number(object.height);
        }
        else {
            message.height = 0;
        }
        if (object.time !== undefined && object.time !== null) {
            message.time = fromJsonTimestamp(object.time);
        }
        else {
            message.time = undefined;
        }
        if (object.totalVotingPower !== undefined &&
            object.totalVotingPower !== null) {
            message.totalVotingPower = Number(object.totalVotingPower);
        }
        else {
            message.totalVotingPower = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.type !== undefined && (obj.type = evidenceTypeToJSON(message.type));
        message.validator !== undefined &&
            (obj.validator = message.validator
                ? Validator.toJSON(message.validator)
                : undefined);
        message.height !== undefined && (obj.height = message.height);
        message.time !== undefined &&
            (obj.time =
                message.time !== undefined ? message.time.toISOString() : null);
        message.totalVotingPower !== undefined &&
            (obj.totalVotingPower = message.totalVotingPower);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseEvidence };
        if (object.type !== undefined && object.type !== null) {
            message.type = object.type;
        }
        else {
            message.type = 0;
        }
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = Validator.fromPartial(object.validator);
        }
        else {
            message.validator = undefined;
        }
        if (object.height !== undefined && object.height !== null) {
            message.height = object.height;
        }
        else {
            message.height = 0;
        }
        if (object.time !== undefined && object.time !== null) {
            message.time = object.time;
        }
        else {
            message.time = undefined;
        }
        if (object.totalVotingPower !== undefined &&
            object.totalVotingPower !== null) {
            message.totalVotingPower = object.totalVotingPower;
        }
        else {
            message.totalVotingPower = 0;
        }
        return message;
    },
};
const baseSnapshot = { height: 0, format: 0, chunks: 0 };
export const Snapshot = {
    encode(message, writer = Writer.create()) {
        if (message.height !== 0) {
            writer.uint32(8).uint64(message.height);
        }
        if (message.format !== 0) {
            writer.uint32(16).uint32(message.format);
        }
        if (message.chunks !== 0) {
            writer.uint32(24).uint32(message.chunks);
        }
        if (message.hash.length !== 0) {
            writer.uint32(34).bytes(message.hash);
        }
        if (message.metadata.length !== 0) {
            writer.uint32(42).bytes(message.metadata);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseSnapshot };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.height = longToNumber(reader.uint64());
                    break;
                case 2:
                    message.format = reader.uint32();
                    break;
                case 3:
                    message.chunks = reader.uint32();
                    break;
                case 4:
                    message.hash = reader.bytes();
                    break;
                case 5:
                    message.metadata = reader.bytes();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseSnapshot };
        if (object.height !== undefined && object.height !== null) {
            message.height = Number(object.height);
        }
        else {
            message.height = 0;
        }
        if (object.format !== undefined && object.format !== null) {
            message.format = Number(object.format);
        }
        else {
            message.format = 0;
        }
        if (object.chunks !== undefined && object.chunks !== null) {
            message.chunks = Number(object.chunks);
        }
        else {
            message.chunks = 0;
        }
        if (object.hash !== undefined && object.hash !== null) {
            message.hash = bytesFromBase64(object.hash);
        }
        if (object.metadata !== undefined && object.metadata !== null) {
            message.metadata = bytesFromBase64(object.metadata);
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.height !== undefined && (obj.height = message.height);
        message.format !== undefined && (obj.format = message.format);
        message.chunks !== undefined && (obj.chunks = message.chunks);
        message.hash !== undefined &&
            (obj.hash = base64FromBytes(message.hash !== undefined ? message.hash : new Uint8Array()));
        message.metadata !== undefined &&
            (obj.metadata = base64FromBytes(message.metadata !== undefined ? message.metadata : new Uint8Array()));
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseSnapshot };
        if (object.height !== undefined && object.height !== null) {
            message.height = object.height;
        }
        else {
            message.height = 0;
        }
        if (object.format !== undefined && object.format !== null) {
            message.format = object.format;
        }
        else {
            message.format = 0;
        }
        if (object.chunks !== undefined && object.chunks !== null) {
            message.chunks = object.chunks;
        }
        else {
            message.chunks = 0;
        }
        if (object.hash !== undefined && object.hash !== null) {
            message.hash = object.hash;
        }
        else {
            message.hash = new Uint8Array();
        }
        if (object.metadata !== undefined && object.metadata !== null) {
            message.metadata = object.metadata;
        }
        else {
            message.metadata = new Uint8Array();
        }
        return message;
    },
};
export class ABCIApplicationClientImpl {
    constructor(rpc) {
        this.rpc = rpc;
    }
    Echo(request) {
        const data = RequestEcho.encode(request).finish();
        const promise = this.rpc.request("tendermint.abci.ABCIApplication", "Echo", data);
        return promise.then((data) => ResponseEcho.decode(new Reader(data)));
    }
    Flush(request) {
        const data = RequestFlush.encode(request).finish();
        const promise = this.rpc.request("tendermint.abci.ABCIApplication", "Flush", data);
        return promise.then((data) => ResponseFlush.decode(new Reader(data)));
    }
    Info(request) {
        const data = RequestInfo.encode(request).finish();
        const promise = this.rpc.request("tendermint.abci.ABCIApplication", "Info", data);
        return promise.then((data) => ResponseInfo.decode(new Reader(data)));
    }
    SetOption(request) {
        const data = RequestSetOption.encode(request).finish();
        const promise = this.rpc.request("tendermint.abci.ABCIApplication", "SetOption", data);
        return promise.then((data) => ResponseSetOption.decode(new Reader(data)));
    }
    DeliverTx(request) {
        const data = RequestDeliverTx.encode(request).finish();
        const promise = this.rpc.request("tendermint.abci.ABCIApplication", "DeliverTx", data);
        return promise.then((data) => ResponseDeliverTx.decode(new Reader(data)));
    }
    CheckTx(request) {
        const data = RequestCheckTx.encode(request).finish();
        const promise = this.rpc.request("tendermint.abci.ABCIApplication", "CheckTx", data);
        return promise.then((data) => ResponseCheckTx.decode(new Reader(data)));
    }
    Query(request) {
        const data = RequestQuery.encode(request).finish();
        const promise = this.rpc.request("tendermint.abci.ABCIApplication", "Query", data);
        return promise.then((data) => ResponseQuery.decode(new Reader(data)));
    }
    Commit(request) {
        const data = RequestCommit.encode(request).finish();
        const promise = this.rpc.request("tendermint.abci.ABCIApplication", "Commit", data);
        return promise.then((data) => ResponseCommit.decode(new Reader(data)));
    }
    InitChain(request) {
        const data = RequestInitChain.encode(request).finish();
        const promise = this.rpc.request("tendermint.abci.ABCIApplication", "InitChain", data);
        return promise.then((data) => ResponseInitChain.decode(new Reader(data)));
    }
    BeginBlock(request) {
        const data = RequestBeginBlock.encode(request).finish();
        const promise = this.rpc.request("tendermint.abci.ABCIApplication", "BeginBlock", data);
        return promise.then((data) => ResponseBeginBlock.decode(new Reader(data)));
    }
    EndBlock(request) {
        const data = RequestEndBlock.encode(request).finish();
        const promise = this.rpc.request("tendermint.abci.ABCIApplication", "EndBlock", data);
        return promise.then((data) => ResponseEndBlock.decode(new Reader(data)));
    }
    ListSnapshots(request) {
        const data = RequestListSnapshots.encode(request).finish();
        const promise = this.rpc.request("tendermint.abci.ABCIApplication", "ListSnapshots", data);
        return promise.then((data) => ResponseListSnapshots.decode(new Reader(data)));
    }
    OfferSnapshot(request) {
        const data = RequestOfferSnapshot.encode(request).finish();
        const promise = this.rpc.request("tendermint.abci.ABCIApplication", "OfferSnapshot", data);
        return promise.then((data) => ResponseOfferSnapshot.decode(new Reader(data)));
    }
    LoadSnapshotChunk(request) {
        const data = RequestLoadSnapshotChunk.encode(request).finish();
        const promise = this.rpc.request("tendermint.abci.ABCIApplication", "LoadSnapshotChunk", data);
        return promise.then((data) => ResponseLoadSnapshotChunk.decode(new Reader(data)));
    }
    ApplySnapshotChunk(request) {
        const data = RequestApplySnapshotChunk.encode(request).finish();
        const promise = this.rpc.request("tendermint.abci.ABCIApplication", "ApplySnapshotChunk", data);
        return promise.then((data) => ResponseApplySnapshotChunk.decode(new Reader(data)));
    }
}
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
const atob = globalThis.atob ||
    ((b64) => globalThis.Buffer.from(b64, "base64").toString("binary"));
function bytesFromBase64(b64) {
    const bin = atob(b64);
    const arr = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; ++i) {
        arr[i] = bin.charCodeAt(i);
    }
    return arr;
}
const btoa = globalThis.btoa ||
    ((bin) => globalThis.Buffer.from(bin, "binary").toString("base64"));
function base64FromBytes(arr) {
    const bin = [];
    for (let i = 0; i < arr.byteLength; ++i) {
        bin.push(String.fromCharCode(arr[i]));
    }
    return btoa(bin.join(""));
}
function toTimestamp(date) {
    const seconds = date.getTime() / 1000;
    const nanos = (date.getTime() % 1000) * 1000000;
    return { seconds, nanos };
}
function fromTimestamp(t) {
    let millis = t.seconds * 1000;
    millis += t.nanos / 1000000;
    return new Date(millis);
}
function fromJsonTimestamp(o) {
    if (o instanceof Date) {
        return o;
    }
    else if (typeof o === "string") {
        return new Date(o);
    }
    else {
        return fromTimestamp(Timestamp.fromJSON(o));
    }
}
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
