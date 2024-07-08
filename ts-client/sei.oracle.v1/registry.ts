import { GeneratedType } from "@cosmjs/proto-signing";
import { MsgDelegateFeedConsent } from "./types/oracle/v1/tx";
import { MsgAggregateExchangeRateVote } from "./types/oracle/v1/tx";

const msgTypes: Array<[string, GeneratedType]>  = [
    ["/sei.oracle.v1.MsgDelegateFeedConsent", MsgDelegateFeedConsent],
    ["/sei.oracle.v1.MsgAggregateExchangeRateVote", MsgAggregateExchangeRateVote],
    
];

export { msgTypes }