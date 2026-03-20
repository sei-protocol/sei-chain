// Ethereum spec: eth_getLogs must return error (-32602) when block range extends beyond current head. Test fails if Sei returns result (e.g. []).
>> {"jsonrpc":"2.0","id":1,"method":"eth_getLogs","params":[{"fromBlock":"0x29","toBlock":"0x2f"}]}
<< {"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"block range extends beyond current head block"}}
