import { GeneratedType } from "@cosmjs/proto-signing";
import { MsgRegisterPointer } from "./types/evm/v1/tx";
import { MsgEVMTransaction } from "./types/evm/v1/tx";
import { MsgSend } from "./types/evm/v1/tx";
import { MsgAssociateContractAddress } from "./types/evm/v1/tx";
import { MsgAssociate } from "./types/evm/v1/tx";

const msgTypes: Array<[string, GeneratedType]>  = [
    ["/sei.evm.v1.MsgRegisterPointer", MsgRegisterPointer],
    ["/sei.evm.v1.MsgEVMTransaction", MsgEVMTransaction],
    ["/sei.evm.v1.MsgSend", MsgSend],
    ["/sei.evm.v1.MsgAssociateContractAddress", MsgAssociateContractAddress],
    ["/sei.evm.v1.MsgAssociate", MsgAssociate],
    
];

export { msgTypes }