const { setupSigners, fundAddress} = require("./lib.js");
const { createWalletClient, http, parseEther, defineChain } = require('viem');
const { privateKeyToAccount } = require('viem/accounts');
const { ethers } = require("hardhat");
const { expect } = require("chai");

const seilocal = defineChain({
  id: 713714,
  name: 'Sei',
  nativeCurrency: {
    decimals: 12,
    name: 'usei',
    symbol: 'usei',
  },
  rpcUrls: {
    default: {
      http: ['http://localhost:8545'],
      webSocket: ['ws://localhost:8546'],
    },
  },
});

function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

describe("EIP-7702 Transaction Test", function () {
  let account;
  let walletClient

  before(async function(){
    await setupSigners(await ethers.getSigners());

    account = privateKeyToAccount('0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80');
    walletClient = createWalletClient({
      account,
      chain: seilocal,
      transport: http()
    });
    await fundAddress(await account.address);
    await sleep(3000);
  });

  it("sends a transaction with temporary code (EIP-7702)", async function () {
    const bscf = await ethers.getContractFactory("BatchCallAndSponsor")
    const bsc = await bscf.deploy();
    await bsc.waitForDeployment();
    let contractAddress = await bsc.getAddress();
      const authorization = await walletClient.signAuthorization({
        contractAddress,
      });
      const hash = await walletClient.sendTransaction({
        authorizationList: [authorization],
        data: bsc.interface.encodeFunctionData('execute',
          [
            [
              {
                data: '0x',
                to: '0xcb98643b8786950F0461f3B0edf99D88F274574D',
                value: parseEther('0.001'),
              },
              {
                data: '0x',
                to: '0xd2135CfB216b74109775236E36d4b433F1DF507B',
                value: parseEther('0.002'),
              },
            ],
          ]
        ),
        to: walletClient.account.address,
      });
      await sleep(3000);
      const receipt = await ethers.provider.getTransactionReceipt(hash);
      expect(receipt.status).to.equal(1);
  });
});