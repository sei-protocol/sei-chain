# KVStore

There is one app here: the KVStoreApplication.

## KVStoreApplication

The KVStoreApplication is a simple merkle key-value store.
Transactions of the form `key=value` are stored as key-value pairs in the tree.
Transactions without an `=` sign set the value to the key.
The app has no replay protection (other than what the mempool provides).
