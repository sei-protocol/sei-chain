const { expect } = require("chai");
const {isBigNumber} = require("hardhat/common");
const {uniq, shuffle} = require("lodash");
const { ethers, upgrades } = require('hardhat');
const { getImplementationAddress } = require('@openzeppelin/upgrades-core');
const { deployEvmContract, setupSigners, fundAddress, getCosmosTx} = require("./lib")
const axios = require("axios");
const { parseEther } = require("ethers");

describe("My test", function() {
    let accounts;
    // send money from account 0 to account 2
    it.only("should send money from account 0 to account 2", async function() {
        accounts = await ethers.getSigners();
        // accounts = await hre.ethers.getSigners()
        console.log("accounts", accounts)
        const account0 = accounts[0];
        console.log("account0", account0.address);
        const account2 = accounts[2];
        console.log("account2", account2.address);
        const balanceBefore = await ethers.provider.getBalance(account2.address);
        if (balanceBefore < 2100000000000000) {
            const tx = await account0.sendTransaction({
                to: account2.address,
                value: 2100000000000000,
            });
            await tx.wait();
        }
        const balanceAfter = await ethers.provider.getBalance(account2.address);
        console.log("balanceBefore", balanceBefore.toString());
        console.log("balanceAfter", balanceAfter.toString());

        const privateKey = "0xc53c11e1e8f58038548d0f6f0a5d87791a1b9e5fb4fc847b263cee9d34f40b2e";
        const wallet = new ethers.Wallet(privateKey, ethers.provider);
        const nonce_ = await ethers.provider.getTransactionCount(account2.address)
        console.log("nonce", nonce_)
        const tx = {
            to: account0.address,
            value: parseEther("0.1"),
            gasLimit: 21000,
            gasPrice: 10000000000,
            nonce: nonce_,
            chainId: 713715,
        };

        console.log("account2 = ", account2)

        const signedTx = await wallet.signTransaction(tx);

        // get block number
        const blockNumber = await ethers.provider.getBlockNumber();
        console.log("blockNumber = ", blockNumber);

        // try to send a tx out from account2
        const nodeUrl = 'https://evm-rpc.arctic-1.seinetwork.io/';
        const response = await axios.post(nodeUrl, {
            method: 'eth_sendRawTransaction',
            params: [signedTx],
            id: 1,
            jsonrpc: "2.0"
        })

        console.log("receipt", response);

        console.log("tx hash = ", ethers.keccak256(signedTx))
    });
});
