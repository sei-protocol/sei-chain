// Sei: get EVM tx hash for Cosmos tx hash; invalid hex returns error
>> {"jsonrpc":"2.0","id":1,"method":"sei_getEvmTx","params":["nothex"]}
<< {"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"failed to decode cosmosHash"}}
