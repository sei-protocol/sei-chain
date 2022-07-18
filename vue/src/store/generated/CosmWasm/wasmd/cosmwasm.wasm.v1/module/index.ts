// THIS FILE IS GENERATED AUTOMATICALLY. DO NOT MODIFY.

import { StdFee } from "@cosmjs/launchpad";
import { SigningStargateClient } from "@cosmjs/stargate";
import { Registry, OfflineSigner, EncodeObject, DirectSecp256k1HdWallet } from "@cosmjs/proto-signing";
import { Api } from "./rest";
import { MsgInstantiateContract } from "./types/cosmwasm/wasm/v1/tx";
import { MsgExecuteContract } from "./types/cosmwasm/wasm/v1/tx";
import { MsgMigrateContract } from "./types/cosmwasm/wasm/v1/tx";
import { MsgIBCSend } from "./types/cosmwasm/wasm/v1/ibc";
import { MsgStoreCode } from "./types/cosmwasm/wasm/v1/tx";
import { MsgUpdateAdmin } from "./types/cosmwasm/wasm/v1/tx";
import { MsgClearAdmin } from "./types/cosmwasm/wasm/v1/tx";
import { MsgIBCCloseChannel } from "./types/cosmwasm/wasm/v1/ibc";


const types = [
  ["/cosmwasm.wasm.v1.MsgInstantiateContract", MsgInstantiateContract],
  ["/cosmwasm.wasm.v1.MsgExecuteContract", MsgExecuteContract],
  ["/cosmwasm.wasm.v1.MsgMigrateContract", MsgMigrateContract],
  ["/cosmwasm.wasm.v1.MsgIBCSend", MsgIBCSend],
  ["/cosmwasm.wasm.v1.MsgStoreCode", MsgStoreCode],
  ["/cosmwasm.wasm.v1.MsgUpdateAdmin", MsgUpdateAdmin],
  ["/cosmwasm.wasm.v1.MsgClearAdmin", MsgClearAdmin],
  ["/cosmwasm.wasm.v1.MsgIBCCloseChannel", MsgIBCCloseChannel],
  
];
export const MissingWalletError = new Error("wallet is required");

export const registry = new Registry(<any>types);

const defaultFee = {
  amount: [],
  gas: "200000",
};

interface TxClientOptions {
  addr: string
}

interface SignAndBroadcastOptions {
  fee: StdFee,
  memo?: string
}

const txClient = async (wallet: OfflineSigner, { addr: addr }: TxClientOptions = { addr: "http://localhost:26657" }) => {
  if (!wallet) throw MissingWalletError;
  let client;
  if (addr) {
    client = await SigningStargateClient.connectWithSigner(addr, wallet, { registry });
  }else{
    client = await SigningStargateClient.offline( wallet, { registry });
  }
  const { address } = (await wallet.getAccounts())[0];

  return {
    signAndBroadcast: (msgs: EncodeObject[], { fee, memo }: SignAndBroadcastOptions = {fee: defaultFee, memo: ""}) => client.signAndBroadcast(address, msgs, fee,memo),
    msgInstantiateContract: (data: MsgInstantiateContract): EncodeObject => ({ typeUrl: "/cosmwasm.wasm.v1.MsgInstantiateContract", value: MsgInstantiateContract.fromPartial( data ) }),
    msgExecuteContract: (data: MsgExecuteContract): EncodeObject => ({ typeUrl: "/cosmwasm.wasm.v1.MsgExecuteContract", value: MsgExecuteContract.fromPartial( data ) }),
    msgMigrateContract: (data: MsgMigrateContract): EncodeObject => ({ typeUrl: "/cosmwasm.wasm.v1.MsgMigrateContract", value: MsgMigrateContract.fromPartial( data ) }),
    msgIBCSend: (data: MsgIBCSend): EncodeObject => ({ typeUrl: "/cosmwasm.wasm.v1.MsgIBCSend", value: MsgIBCSend.fromPartial( data ) }),
    msgStoreCode: (data: MsgStoreCode): EncodeObject => ({ typeUrl: "/cosmwasm.wasm.v1.MsgStoreCode", value: MsgStoreCode.fromPartial( data ) }),
    msgUpdateAdmin: (data: MsgUpdateAdmin): EncodeObject => ({ typeUrl: "/cosmwasm.wasm.v1.MsgUpdateAdmin", value: MsgUpdateAdmin.fromPartial( data ) }),
    msgClearAdmin: (data: MsgClearAdmin): EncodeObject => ({ typeUrl: "/cosmwasm.wasm.v1.MsgClearAdmin", value: MsgClearAdmin.fromPartial( data ) }),
    msgIBCCloseChannel: (data: MsgIBCCloseChannel): EncodeObject => ({ typeUrl: "/cosmwasm.wasm.v1.MsgIBCCloseChannel", value: MsgIBCCloseChannel.fromPartial( data ) }),
    
  };
};

interface QueryClientOptions {
  addr: string
}

const queryClient = async ({ addr: addr }: QueryClientOptions = { addr: "http://localhost:1317" }) => {
  return new Api({ baseUrl: addr });
};

export {
  txClient,
  queryClient,
};
