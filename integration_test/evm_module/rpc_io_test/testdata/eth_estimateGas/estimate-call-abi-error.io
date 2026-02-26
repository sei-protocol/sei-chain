// estimates a contract call that reverts using Solidity Error(string) data
// speconly: client response is only checked for schema validity.
>> {"jsonrpc":"2.0","id":1,"method":"eth_estimateGas","params":[{"from":"0x0102030000000000000000000000000000000000","input":"0x01","to":"0x0ee3ab1371c93e7c0c281cc0c2107cdebc8b1930"}]}
<< {"jsonrpc":"2.0","id":1,"error":{"code":3,"message":"execution reverted: user error","data":"0x08c379a00000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000a75736572206572726f72"}}
