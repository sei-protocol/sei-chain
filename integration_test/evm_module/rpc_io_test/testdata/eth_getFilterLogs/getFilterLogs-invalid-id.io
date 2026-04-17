// invalid filter id returns error
>> {"jsonrpc":"2.0","id":1,"method":"eth_getFilterLogs","params":["0x999"]}
<< {"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"filter not found"}}
