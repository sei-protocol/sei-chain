// gets fee history information
// speconly: client response is only checked for schema validity.
>> {"jsonrpc":"2.0","id":1,"method":"eth_feeHistory","params":["0x1","0x1b",[95,99]]}
<< {"jsonrpc":"2.0","id":1,"result":{"oldestBlock":"0x1b","reward":[["0x1","0x1"]],"baseFeePerGas":["0x3b9aca00","0x342dc057"],"gasUsedRatio":[0.0016543629259729885],"baseFeePerBlobGas":["0x0","0x0"],"blobGasUsedRatio":[0]}}
