import { StdFee } from "@cosmjs/launchpad";
import { Registry, OfflineSigner, EncodeObject } from "@cosmjs/proto-signing";
import { Api } from "./rest";
import { MsgChangeAdmin } from "./types/tokenfactory/tx";
import { MsgBurn } from "./types/tokenfactory/tx";
import { MsgMint } from "./types/tokenfactory/tx";
import { MsgCreateDenom } from "./types/tokenfactory/tx";
export declare const MissingWalletError: Error;
export declare const registry: Registry;
interface TxClientOptions {
    addr: string;
}
interface SignAndBroadcastOptions {
    fee: StdFee;
    memo?: string;
}
declare const txClient: (wallet: OfflineSigner, { addr: addr }?: TxClientOptions) => Promise<{
    signAndBroadcast: (msgs: EncodeObject[], { fee, memo }?: SignAndBroadcastOptions) => any;
    msgChangeAdmin: (data: MsgChangeAdmin) => EncodeObject;
    msgBurn: (data: MsgBurn) => EncodeObject;
    msgMint: (data: MsgMint) => EncodeObject;
    msgCreateDenom: (data: MsgCreateDenom) => EncodeObject;
}>;
interface QueryClientOptions {
    addr: string;
}
declare const queryClient: ({ addr: addr }?: QueryClientOptions) => Promise<Api<unknown>>;
export { txClient, queryClient, };
