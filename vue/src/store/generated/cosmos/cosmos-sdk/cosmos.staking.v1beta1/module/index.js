// THIS FILE IS GENERATED AUTOMATICALLY. DO NOT MODIFY.
import { SigningStargateClient } from "@cosmjs/stargate";
import { Registry } from "@cosmjs/proto-signing";
import { Api } from "./rest";
import { MsgUndelegate } from "./types/cosmos/staking/v1beta1/tx";
import { MsgEditValidator } from "./types/cosmos/staking/v1beta1/tx";
import { MsgCreateValidator } from "./types/cosmos/staking/v1beta1/tx";
import { MsgDelegate } from "./types/cosmos/staking/v1beta1/tx";
import { MsgBeginRedelegate } from "./types/cosmos/staking/v1beta1/tx";
const types = [
    ["/cosmos.staking.v1beta1.MsgUndelegate", MsgUndelegate],
    ["/cosmos.staking.v1beta1.MsgEditValidator", MsgEditValidator],
    ["/cosmos.staking.v1beta1.MsgCreateValidator", MsgCreateValidator],
    ["/cosmos.staking.v1beta1.MsgDelegate", MsgDelegate],
    ["/cosmos.staking.v1beta1.MsgBeginRedelegate", MsgBeginRedelegate],
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
        msgUndelegate: (data) => ({ typeUrl: "/cosmos.staking.v1beta1.MsgUndelegate", value: MsgUndelegate.fromPartial(data) }),
        msgEditValidator: (data) => ({ typeUrl: "/cosmos.staking.v1beta1.MsgEditValidator", value: MsgEditValidator.fromPartial(data) }),
        msgCreateValidator: (data) => ({ typeUrl: "/cosmos.staking.v1beta1.MsgCreateValidator", value: MsgCreateValidator.fromPartial(data) }),
        msgDelegate: (data) => ({ typeUrl: "/cosmos.staking.v1beta1.MsgDelegate", value: MsgDelegate.fromPartial(data) }),
        msgBeginRedelegate: (data) => ({ typeUrl: "/cosmos.staking.v1beta1.MsgBeginRedelegate", value: MsgBeginRedelegate.fromPartial(data) }),
    };
};
const queryClient = async ({ addr: addr } = { addr: "http://localhost:1317" }) => {
    return new Api({ baseUrl: addr });
};
export { txClient, queryClient, };
