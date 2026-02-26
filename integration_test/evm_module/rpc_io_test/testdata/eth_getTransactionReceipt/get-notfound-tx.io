// requests the receipt for a non-existent tx hash
>> {"jsonrpc":"2.0","id":1,"method":"eth_getTransactionReceipt","params":["0x00000000000000000000000000000000000000000000000000000000deadbeef"]}
<< {"jsonrpc":"2.0","id":1,"result":null}
