// Sei extension: tx not found returns error
>> {"jsonrpc":"2.0","id":1,"method":"eth_getVMError","params":["0x0000000000000000000000000000000000000000000000000000000000000000"]}
<< {"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"receipt not found"}}
