// checks that an error is returned if `toBlock` is greater than the latest block
>> {"jsonrpc":"2.0","id":1,"method":"eth_getLogs","params":[{"fromBlock":"0x29","toBlock":"0x2f"}]}
<< {"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"block range extends beyond current head block"}}
