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
      url: "http://127.0.0.1:8545",
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
