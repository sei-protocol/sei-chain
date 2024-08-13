const tokenFactoryDenoms = {
    "testnet": "factory/sei10xlj95ef20tczjrq5w5ah0vz8t5pxzeza4gaad/dapptest"
}

const cw20Addresses = {
    "testnet": {"evm": "0xcD10A4FdeE9CefB7732161f4B20b018bA3F4e7fF", "sei": "sei1d5cs4y0cfdm8dvak4fnmcudqd9htgsnw0djryuvpwhhmyvywypmsxv7vnq"}
}

const rpcUrls = {
    "testnet": "https://rpc-testnet.sei-apis.com",
    "devnet": "https://rpc-arctic-1.sei-apis.com"
}

const chainIds = {
    "testnet": "atlantic-2",
    "devnet": "arctic-1"
}

module.exports = {
    tokenFactoryDenoms,
    cw20Addresses,
    rpcUrls,
    chainIds
}