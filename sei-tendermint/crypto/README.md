# crypto

crypto is the cryptographic package adapted for Tendermint's uses

## Importing it

To get the interfaces,
`import "github.com/tendermint/tendermint/crypto"`

For any specific algorithm, use its specific module e.g.
`import "github.com/tendermint/tendermint/crypto/ed25519"`

## Binary encoding

For Binary encoding, please refer to the [Tendermint encoding specification](https://docs.tendermint.com/master/spec/core/encoding.html).

## JSON Encoding

JSON encoding is done using tendermint's internal json encoder. For more information on JSON encoding, please refer to [Tendermint JSON encoding](https://github.com/tendermint/tendermint/blob/ccc990498df70f5a3df06d22476c9bb83812cbe3/libs/json/doc.go)

```go
Example JSON encodings:

ed25519.PrivKey     - {"type":"tendermint/PrivKeyEd25519","value":"EVkqJO/jIXp3rkASXfh9YnyToYXRXhBr6g9cQVxPFnQBP/5povV4HTjvsy530kybxKHwEi85iU8YL0qQhSYVoQ=="}
ed25519.PubKey      - {"type":"tendermint/PubKeyEd25519","value":"AT/+aaL1eB0477Mud9JMm8Sh8BIvOYlPGC9KkIUmFaE="}
crypto.PrivKeySecp256k1   - {"type":"tendermint/PrivKeySecp256k1","value":"zx4Pnh67N+g2V+5vZbQzEyRerX9c4ccNZOVzM9RvJ0Y="}
crypto.PubKeySecp256k1    - {"type":"tendermint/PubKeySecp256k1","value":"A8lPKJXcNl5VHt1FK8a244K9EJuS4WX1hFBnwisi0IJx"}
```
