// gets tx with hash missing 0x prefix
>> {"jsonrpc":"2.0","id":1,"method":"debug_getRawTransaction","params":["1000000000000000000000000000000000000000000000000000000000000001"]}
<< {"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"invalid argument 0: json: cannot unmarshal hex string without 0x prefix into Go value of type common.Hash"}}
