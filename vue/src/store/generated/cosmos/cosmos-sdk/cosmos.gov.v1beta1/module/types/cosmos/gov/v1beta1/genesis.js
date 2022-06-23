/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Deposit, Vote, Proposal, DepositParams, VotingParams, TallyParams, } from "../../../cosmos/gov/v1beta1/gov";
export const protobufPackage = "cosmos.gov.v1beta1";
const baseGenesisState = { startingProposalId: 0 };
export const GenesisState = {
    encode(message, writer = Writer.create()) {
        if (message.startingProposalId !== 0) {
            writer.uint32(8).uint64(message.startingProposalId);
        }
        for (const v of message.deposits) {
            Deposit.encode(v, writer.uint32(18).fork()).ldelim();
        }
        for (const v of message.votes) {
            Vote.encode(v, writer.uint32(26).fork()).ldelim();
        }
        for (const v of message.proposals) {
            Proposal.encode(v, writer.uint32(34).fork()).ldelim();
        }
        if (message.depositParams !== undefined) {
            DepositParams.encode(message.depositParams, writer.uint32(42).fork()).ldelim();
        }
        if (message.votingParams !== undefined) {
            VotingParams.encode(message.votingParams, writer.uint32(50).fork()).ldelim();
        }
        if (message.tallyParams !== undefined) {
            TallyParams.encode(message.tallyParams, writer.uint32(58).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseGenesisState };
        message.deposits = [];
        message.votes = [];
        message.proposals = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.startingProposalId = longToNumber(reader.uint64());
                    break;
                case 2:
                    message.deposits.push(Deposit.decode(reader, reader.uint32()));
                    break;
                case 3:
                    message.votes.push(Vote.decode(reader, reader.uint32()));
                    break;
                case 4:
                    message.proposals.push(Proposal.decode(reader, reader.uint32()));
                    break;
                case 5:
                    message.depositParams = DepositParams.decode(reader, reader.uint32());
                    break;
                case 6:
                    message.votingParams = VotingParams.decode(reader, reader.uint32());
                    break;
                case 7:
                    message.tallyParams = TallyParams.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseGenesisState };
        message.deposits = [];
        message.votes = [];
        message.proposals = [];
        if (object.startingProposalId !== undefined &&
            object.startingProposalId !== null) {
            message.startingProposalId = Number(object.startingProposalId);
        }
        else {
            message.startingProposalId = 0;
        }
        if (object.deposits !== undefined && object.deposits !== null) {
            for (const e of object.deposits) {
                message.deposits.push(Deposit.fromJSON(e));
            }
        }
        if (object.votes !== undefined && object.votes !== null) {
            for (const e of object.votes) {
                message.votes.push(Vote.fromJSON(e));
            }
        }
        if (object.proposals !== undefined && object.proposals !== null) {
            for (const e of object.proposals) {
                message.proposals.push(Proposal.fromJSON(e));
            }
        }
        if (object.depositParams !== undefined && object.depositParams !== null) {
            message.depositParams = DepositParams.fromJSON(object.depositParams);
        }
        else {
            message.depositParams = undefined;
        }
        if (object.votingParams !== undefined && object.votingParams !== null) {
            message.votingParams = VotingParams.fromJSON(object.votingParams);
        }
        else {
            message.votingParams = undefined;
        }
        if (object.tallyParams !== undefined && object.tallyParams !== null) {
            message.tallyParams = TallyParams.fromJSON(object.tallyParams);
        }
        else {
            message.tallyParams = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.startingProposalId !== undefined &&
            (obj.startingProposalId = message.startingProposalId);
        if (message.deposits) {
            obj.deposits = message.deposits.map((e) => e ? Deposit.toJSON(e) : undefined);
        }
        else {
            obj.deposits = [];
        }
        if (message.votes) {
            obj.votes = message.votes.map((e) => (e ? Vote.toJSON(e) : undefined));
        }
        else {
            obj.votes = [];
        }
        if (message.proposals) {
            obj.proposals = message.proposals.map((e) => e ? Proposal.toJSON(e) : undefined);
        }
        else {
            obj.proposals = [];
        }
        message.depositParams !== undefined &&
            (obj.depositParams = message.depositParams
                ? DepositParams.toJSON(message.depositParams)
                : undefined);
        message.votingParams !== undefined &&
            (obj.votingParams = message.votingParams
                ? VotingParams.toJSON(message.votingParams)
                : undefined);
        message.tallyParams !== undefined &&
            (obj.tallyParams = message.tallyParams
                ? TallyParams.toJSON(message.tallyParams)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseGenesisState };
        message.deposits = [];
        message.votes = [];
        message.proposals = [];
        if (object.startingProposalId !== undefined &&
            object.startingProposalId !== null) {
            message.startingProposalId = object.startingProposalId;
        }
        else {
            message.startingProposalId = 0;
        }
        if (object.deposits !== undefined && object.deposits !== null) {
            for (const e of object.deposits) {
                message.deposits.push(Deposit.fromPartial(e));
            }
        }
        if (object.votes !== undefined && object.votes !== null) {
            for (const e of object.votes) {
                message.votes.push(Vote.fromPartial(e));
            }
        }
        if (object.proposals !== undefined && object.proposals !== null) {
            for (const e of object.proposals) {
                message.proposals.push(Proposal.fromPartial(e));
            }
        }
        if (object.depositParams !== undefined && object.depositParams !== null) {
            message.depositParams = DepositParams.fromPartial(object.depositParams);
        }
        else {
            message.depositParams = undefined;
        }
        if (object.votingParams !== undefined && object.votingParams !== null) {
            message.votingParams = VotingParams.fromPartial(object.votingParams);
        }
        else {
            message.votingParams = undefined;
        }
        if (object.tallyParams !== undefined && object.tallyParams !== null) {
            message.tallyParams = TallyParams.fromPartial(object.tallyParams);
        }
        else {
            message.tallyParams = undefined;
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
