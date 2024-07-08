import { GeneratedType } from "@cosmjs/proto-signing";
import { MsgCancelOrders } from "./types/dex/tx";
import { MsgPlaceOrders } from "./types/dex/tx";
import { MsgContractDepositRent } from "./types/dex/tx";
import { MsgRegisterContract } from "./types/dex/tx";
import { MsgUnregisterContract } from "./types/dex/tx";
import { MsgRegisterPairs } from "./types/dex/tx";
import { MsgUpdatePriceTickSize } from "./types/dex/tx";
import { MsgUnsuspendContract } from "./types/dex/tx";
import { MsgUpdateQuantityTickSize } from "./types/dex/tx";

const msgTypes: Array<[string, GeneratedType]>  = [
    ["/seiprotocol.seichain.dex.MsgCancelOrders", MsgCancelOrders],
    ["/seiprotocol.seichain.dex.MsgPlaceOrders", MsgPlaceOrders],
    ["/seiprotocol.seichain.dex.MsgContractDepositRent", MsgContractDepositRent],
    ["/seiprotocol.seichain.dex.MsgRegisterContract", MsgRegisterContract],
    ["/seiprotocol.seichain.dex.MsgUnregisterContract", MsgUnregisterContract],
    ["/seiprotocol.seichain.dex.MsgRegisterPairs", MsgRegisterPairs],
    ["/seiprotocol.seichain.dex.MsgUpdatePriceTickSize", MsgUpdatePriceTickSize],
    ["/seiprotocol.seichain.dex.MsgUnsuspendContract", MsgUnsuspendContract],
    ["/seiprotocol.seichain.dex.MsgUpdateQuantityTickSize", MsgUpdateQuantityTickSize],
    
];

export { msgTypes }