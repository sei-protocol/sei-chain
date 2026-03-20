// Sei namespace: uninstall filter
>> {"jsonrpc":"2.0","id":1,"method":"sei_newBlockFilter"}
<< {"jsonrpc":"2.0","id":1,"result":"0x1"}
>> {"jsonrpc":"2.0","id":2,"method":"sei_uninstallFilter","params":["0x1"]}
<< {"jsonrpc":"2.0","id":2,"result":true}
