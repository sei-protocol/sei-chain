// eth_sendTransaction requires unlocked account; over HTTP often returns error
>> {"jsonrpc":"2.0","id":1,"method":"eth_sendTransaction","params":[{"from":"0xb60e8dd61c5d32be8058bb8eb970870f07233155","to":"0xd46e8dd67c5d32be8058bb8eb970870f07244567","gas":"0x76c0","gasPrice":"0x9184e72a000","value":"0x9184e72a","input":"0x"}]}
<< {"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not supported"}}
