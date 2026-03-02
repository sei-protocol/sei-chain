// estimates a contract call that reverts
// speconly: client response is only checked for schema validity.
>> {"jsonrpc":"2.0","id":1,"method":"eth_estimateGas","params":[{"from":"0x0102030000000000000000000000000000000000","input":"0xff030405","to":"0x17e7eedce4ac02ef114a7ed9fe6e2f33feba1667"}]}
<< {"jsonrpc":"2.0","id":1,"error":{"code":3,"message":"execution reverted","data":"0x77726f6e672d63616c6c6461746173697a65"}}
