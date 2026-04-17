// Sei: get EVM address for Sei address; invalid bech32 returns error
>> {"jsonrpc":"2.0","id":1,"method":"sei_getEVMAddress","params":["invalid"]}
<< {"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"decoding bech32 failed"}}
