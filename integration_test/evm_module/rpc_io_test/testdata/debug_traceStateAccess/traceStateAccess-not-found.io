// Sei extension: tx not found returns error
>> {"jsonrpc":"2.0","id":1,"method":"debug_traceStateAccess","params":["0x0000000000000000000000000000000000000000000000000000000000000000"]}
<< {"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"transaction not found"}}
