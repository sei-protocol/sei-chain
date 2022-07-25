import { StdFee } from "@cosmjs/launchpad";
import { Registry, OfflineSigner, EncodeObject } from "@cosmjs/proto-signing";
import { Api } from "./rest";
import { MsgPlaceOrders } from "./types/dex/tx";
import { MsgCancelOrders } from "./types/dex/tx";
import { MsgRegisterContract } from "./types/dex/tx";
import { MsgLiquidation } from "./types/dex/tx";
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
    msgPlaceOrders: (data: MsgPlaceOrders) => EncodeObject;
    msgCancelOrders: (data: MsgCancelOrders) => EncodeObject;
    msgRegisterContract: (data: MsgRegisterContract) => EncodeObject;
    msgLiquidation: (data: MsgLiquidation) => EncodeObject;
}>;
interface QueryClientOptions {
    addr: string;
}
declare const queryClient: ({ addr: addr }?: QueryClientOptions) => Promise<Api<unknown>>;
export { txClient, queryClient, };
