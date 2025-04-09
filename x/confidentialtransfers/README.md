# Confidential Transfers

The `confidentialtransfers` module allows users to maintain confidential token balances.
By depositing to their confidential balances, users can make confidential transfers to other
users' confidential balances that obfuscate the amounts sent and received.

This module is designed to be similar to the Bank module, which maps addresses to balances for each denom.
With the `confidentialtransfers` module, the mapping is between addresses and an `account` structure for each denom.
This account stores encrypted information about the users confidential balances.

The mechanics of this account based confidential transfers system is inspired by the [Zether](https://eprint.iacr.org/2019/191.pdf) paper,
as well as [PGC: Decentralized Confidential Payment System with Auditability](https://eprint.iacr.org/2019/319.pdf) which details improvements to the Zether protocol.

## Account State
Each Confidential Balance is encapsulated as an account struct
```go
type Account struct {
	// The Public Key, used for Twisted El Gamal Encryption
	PublicKey curves.Point

	// The TEG encrypted low 32 bits of the pending balance.
	// This is calculated as Encrypted(encryptionPK, <low_32_bits_pending_balance>)
	PendingBalanceLo *elgamal.Ciphertext

	// The TEG encrypted high bits (17th bit and above) of the pending balance.
	// This is calculated as Encrypted(encryptionPK, <high_bits_pending_balance>)
	// Where <high_bits_pending_balance> is at most a 48 bit number.
	PendingBalanceHi *elgamal.Ciphertext

	// The amount of transfers into this account that have not been applied.
	// This is capped at 2^16 to prevent PendingBalanceLo from overflowing.
	PendingBalanceCreditCounter uint16

	// The available balance, encrypted using the Twisted El Gamal public key.
	// This is calculated as Encrypted(encryptionPK, <available_balance>)
	AvailableBalance *elgamal.Ciphertext

	// The available balance, encrypted using an AES Key.
	// This is calculated as AsymmetricEncryption(otherPK, <available_balance>)
	// This is stored as the Base64-encoded ciphertext
	DecryptableAvailableBalance string
}
```

Fields of the Account struct:
- `PublicKey`: The public key of the user, used for encryption and decryption of the confidential balance.
- `PendingBalanceLo`: The low 32 bits of the pending balance, encrypted using the user's public key.
- `PendingBalanceHi`: The high bits of the pending balance, encrypted using the user's public key.
- `PendingBalanceCreditCounter`: A counter for the number of pending balance credits, capped at 2^16 to prevent overflow.
- `AvailableBalance`: The available balance, encrypted using the user's public key.
- `DecryptableAvailableBalance`: The available balance, encrypted using an AES key, stored as a Base64-encoded ciphertext.

## Messages

### InitializeAccount

Initializes a Confidential Balance Account of the given denom for the address.
A Confidential Balance Account is required in order to send and receive tokens confidentially.

```protobuf
// MsgInitializeAccount represents a message to create a new confidential transfer account.
message MsgInitializeAccount {
    string   from_address = 1 [(gogoproto.moretags) = "yaml:\"from_address\""];
    string   denom = 2 [(gogoproto.moretags) = "yaml:\"denom\""];
    bytes public_key = 3 [(gogoproto.moretags) = "yaml:\"public_key\""];
    string decryptable_balance = 4 [(gogoproto.moretags) = "yaml:\"decryptable_balance\""];
    Ciphertext pending_balance_lo = 5 [(gogoproto.moretags) = "yaml:\"pending_balance_lo\""];
    Ciphertext pending_balance_hi = 6 [(gogoproto.moretags) = "yaml:\"pending_balance_hi\""];
    Ciphertext available_balance = 7 [(gogoproto.moretags) = "yaml:\"available_balance\""];
    InitializeAccountMsgProofs proofs = 8 [(gogoproto.moretags) = "yaml:\"proofs\""];
}

message InitializeAccountMsgProofs {
    PubkeyValidityProof pubkey_validity_proof = 1;
    ZeroBalanceProof zero_pending_balance_lo_proof = 2;
    ZeroBalanceProof zero_pending_balance_hi_proof = 3;
    ZeroBalanceProof zero_available_balance_proof = 4;
}
```

To initialize an account, the user must provide:
1. A Twisted El Gamal public key for which the user has the corresponding private key.
2. 3 ciphertexts, all of which are encryptions of the value 0 using the given public key.
These will form the initial balances (`pending_balance_lo`, `pending_balance_hi`, `available_balance`) of the account.
3. A `decryptable_balance` - this is the available balance (0) encrypted using a symmetric AES key that the user keeps.
This `decryptable_balance` allows the user to efficiently decrypt their available balance.
4. Zero Knowledge Proofs that the public key is valid and that the ciphertexts are encryptions of 0.

These can be generated using the encryption libraries and client libraries.

**Proof Verifications:**
- Verify that the public key sent by the user is a valid Twisted El Gamal Public Key
- Verify that the encrypted account balances sent by the user encrypt 0.

**State Modifications:**
- Creates a mapping from address: account via the confidential transfers keeper.

---

### Deposit
Deposits tokens into the users Confidential Account Balance.
```protobuf
// MsgDeposit represents a message for depositing tokens into a confidential account
message MsgDeposit {
    string from_address = 1 [(gogoproto.moretags) = "yaml:\"from_address\""];
    string denom = 2 [(gogoproto.moretags) = "yaml:\"denom\""];
    uint64 amount = 3 [(gogoproto.moretags) = "yaml:\"amount\""];
}
```
**State Modifications:**
- Updates the pending balances of the confidential account with the given denom.
- Updates the balances of the user in the bank module.

---

### ApplyPendingBalance

Applies any incoming transfers that are in the account's pending balance to the available balance. 
This step is required before the user can spend received funds.

```protobuf
// MsgApplyPendingBalance represents a message to apply incoming pending transfers.
message MsgApplyPendingBalance {
  string address = 1 [(gogoproto.moretags) = "yaml:\"address\""];
  string denom = 2 [(gogoproto.moretags) = "yaml:\"denom\""];
  string new_decryptable_available_balance = 3 [(gogoproto.moretags) = "yaml:\"new_decryptable_available_balance\""];
  uint32 current_pending_balance_counter = 4 [(gogoproto.moretags) = "yaml:\"current_pending_balance_counter\""];
  Ciphertext current_available_balance = 5 [(gogoproto.moretags) = "yaml:\"current_available_balance\""];
}
```

To apply the pending balance, the user must provide:
1. The current value of the `pending_balance_credit_counter` - the operation will fail if this does not match the on-chain value.
2. The current `available_balance` ciphertext - the operation will fail if doesn't match the on-chain ciphertext.
3. `decryptable_available_balance`: The updated available balance after balances are applied, encrypted using the AES key.

**Verifications:**
- Ensure the `pending_balance_credit_counter` provided matches the on-chain counter.
- Ensure that the 'current_available_balance' provided matches the on-chain available balance.

**State Modifications:**
- Updates the account struct by increasing the available balance by the pending balances.
- Resets the `pending_balance_lo`, `pending_balance_hi`, and `pending_balance_credit_counter` to 0.
- Updates the `decryptable_available_balance` field.

---

### Transfer

Transfers an encrypted amount from the sender's confidential account to the recipient's confidential account.
The amount transferred must be not more than a 48 bit number.

```protobuf
// MsgTransfer represents a message to send coins confidentially.
message MsgTransfer {
    string   from_address = 1;
    string   to_address = 2;
    string   denom = 3;
    Ciphertext from_amount_lo = 4;
    Ciphertext from_amount_hi = 5;
    Ciphertext to_amount_lo = 6;
    Ciphertext to_amount_hi = 7;
    Ciphertext remaining_balance = 8;
    string decryptable_balance = 9;
    TransferMsgProofs proofs = 10;
    repeated Auditor auditors = 11;
}

message TransferMsgProofs {
    CiphertextValidityProof remaining_balance_commitment_validity_proof = 1;
    CiphertextValidityProof sender_transfer_amount_lo_validity_proof = 2;
    CiphertextValidityProof sender_transfer_amount_hi_validity_proof = 3;
    CiphertextValidityProof recipient_transfer_amount_lo_validity_proof = 4;
    CiphertextValidityProof recipient_transfer_amount_hi_validity_proof = 5;
    RangeProof remaining_balance_range_proof = 6;
    CiphertextCommitmentEqualityProof remaining_balance_equality_proof = 7;
    CiphertextCiphertextEqualityProof transfer_amount_lo_equality_proof = 8;
    CiphertextCiphertextEqualityProof transfer_amount_hi_equality_proof = 9;
    RangeProof transfer_amount_lo_range_proof = 10;
    RangeProof transfer_amount_hi_range_proof = 11;
}
```

To send a confidential transfer, the sender must provide:
1. Transfer amount, split into lo and hi bits (lo = 16 bits, hi = 32 bits). This is encrypted with both
 - The Senders Public Key
 - The Recipients Public Key
2. `remaining_balance`: The senders remaining available balance after the transfer, encrypted using the senders public key.
3. `decryptable_balance`: The senders remaining available balance after the transfer, encrypted using the senders AES key.
4. Zero-knowledge proofs verifying:
  - Ciphertexts were encrypted using the correct public keys.
  - Sender has enough funds to make the transfer.
  - The `remaining_available_balance` is correctly calculated.
  - Encrypted transfer values are within the correct range.
  - Encrypted transfer values are equal to each other.
5. (Optional) Encrypted transfer amounts and proofs for each auditor, if the sender wants to include them.

**Verifications:**
- Validate that all ciphertexts are generated using the correct public keys.
- Validate that the sender has sufficient balance to make the transfer. This is done by:
  - Validating that the `remaining_balance` ciphertext encrypts a value greater than 0
  - Validating that the `remaining_balance` ciphertext is equal to the sender's available balance minus the transfer amount.
- Validate that the transfer amounts encrypted using different public keys are equal to each other.
- Validate that transfer_amount_lo and transfer_amount_hi are within the correct range. (16 bit and 32 bit respectively)
- Check integrity of auditor fields if provided.

**State Modifications:**
- Deducts amount from sender’s available balance.
- Updates sender's decryptable balance.
- Adds amount to recipient’s pending balance and increases recipient's `pending_balance_counter`.

---

### Withdraw

Withdraws funds from the confidential balance into the user’s public (bank module) balance.

```protobuf
// MsgWithdraw represents a message to withdraw from a confidential module account.
message MsgWithdraw {
    string from_address = 1;
    string denom = 2;
    string amount = 3;
    string decryptable_balance = 4;
    Ciphertext remaining_balance_commitment = 5;
    WithdrawMsgProofs proofs = 6;
}

message WithdrawMsgProofs {
    RangeProof remaining_balance_range_proof = 1;
    CiphertextCommitmentEqualityProof remaining_balance_equality_proof = 2;
}
```

To withdraw tokens, the user must provide:
1. Amount to withdraw (in plaintext).
2. `remaining_balance_commitment`: The remaining `available_balance` after the withdrawal, encrypted using the users public key.
3. `decryptable_balance`: The remaining `available_balance` after the withdrawal, encrypted using the users AES key.
4. ZK Proofs:
  - That the `remaining_balance_commitment` encodes a value greater than 0.
  - That the `remaining_balance_commitment` is equal to the available balance minus the withdrawal amount.

**Verifications:**
- Check that the account has sufficient funds to make the withdrawal. This is done by verifying that:
  1. The `remaining_balance_commitment` ciphertext encrypts a value greater than 0.
  2. The `remaining_balance_commitment` ciphertext is equal to the available balance minus the withdrawal amount.

**State Modifications:**
- Subtracts amount from the available balance.
- Updates the decryptable balance.
- Sends the withdrawn amount to the user’s public token balance via the bank module.

---

### CloseAccount

Closes a confidential account if and only if the confidential balances are all zero.

```protobuf
// MsgCloseAccount represents a message to close a confidential token account.
message MsgCloseAccount {
    string address = 1;
    string denom = 2;
    CloseAccountMsgProofs proofs = 3;
}

message CloseAccountMsgProofs {
    ZeroBalanceProof zero_available_balance_proof = 1;
    ZeroBalanceProof zero_pending_balance_lo_proof = 2;
    ZeroBalanceProof zero_pending_balance_hi_proof = 3;
}
```

To close an account, the user must submit:
- Proofs that `available_balance`, `pending_balance_lo`, and `pending_balance_hi` are all encryptions of zero.

**Verifications:**
- Validate ZK proofs that the encrypted values encrypt the zero value.

**State Modifications:**
- Deletes the account entry in the module state for the address and denom.






## Examples

The `confidentialtransfers` module allows users to maintain and move confidential balances on Sei. Below are some example `seid` CLI commands demonstrating how to initialize accounts, deposit funds, transfer confidentially, and withdraw back to public balances.

---

### Initialize a Confidential Token Account

Before sending or receiving confidential transfers, you must initialize a confidential token account for the desired denom.

```sh
seid tx ct init-account usei --from sender --fees=20000usei
```

> Replace `sender` with the name of your local key (e.g. created via `seid keys add sender`).

---

### Deposit Tokens into Confidential Balance

Once initialized, deposit tokens into your confidential balance.

```sh
seid tx ct deposit 500000usei --from sender --fees=20000usei
```

> Note: This is a public transaction. The deposit amount is visible on-chain.

---

### Apply Pending Balances

Tokens are initially received into a pending balance. You must apply them before you can spend them.

```sh
seid tx ct apply-pending-balance usei --from sender --fees=20000usei
```

---

### Transfer Tokens Confidentially

Transfer confidential tokens from one account to another. The recipient must have already initialized a confidential account for the same denom.

```sh
seid tx ct transfer sei1recipientaddr... 500000usei --from sender --fees=20000usei --gas=3000000
```
> Note: This transaction consumes a lot more gas then a normal transfer, as it requires Range proof verification.
---

### Withdraw Tokens to Public Balance

To convert confidential tokens back into public balances, use the `withdraw` command:

```sh
seid tx ct withdraw 500000usei --from recipient --fees=20000usei --gas=3000000
```

> Note: This transaction consumes a lot more gas then a normal transfer, as it requires Range proof verification.

---

### Query Confidential Account (Encrypted View)

View a confidential account’s state with encrypted balances (default view):

```sh
seid query ct account usei sei1senderaddress...
```

---

### Query Confidential Account (Decrypted View)

If you are the account owner, you can view the decrypted balance using your local key:

```sh
seid query ct account usei sei1senderaddress... --decryptor sender
```

### 🧾 Query Decrypted Transaction Details

If you want to inspect a confidential transfer’s decrypted fields and you’re the sender or recipient, you can pass the decryptor flag when querying the transaction:

```sh
seid query tx <tx_hash> --decryptor sender
```

> Replace `<tx_hash>` with the actual transaction hash returned when the confidential transfer was submitted. You will be able to see decrypted values such as the transfer amount and remaining balance if your local key matches the sender or recipient.

---

## Appendix A: Pending vs Available Balance

To protect against front-running attacks and maintain the correctness of zero-knowledge proofs, the confidential account structure on Sei separates balances into two components:

- **Available Balance**: Tokens that the account owner can immediately spend.
- **Pending Balance**: Tokens received from others, which are not yet spendable.

### Why this Split?

Zero-knowledge proofs are generated off-chain against the current state of an account. If another user sends tokens to your account before your transaction is processed, the encrypted state changes—causing your proof to become invalid.

An attacker could exploit this by continuously sending small transfers to your account, effectively preventing your transactions from succeeding. This is known as a **proof invalidation attack**.

To prevent this, **incoming transfers are not applied directly to your spendable balance**, but are instead held in a pending state.

### Example: Safe Transfer Flow

1. Alice has an available balance of 50.
2. Bob transfers 10 tokens to Alice.
3. Alice’s account state becomes:
  - `available_balance = 50`
  - `pending_balance = 10`

Bob’s transfer does **not** affect Alice’s spendable balance, ensuring her in-progress transactions are unaffected.

To use the 10 tokens, Alice must **explicitly apply** her pending balance:

```go
ApplyPendingBalance {
  from: Alice,
  ...
}
```

This moves the encrypted pending amount into the available balance, a step only the account owner can perform.

---

This separation ensures that:
- **Only the account owner** can modify their available balance.
- Zero-knowledge proofs remain valid even in the presence of incoming transfers.
- The system remains resistant to front-running or denial-of-service attacks targeting confidential account state.

## Appendix B: Available Balance vs Decryptable Available Balance

Confidential accounts on Sei maintain **two versions** of the available balance:

- `available_balance`: Encrypted using ElGamal, used for proving correctness in zero-knowledge circuits.
- `decryptable_available_balance`: Encrypted using symmetric authenticated encryption (e.g., AES), easily decryptable by the user.

### Why Maintain Two Versions?

The `available_balance` is stored as an ElGamal ciphertext to support **homomorphic operations** and **zero-knowledge proofs** during confidential transfers. However, ElGamal ciphertexts are **not easily decryptable** by client applications without performing expensive cryptographic operations or brute-force-style scanning.

To improve **user experience and performance**, the same balance is also encrypted using a symmetric scheme and stored as `decryptable_available_balance`. This value can be decrypted locally by the account owner using a symmetric key, which can either be:
- Independently generated and stored client-side, or
- Derived deterministically from the owner’s signing key.

### Warning

This `decryptable_avialable_balance` is a **convenience field** and should not be used for cryptographic proofs or validation.
The chain does **not** validate correctness of this field, and it is the responsibility of the client to ensure that the correct value is sent so it matches the `available_balance` field.

Since client libraries depend on the `decryptable_available_balance`, an incorrect `decryptable_available_balance` will lead to failed transactions.
To resolve this, the client will need to decrypt the `available_balance` and update the `decryptable_available_balance` field to the correct value for the subsequent transaction.

### Usage Guidelines

- Clients should **use `decryptable_available_balance`** for displaying and tracking their confidential balance.
- `available_balance` should only be used to **generate or verify zero-knowledge proofs**, such as when transferring tokens or applying pending balance.
- Both fields must represent the **same balance** at all times. Instructions that modify the available balance (`Transfer`, `ApplyPendingBalance`, `Withdraw`) **must include an updated `decryptable_available_balance`**.

### Design Summary

The addition of a decryptable field is a **user-convenience optimization** that enables fast, low-cost access to confidential balances without weakening privacy or correctness guarantees.

It ensures that users can:
- Quickly see their confidential balance
- Avoid performing on-chain ZK proof validations or off-chain decryption every time they want to view their funds

## Appendix C: Splitting Pending Balance into Lo and Hi

Unlike the available balance, the **pending balance is updated externally** (e.g., through incoming transfers). 
Since only the account owner knows the decryption key, it is not possible for senders to update some decryptable pending balance.

If the full 64-bit value were stored in a single ciphertext, decryption would become increasingly expensive, especially if many transfers are received. 
By splitting into two parts, we can **limit the ciphertext ranges** to keep decryption efficient.

To keep the pending balance decryptable while allowing many external writes, we store it as **two separate ElGamal ciphertexts**:

- `pending_balance_lo`: Encrypts the low 32 bits of the pending balance.
- `pending_balance_hi`: Encrypts the high bits of the pending balance, from bit 17 onwards.

The total pending balance is calculated by taking the sum of:
`pending_balance_lo` and `pending_balance_hi << 16` (`pending_balance_hi` shifted left by 16 bits.)

### Updating balances with Transfers
In confidential transfers, the amount being transferred is encrypted into two parts:

```protobuf
message MsgTransfer {
    ...
    Ciphertext from_amount_lo = 4;
    Ciphertext from_amount_hi = 5;
    Ciphertext to_amount_lo = 6;
    Ciphertext to_amount_hi = 7;
    ...
}
```

Upon execution, the transfer amount ciphertexts are aggregated into the receiver’s account:

```go
pending_balance_lo += to_amount_lo
pending_balance_hi += to_amount_hi
```

### Preventing Overflow

To avoid `pending_balance_lo` overflowing its encryption range, we impose structural limits:

```go
type Account struct {
	...

	PendingBalanceLo *elgamal.Ciphertext
	PendingBalanceHi *elgamal.Ciphertext
	PendingBalanceCreditCounter uint16
}
```

- `pending_balance_credit_counter` tracks how many incoming transfers the account has received since the last `ApplyPendingBalance`.
- This counter is capped at `2^16` to ensure that `pending_balance_lo` never exceeds 32 bits.

### Encryption Strategy

Each incoming transfer must follow these rules:
- The total transfer amount must be ≤ 48 bits.
- The transfer amount is split into:
  - A **16-bit** value encrypted into `encrypted_amount_lo_receiver`
  - A **32-bit** value encrypted into `encrypted_amount_hi_receiver`

This design ensures that after `2^16` incoming transfers:
- `pending_balance_lo` encrypts a value ≤ 32 bits — **fast to decrypt**
- `pending_balance_hi` encrypts a value ≤ 48 bits — **slower to decrypt**, but still manageable

### Summary

| Component                   | Bit Width | Max Encrypted Value After `2^16` Transfers |
|----------------------------|-----------|--------------------------------------------|
| `pending_balance_lo`       | 16 bits   | 32-bit ciphertext (fast to decrypt)        |
| `pending_balance_hi`       | 32 bits   | 48-bit ciphertext (slower to decrypt)      |

Clients with large token balances can mitigate decryption costs by **frequently applying the pending balance**, which resets the counter and avoids accumulating large ciphertexts.

This approach balances:
- Efficient ciphertext decryption for most users
- Scalability for high-volume accounts
- Security and usability guarantees in a decentralized system

## Appendix D: Twisted ElGamal Key Generation and Denom Binding

Each confidential account on Sei requires a **Twisted ElGamal public key** for encrypting balances. Although the protocol does **not enforce a specific key derivation method**, the initializer must prove knowledge of the corresponding private key when creating a confidential account.

### One Keypair per Denom

Each account must maintain a **unique encryption keypair for every denom**. This is because confidential balances are isolated per denom, and sharing a single key across denoms would:
- Weaken separation of balance states,
- Complicate zero-knowledge proof verification,
- And introduce potential privacy leakage across denoms.

### Recommended Key Derivation Scheme

To simplify secure key management for users, we recommend deriving the ElGamal keypair **directly from the user's Ethereum-compatible private key and the denom string**.

This offers several advantages:
1. **No extra keys to manage** – keys are deterministically derived per user per denom.
2. **Non-custodial derivation** – dApps can generate the keys on-the-fly by prompting users to sign a message, without needing access to their private key.

### How It Works

1. The user signs a **denom-bound string** like `ct:uatom` using their existing private key.
2. The signature is hashed and used to deterministically derive the ElGamal private key.
3. The ElGamal public key is computed using the fixed base point of the twisted curve.

#### Example Derivation Flow

```go
// Step 1: Sign the denom
signature := Sign("ct:<denom>", ethPrivateKey)

// Step 2: Hash the signature to derive deterministic seed bytes
seed := Keccak256("Ethereum Signed Message:\n...")

// Step 3: Use seed to derive the ElGamal keypair
elgamalKeyPair := twistedElGamal.KeyGen(seed)
```

This ensures that:
- The same user signing the same denom always derives the same encryption key.
- Only users with signing authority can initialize or manage their confidential balances.

You can refer to [keygen.go](x/confidentialtransfers/utils/keygen.go) for the implementation of the key generation process.
### AES Key Derivation for Decryptable Balances

We use a similar derivation process for generating AES keys used in `decryptable_available_balance`. This ensures consistency across clients:

```go
aesKey := DeriveAESKeyFromSignedDenom(userPrivateKey, denom)
```
---

### Design Summary

| Feature                       | Description                                                                 |
|------------------------------|-----------------------------------------------------------------------------|
| One keypair per denom        | Prevents cross-denom leakage and ensures proof isolation                   |
| Derived from user key + denom| Enables deterministic derivation without extra key storage                 |
| ETH-style message signing    | Allows dApps to derive user keys securely via signature prompts            |
| Shared standard              | Enables client and server libraries to interoperate seamlessly             |

By following this standard, client libraries can reliably encrypt, decrypt, and validate confidential balances while maintaining user-friendly and secure key management.

## Appendix E: Encryption Scheme

The Confidential Transfers module on Sei leverages both **public key encryption** and **symmetric encryption** to enable private, verifiable token transfers on-chain.

### 1. Twisted ElGamal: Homomorphic Public Key Encryption

For encrypting balances and amounts in transfer operations, the module uses a modified version of the ElGamal encryption scheme known as **Twisted ElGamal**.

Twisted ElGamal is a **homomorphic encryption scheme**, meaning it allows mathematical operations to be performed directly on ciphertexts without decrypting them. Specifically, it supports:
- **Addition/Subtraction** of encrypted balances
- **Scalar Multiplication** (e.g., encrypting multiples of a value)

This property is crucial for zero-knowledge proof systems and on-chain logic that needs to update balances while preserving privacy.

### How It Works

A Twisted ElGamal ciphertext consists of two components:
1. A **Pedersen commitment** to the encrypted value 
2. A **decryption handle** binding the encryption randomness to a specific public key

This structure makes Twisted ElGamal particularly suitable for zero-knowledge proof systems, many of which are already optimized for working with Pedersen commitments.

### Decryption Limitations and Design Considerations

ElGamal decryption becomes exponentially slower as the size of the encrypted message increases. On modern hardware:
- 32-bit ciphertexts can be decrypted in seconds
- 48-bit and 64-bit ciphertexts are **infeasible to brute-force**

Because of this, the module:
- Splits large values (e.g. pending balances) into **low and high ciphertexts**
- Limits the encrypted transfer amount to **48 bits**
- Requires regular application of pending balances to avoid ciphertext overflow

This design ensures the encryption remains usable, performant, and secure for both everyday and high-volume users.

---

