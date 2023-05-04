// THIS FILE IS GENERATED AUTOMATICALLY. DO NOT MODIFY.
import { SigningStargateClient } from "@cosmjs/stargate";
import { Registry } from "@cosmjs/proto-signing";
import { Api } from "./rest";
import { MsgAggregateExchangeRateVote } from "./types/oracle/tx";
import { MsgAggregateExchangeRatePrevote } from "./types/oracle/tx";
import { MsgAggregateExchangeRateCombinedVote } from "./types/oracle/tx";
import { MsgDelegateFeedConsent } from "./types/oracle/tx";
const types = [
    ["/seiprotocol.seichain.oracle.MsgAggregateExchangeRateVote", MsgAggregateExchangeRateVote],
    ["/seiprotocol.seichain.oracle.MsgAggregateExchangeRatePrevote", MsgAggregateExchangeRatePrevote],
    ["/seiprotocol.seichain.oracle.MsgAggregateExchangeRateCombinedVote", MsgAggregateExchangeRateCombinedVote],
    ["/seiprotocol.seichain.oracle.MsgDelegateFeedConsent", MsgDelegateFeedConsent],
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
        msgAggregateExchangeRateVote: (data) => ({ typeUrl: "/seiprotocol.seichain.oracle.MsgAggregateExchangeRateVote", value: MsgAggregateExchangeRateVote.fromPartial(data) }),
        msgAggregateExchangeRatePrevote: (data) => ({ typeUrl: "/seiprotocol.seichain.oracle.MsgAggregateExchangeRatePrevote", value: MsgAggregateExchangeRatePrevote.fromPartial(data) }),
        msgAggregateExchangeRateCombinedVote: (data) => ({ typeUrl: "/seiprotocol.seichain.oracle.MsgAggregateExchangeRateCombinedVote", value: MsgAggregateExchangeRateCombinedVote.fromPartial(data) }),
        msgDelegateFeedConsent: (data) => ({ typeUrl: "/seiprotocol.seichain.oracle.MsgDelegateFeedConsent", value: MsgDelegateFeedConsent.fromPartial(data) }),
    };
};
const queryClient = async ({ addr: addr } = { addr: "http://localhost:1317" }) => {
    return new Api({ baseUrl: addr });
};
export { txClient, queryClient, };
