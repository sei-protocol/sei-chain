// THIS FILE IS GENERATED AUTOMATICALLY. DO NOT MODIFY.
import { SigningStargateClient } from "@cosmjs/stargate";
import { Registry } from "@cosmjs/proto-signing";
import { Api } from "./rest";
import { MsgCreateDenom } from "./types/tokenfactory/tx";
import { MsgBurn } from "./types/tokenfactory/tx";
import { MsgChangeAdmin } from "./types/tokenfactory/tx";
import { MsgMint } from "./types/tokenfactory/tx";
const types = [
    ["/seiprotocol.seichain.tokenfactory.MsgCreateDenom", MsgCreateDenom],
    ["/seiprotocol.seichain.tokenfactory.MsgBurn", MsgBurn],
    ["/seiprotocol.seichain.tokenfactory.MsgChangeAdmin", MsgChangeAdmin],
    ["/seiprotocol.seichain.tokenfactory.MsgMint", MsgMint],
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
        msgCreateDenom: (data) => ({ typeUrl: "/seiprotocol.seichain.tokenfactory.MsgCreateDenom", value: MsgCreateDenom.fromPartial(data) }),
        msgBurn: (data) => ({ typeUrl: "/seiprotocol.seichain.tokenfactory.MsgBurn", value: MsgBurn.fromPartial(data) }),
        msgChangeAdmin: (data) => ({ typeUrl: "/seiprotocol.seichain.tokenfactory.MsgChangeAdmin", value: MsgChangeAdmin.fromPartial(data) }),
        msgMint: (data) => ({ typeUrl: "/seiprotocol.seichain.tokenfactory.MsgMint", value: MsgMint.fromPartial(data) }),
    };
};
const queryClient = async ({ addr: addr } = { addr: "http://localhost:1317" }) => {
    return new Api({ baseUrl: addr });
};
export { txClient, queryClient, };
