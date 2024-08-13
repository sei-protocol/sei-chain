const tokenFactoryDenoms = {
    "testnet": "factory/sei10xlj95ef20tczjrq5w5ah0vz8t5pxzeza4gaad/dapptest",
    "devnet": "factory/sei10xlj95ef20tczjrq5w5ah0vz8t5pxzeza4gaad/dapptest"
}

const cw20Addresses = {
    "testnet": {"evm": "0xcD10A4FdeE9CefB7732161f4B20b018bA3F4e7fF", "sei": "sei1d5cs4y0cfdm8dvak4fnmcudqd9htgsnw0djryuvpwhhmyvywypmsxv7vnq"},
    "devnet": {"evm": "0x2118f39DB4a32327523eA9ED6299EE5E2dee7b3d", "sei": "sei10609np8vn9udq3lg2rp3k2ymtzrxnq9dnezyag9ygklnnua4rqks89np9j"}
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