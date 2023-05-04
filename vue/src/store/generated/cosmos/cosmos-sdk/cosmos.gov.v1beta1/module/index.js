// THIS FILE IS GENERATED AUTOMATICALLY. DO NOT MODIFY.
import { SigningStargateClient } from "@cosmjs/stargate";
import { Registry } from "@cosmjs/proto-signing";
import { Api } from "./rest";
import { MsgVote } from "./types/cosmos/gov/v1beta1/tx";
import { MsgDeposit } from "./types/cosmos/gov/v1beta1/tx";
import { MsgSubmitProposal } from "./types/cosmos/gov/v1beta1/tx";
import { MsgVoteWeighted } from "./types/cosmos/gov/v1beta1/tx";
const types = [
    ["/cosmos.gov.v1beta1.MsgVote", MsgVote],
    ["/cosmos.gov.v1beta1.MsgDeposit", MsgDeposit],
    ["/cosmos.gov.v1beta1.MsgSubmitProposal", MsgSubmitProposal],
    ["/cosmos.gov.v1beta1.MsgVoteWeighted", MsgVoteWeighted],
];
export const MissingWalletError = new Error("wallet is required");
export const registry = new Registry(types);
const defaultFee = {
    amount: [],
    gas: "200000",
};
const txClient = async (wallet, { addr: addr } = { addr: "http://localhost:26657" }) => {
    if (!wallet)
        throw MissingWalletError;
    let client;
    if (addr) {
        client = await SigningStargateClient.connectWithSigner(addr, wallet, { registry });
    }
    else {
        client = await SigningStargateClient.offline(wallet, { registry });
    }
    const { address } = (await wallet.getAccounts())[0];
    return {
        signAndBroadcast: (msgs, { fee, memo } = { fee: defaultFee, memo: "" }) => client.signAndBroadcast(address, msgs, fee, memo),
        msgVote: (data) => ({ typeUrl: "/cosmos.gov.v1beta1.MsgVote", value: MsgVote.fromPartial(data) }),
        msgDeposit: (data) => ({ typeUrl: "/cosmos.gov.v1beta1.MsgDeposit", value: MsgDeposit.fromPartial(data) }),
        msgSubmitProposal: (data) => ({ typeUrl: "/cosmos.gov.v1beta1.MsgSubmitProposal", value: MsgSubmitProposal.fromPartial(data) }),
        msgVoteWeighted: (data) => ({ typeUrl: "/cosmos.gov.v1beta1.MsgVoteWeighted", value: MsgVoteWeighted.fromPartial(data) }),
    };
};
const queryClient = async ({ addr: addr } = { addr: "http://localhost:1317" }) => {
    return new Api({ baseUrl: addr });
};
export { txClient, queryClient, };
