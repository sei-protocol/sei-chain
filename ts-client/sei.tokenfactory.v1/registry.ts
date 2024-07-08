import { GeneratedType } from "@cosmjs/proto-signing";
import { MsgBurn } from "./types/tokenfactory/v1/tx";
import { MsgCreateDenom } from "./types/tokenfactory/v1/tx";
import { MsgMint } from "./types/tokenfactory/v1/tx";
import { MsgChangeAdmin } from "./types/tokenfactory/v1/tx";
import { MsgSetDenomMetadata } from "./types/tokenfactory/v1/tx";

const msgTypes: Array<[string, GeneratedType]>  = [
    ["/sei.tokenfactory.v1.MsgBurn", MsgBurn],
    ["/sei.tokenfactory.v1.MsgCreateDenom", MsgCreateDenom],
    ["/sei.tokenfactory.v1.MsgMint", MsgMint],
    ["/sei.tokenfactory.v1.MsgChangeAdmin", MsgChangeAdmin],
    ["/sei.tokenfactory.v1.MsgSetDenomMetadata", MsgSetDenomMetadata],
    
];

export { msgTypes }