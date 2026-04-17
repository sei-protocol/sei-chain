// create filter then uninstall it
>> {"jsonrpc":"2.0","id":1,"method":"eth_newBlockFilter"}
<< {"jsonrpc":"2.0","id":1,"result":"0x1"}
>> {"jsonrpc":"2.0","id":2,"method":"eth_uninstallFilter","params":["0x1"]}
<< {"jsonrpc":"2.0","id":2,"result":true}
