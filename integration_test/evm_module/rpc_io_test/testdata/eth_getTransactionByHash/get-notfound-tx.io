// gets a non-existent transaction
>> {"jsonrpc":"2.0","id":1,"method":"eth_getTransactionByHash","params":["0x00000000000000000000000000000000000000000000000000000000deadbeef"]}
<< {"jsonrpc":"2.0","id":1,"result":null}
