const rpcUrls = {
    "seilocal": "http://127.0.0.1:26657",
    "testnet": "https://rpc-testnet.sei-apis.com",
    "devnet": "https://rpc-arctic-1.sei-apis.com",
    "seiCluster": "http://127.0.0.1:26657"
}

const evmRpcUrls = {
    "seilocal": "http://127.0.0.1:8545",
    "testnet": "https://evm-rpc-testnet.sei-apis.com",
    "devnet": "https://evm-rpc-arctic-1.sei-apis.com",
    "seiCluster": "http://127.0.0.1:8545"
}

const chainIds = {
    "seilocal": "sei-chain",
    "testnet": "atlantic-2",
    "devnet": "arctic-1",
    "seiCluster":"sei-chain",
}

module.exports = {
    rpcUrls,
    evmRpcUrls,
    chainIds
}