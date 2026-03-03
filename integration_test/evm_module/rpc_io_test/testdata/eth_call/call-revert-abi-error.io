// calls a contract that reverts with an ABI-encoded Error(string) value
>> {"jsonrpc":"2.0","id":1,"method":"eth_call","params":[{"from":"0x0000000000000000000000000000000000000000","gas":"0x186a0","input":"0x01","to":"0x0ee3ab1371c93e7c0c281cc0c2107cdebc8b1930"},"latest"]}
<< {"jsonrpc":"2.0","id":1,"error":{"code":3,"message":"execution reverted: user error","data":"0x08c379a00000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000a75736572206572726f72"}}
