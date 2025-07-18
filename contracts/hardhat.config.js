require('dotenv').config({path:__dirname+'/.env'})
require("@nomicfoundation/hardhat-toolbox");
require('@openzeppelin/hardhat-upgrades');

/** @type import('hardhat/config').HardhatUserConfig */
module.exports = {
  solidity: {
    compilers: [
      {
        version: "0.8.25",
        settings: {
          evmVersion: "cancun",
          optimizer: {
            enabled: true,
            runs: 1000,
          },
        },
      },
      {
        version: "0.4.25",
        settings: {
          optimizer: {
            enabled: true,
            runs: 200,
          },
        },
      },
    ],
    overrides: {
      "legacy/Disperse.sol": {
        version: "0.4.25",
        settings: {
          optimizer: {
            enabled: true,
            runs: 200,
          },
        },
      },
    },
  },
  mocha: {
    timeout: 100000000,
  },
  paths: {
    sources: "./src", // contracts are in ./src
  },
  networks: {
    goerli: {
      url: "https://eth-goerli.g.alchemy.com/v2/NHwLuOObixEHj3aKD4LzN5y7l21bopga", // Replace with your JSON-RPC URL
      address: ["0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52"],
      accounts: ["0x57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e"], // Replace with your private key
    },
    seilocal: {
      url: "http://127.0.0.1:8545",
      address: ["0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52", "0x70997970C51812dc3A010C7d01b50e0d17dc79C8"],
      accounts: ["0x57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e", "0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d"],
    },
    devnet: {
      url: "https://evm-rpc.arctic-1.seinetwork.io/",
      address: ["0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52"],
      accounts: ["0x57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e", "0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d"],
    }
  },
};
