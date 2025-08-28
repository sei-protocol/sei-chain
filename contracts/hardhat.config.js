require('dotenv').config({path:__dirname+'/.env'})
require("@nomicfoundation/hardhat-toolbox");
require('@openzeppelin/hardhat-upgrades');

/** @type import('hardhat/config').HardhatUserConfig */
module.exports = {
  solidity: {
    version: "0.8.28",
    settings: {
      evmVersion: "prague",
      optimizer: {
        enabled: true,
        runs: 1000,
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
      address: ["0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52", "0x70997970C51812dc3A010C7d01b50e0d17dc79C8","0x817E1414b633948e50101Df0b722DeA5f8C29109"],
      accounts: ["0x57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e", "0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d","0x888432482e2cbcf4e2b248a388f2a6d9fe7b59a11e9136fd615942d7421e89bf"],
    },
    devnet: {
      url: "https://evm-rpc.arctic-1.seinetwork.io/",
      address: ["0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52"],
      accounts: ["0x57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e", "0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d"],
    }
  },
};
