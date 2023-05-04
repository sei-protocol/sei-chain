import { StdFee } from "@cosmjs/launchpad";
import { Registry, OfflineSigner, EncodeObject } from "@cosmjs/proto-signing";
import { Api } from "./rest";
import { MsgBeginRedelegate } from "./types/cosmos/staking/v1beta1/tx";
import { MsgCreateValidator } from "./types/cosmos/staking/v1beta1/tx";
import { MsgDelegate } from "./types/cosmos/staking/v1beta1/tx";
import { MsgEditValidator } from "./types/cosmos/staking/v1beta1/tx";
import { MsgUndelegate } from "./types/cosmos/staking/v1beta1/tx";
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
    msgBeginRedelegate: (data: MsgBeginRedelegate) => EncodeObject;
    msgCreateValidator: (data: MsgCreateValidator) => EncodeObject;
    msgDelegate: (data: MsgDelegate) => EncodeObject;
    msgEditValidator: (data: MsgEditValidator) => EncodeObject;
    msgUndelegate: (data: MsgUndelegate) => EncodeObject;
}>;
interface QueryClientOptions {
    addr: string;
}
declare const queryClient: ({ addr: addr }?: QueryClientOptions) => Promise<Api<unknown>>;
export { txClient, queryClient, };
