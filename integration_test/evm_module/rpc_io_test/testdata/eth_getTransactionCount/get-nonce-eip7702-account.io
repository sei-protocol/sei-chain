// Retrieves the nonce for an account that has an EIP-7702 code delegation applied.
// For such accounts, the nonce stored in state does not match the 'transaction count'.
>> {"jsonrpc":"2.0","id":1,"method":"eth_getTransactionCount","params":["0xeda8645ba6948855e3b3cd596bbb07596d59c603","latest"]}
<< {"jsonrpc":"2.0","id":1,"result":"0x1"}
