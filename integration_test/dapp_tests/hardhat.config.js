require("@nomiclabs/hardhat-waffle");
require("@nomiclabs/hardhat-ethers");

/** @type import('hardhat/config').HardhatUserConfig */
module.exports = {
  solidity: {
    version: "0.8.20",
    settings: {
      optimizer: {
        enabled: true,
        runs: 1000,
      },
    },
  },
  mocha: {
    timeout: 100000000,
  },
  networks: {
    seilocal: {
      // The in-process runner sets SEI_EVM_RPC to the node's dynamic EVM port; under docker
      // it's unset, so the container's fixed :8545 stands.
      url: process.env.SEI_EVM_RPC || "http://127.0.0.1:8545",
      accounts: {
        mnemonic: process.env.DAPP_TESTS_MNEMONIC,
        path: "m/44'/118'/0'/0/0",
        initialIndex: 0,
        count: 1
      },
    },
    testnet: {
      url: "https://evm-rpc-testnet.sei-apis.com",
      accounts: {
        mnemonic: process.env.DAPP_TESTS_MNEMONIC,
        path: "m/44'/118'/0'/0/0",
        initialIndex: 0,
        count: 1
      },
    },
    devnet: {
      url: "https://evm-rpc-arctic-1.sei-apis.com",
      accounts: {
        mnemonic: process.env.DAPP_TESTS_MNEMONIC,
        path: "m/44'/118'/0'/0/0",
        initialIndex: 0,
        count: 1
      },
    },
  },
};
