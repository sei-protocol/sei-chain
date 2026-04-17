// checks that an error is returned if `fromBlock` is larger than `toBlock`
>> {"jsonrpc":"2.0","id":1,"method":"eth_getLogs","params":[{"fromBlock":"0x29","toBlock":"0x26"}]}
<< {"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"invalid block range params"}}
