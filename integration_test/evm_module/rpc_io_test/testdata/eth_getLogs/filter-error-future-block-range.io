// Expect error (not empty result): toBlock must stay above chain head. Small fixed heights
// become historical as the chain grows and would incorrectly return [].
>> {"jsonrpc":"2.0","id":1,"method":"eth_getLogs","params":[{"fromBlock":"0x1","toBlock":"0x7fffffffffffffff"}]}
<< {"jsonrpc":"2.0","id":1,"error":{"code":-32602}}
