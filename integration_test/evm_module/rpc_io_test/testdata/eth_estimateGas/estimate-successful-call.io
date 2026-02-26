// estimates a successful contract call
// speconly: client response is only checked for schema validity.
>> {"jsonrpc":"2.0","id":1,"method":"eth_estimateGas","params":[{"from":"0x0102030000000000000000000000000000000000","input":"0xff01","to":"0x17e7eedce4ac02ef114a7ed9fe6e2f33feba1667"}]}
<< {"jsonrpc":"2.0","id":1,"result":"0x5316"}
