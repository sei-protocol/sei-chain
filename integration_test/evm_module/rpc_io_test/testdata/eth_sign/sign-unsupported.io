// eth_sign requires unlocked account; over HTTP often returns error
>> {"jsonrpc":"2.0","id":1,"method":"eth_sign","params":["0x9b2055d370f73ec7d8a03e965129118dc8f5bf83","0xdeadbeaf"]}
<< {"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not supported"}}
