// THIS FILE IS GENERATED AUTOMATICALLY. DO NOT MODIFY.

import { StdFee } from "@cosmjs/launchpad";
import { SigningStargateClient } from "@cosmjs/stargate";
import { Registry, OfflineSigner, EncodeObject, DirectSecp256k1HdWallet } from "@cosmjs/proto-signing";
import { Api } from "./rest";
import { MsgAggregateExchangeRatePrevote } from "./types/oracle/tx";
import { MsgAggregateExchangeRateVote } from "./types/oracle/tx";
import { MsgAggregateExchangeRateCombinedVote } from "./types/oracle/tx";
import { MsgDelegateFeedConsent } from "./types/oracle/tx";


const types = [
  ["/seiprotocol.seichain.oracle.MsgAggregateExchangeRatePrevote", MsgAggregateExchangeRatePrevote],
  ["/seiprotocol.seichain.oracle.MsgAggregateExchangeRateVote", MsgAggregateExchangeRateVote],
  ["/seiprotocol.seichain.oracle.MsgAggregateExchangeRateCombinedVote", MsgAggregateExchangeRateCombinedVote],
  ["/seiprotocol.seichain.oracle.MsgDelegateFeedConsent", MsgDelegateFeedConsent],
  
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
    msgAggregateExchangeRatePrevote: (data: MsgAggregateExchangeRatePrevote): EncodeObject => ({ typeUrl: "/seiprotocol.seichain.oracle.MsgAggregateExchangeRatePrevote", value: MsgAggregateExchangeRatePrevote.fromPartial( data ) }),
    msgAggregateExchangeRateVote: (data: MsgAggregateExchangeRateVote): EncodeObject => ({ typeUrl: "/seiprotocol.seichain.oracle.MsgAggregateExchangeRateVote", value: MsgAggregateExchangeRateVote.fromPartial( data ) }),
    msgAggregateExchangeRateCombinedVote: (data: MsgAggregateExchangeRateCombinedVote): EncodeObject => ({ typeUrl: "/seiprotocol.seichain.oracle.MsgAggregateExchangeRateCombinedVote", value: MsgAggregateExchangeRateCombinedVote.fromPartial( data ) }),
    msgDelegateFeedConsent: (data: MsgDelegateFeedConsent): EncodeObject => ({ typeUrl: "/seiprotocol.seichain.oracle.MsgDelegateFeedConsent", value: MsgDelegateFeedConsent.fromPartial( data ) }),
    
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
