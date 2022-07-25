// THIS FILE IS GENERATED AUTOMATICALLY. DO NOT MODIFY.

import { StdFee } from "@cosmjs/launchpad";
import { SigningStargateClient } from "@cosmjs/stargate";
import { Registry, OfflineSigner, EncodeObject, DirectSecp256k1HdWallet } from "@cosmjs/proto-signing";
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
    msgUndelegate: (data: MsgUndelegate): EncodeObject => ({ typeUrl: "/cosmos.staking.v1beta1.MsgUndelegate", value: MsgUndelegate.fromPartial( data ) }),
    msgEditValidator: (data: MsgEditValidator): EncodeObject => ({ typeUrl: "/cosmos.staking.v1beta1.MsgEditValidator", value: MsgEditValidator.fromPartial( data ) }),
    msgCreateValidator: (data: MsgCreateValidator): EncodeObject => ({ typeUrl: "/cosmos.staking.v1beta1.MsgCreateValidator", value: MsgCreateValidator.fromPartial( data ) }),
    msgDelegate: (data: MsgDelegate): EncodeObject => ({ typeUrl: "/cosmos.staking.v1beta1.MsgDelegate", value: MsgDelegate.fromPartial( data ) }),
    msgBeginRedelegate: (data: MsgBeginRedelegate): EncodeObject => ({ typeUrl: "/cosmos.staking.v1beta1.MsgBeginRedelegate", value: MsgBeginRedelegate.fromPartial( data ) }),
    
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
