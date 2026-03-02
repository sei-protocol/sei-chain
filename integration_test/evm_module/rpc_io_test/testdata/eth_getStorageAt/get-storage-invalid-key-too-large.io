// requests an invalid storage key
>> {"jsonrpc":"2.0","id":1,"method":"eth_getStorageAt","params":["0xaa00000000000000000000000000000000000000","0x00000000000000000000000000000000000000000000000000000000000000000","latest"]}
<< {"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"storage key too long (want at most 32 bytes): \"0x00000000000000000000000000000000000000000000000000000000000000000\""}}
