// THIS FILE IS GENERATED AUTOMATICALLY. DO NOT MODIFY.
import { SigningStargateClient } from "@cosmjs/stargate";
import { Registry } from "@cosmjs/proto-signing";
import { Api } from "./rest";
import { MsgIBCSend } from "./types/cosmwasm/wasm/v1/ibc";
import { MsgInstantiateContract } from "./types/cosmwasm/wasm/v1/tx";
import { MsgUpdateAdmin } from "./types/cosmwasm/wasm/v1/tx";
import { MsgStoreCode } from "./types/cosmwasm/wasm/v1/tx";
import { MsgExecuteContract } from "./types/cosmwasm/wasm/v1/tx";
import { MsgIBCCloseChannel } from "./types/cosmwasm/wasm/v1/ibc";
import { MsgMigrateContract } from "./types/cosmwasm/wasm/v1/tx";
import { MsgClearAdmin } from "./types/cosmwasm/wasm/v1/tx";
const types = [
    ["/cosmwasm.wasm.v1.MsgIBCSend", MsgIBCSend],
    ["/cosmwasm.wasm.v1.MsgInstantiateContract", MsgInstantiateContract],
    ["/cosmwasm.wasm.v1.MsgUpdateAdmin", MsgUpdateAdmin],
    ["/cosmwasm.wasm.v1.MsgStoreCode", MsgStoreCode],
    ["/cosmwasm.wasm.v1.MsgExecuteContract", MsgExecuteContract],
    ["/cosmwasm.wasm.v1.MsgIBCCloseChannel", MsgIBCCloseChannel],
    ["/cosmwasm.wasm.v1.MsgMigrateContract", MsgMigrateContract],
    ["/cosmwasm.wasm.v1.MsgClearAdmin", MsgClearAdmin],
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
        msgIBCSend: (data) => ({ typeUrl: "/cosmwasm.wasm.v1.MsgIBCSend", value: MsgIBCSend.fromPartial(data) }),
        msgInstantiateContract: (data) => ({ typeUrl: "/cosmwasm.wasm.v1.MsgInstantiateContract", value: MsgInstantiateContract.fromPartial(data) }),
        msgUpdateAdmin: (data) => ({ typeUrl: "/cosmwasm.wasm.v1.MsgUpdateAdmin", value: MsgUpdateAdmin.fromPartial(data) }),
        msgStoreCode: (data) => ({ typeUrl: "/cosmwasm.wasm.v1.MsgStoreCode", value: MsgStoreCode.fromPartial(data) }),
        msgExecuteContract: (data) => ({ typeUrl: "/cosmwasm.wasm.v1.MsgExecuteContract", value: MsgExecuteContract.fromPartial(data) }),
        msgIBCCloseChannel: (data) => ({ typeUrl: "/cosmwasm.wasm.v1.MsgIBCCloseChannel", value: MsgIBCCloseChannel.fromPartial(data) }),
        msgMigrateContract: (data) => ({ typeUrl: "/cosmwasm.wasm.v1.MsgMigrateContract", value: MsgMigrateContract.fromPartial(data) }),
        msgClearAdmin: (data) => ({ typeUrl: "/cosmwasm.wasm.v1.MsgClearAdmin", value: MsgClearAdmin.fromPartial(data) }),
    };
};
const queryClient = async ({ addr: addr } = { addr: "http://localhost:1317" }) => {
    return new Api({ baseUrl: addr });
};
export { txClient, queryClient, };
