const { expect } = require("chai");
const hre = require("hardhat");

const {sendFunds, deployEthersContract, estimateAndCall, setupAccountWithMnemonic} = require("../utils");
const { fundAddress, execute } = require("../../../contracts/test/lib.js");
const {chainIds, rpcUrls} = require("../constants");

const testChain = process.env.DAPP_TEST_ENV;
console.log("testChain", testChain);
describe("NFT Marketplace", function () {

    let marketplace, deployer, erc721token, originalSeidConfig;

    before(async function () {
        const accounts = hre.config.networks[testChain].accounts
        const deployerWallet = hre.ethers.Wallet.fromMnemonic(accounts.mnemonic, accounts.path);
        deployer = deployerWallet.connect(hre.ethers.provider);

        const seidConfig = await execute('seid config');
        originalSeidConfig = JSON.parse(seidConfig);

        if (testChain === 'seilocal') {
            await fundAddress(deployer.address, amount="2000000000000000000000");
        }  else {
            // Set default seid config to the specified rpc url.
            await execute(`seid config chain-id ${chainIds[testChain]}`)
            await execute(`seid config node ${rpcUrls[testChain]}`)
        }

        await execute(`seid config keyring-backend test`)

        await sendFunds('0.01', deployer.address, deployer)
        await setupAccountWithMnemonic("dapptest", accounts.mnemonic, deployer);

        // Deploy MockNFT
        const erc721ContractArtifact = await hre.artifacts.readArtifact("MockERC721");
        erc721token = await deployEthersContract("MockERC721", erc721ContractArtifact.abi, erc721ContractArtifact.bytecode, deployer, ["MockERC721", "MKTNFT"])

        const numNftsToMint = 10
        await estimateAndCall(erc721token, "batchMint", [deployer.address, numNftsToMint]);

        const nftMarketplaceArtifact = await hre.artifacts.readArtifact("NftMarketplace");
        marketplace = await deployEthersContract("NftMarketplace", nftMarketplaceArtifact.abi, nftMarketplaceArtifact.bytecode, deployer)
    })

    describe("Orders", function () {
        async function testNFTMarketplaceOrder(buyer, seller, nftContract) {
            // Refers to the first token owned by the deployer.
            const tokenId = await nftContract.tokenOfOwnerByIndex(deployer.address, 0);

            if (seller.address !== deployer.address) {
                // Send one NFT to the seller so they can list it.
                await estimateAndCall(nftContract, "transferFrom", [deployer.address, seller.address, tokenId]);

                let nftOwner = await nftContract.ownerOf(tokenId);

                // Seller should have the token here
                expect(nftOwner).to.equal(seller.address, "NFT should have been transferred to the seller");
            }

            const sellerNftbalance = await nftContract.balanceOf(seller.address);
            // Deployer should have at least one token here.
            expect(Number(sellerNftbalance)).to.be.greaterThanOrEqual(1, "Seller must have at least 1 NFT remaining")

            // List the NFT on the marketplace contract.
            const nftPrice = hre.ethers.utils.parseEther("0.1");
            await estimateAndCall(nftContract.connect(seller), "setApprovalForAll", [marketplace.address, true])
            await estimateAndCall(marketplace.connect(seller), "listItem", [nftContract.address, tokenId, nftPrice])

            // Confirm that the NFT was listed.
            const listing = await marketplace.getListing(nftContract.address, tokenId);
            expect(listing.price).to.equal(nftPrice, "Listing price should be correct");
            expect(listing.seller).to.equal(seller.address, "Listing seller should be correct");

            // Buyer purchases the NFT from the marketplace contract.
            await estimateAndCall(marketplace.connect(buyer), "buyItem", [nftContract.address, tokenId], nftPrice);

            const newSellerNftbalance = await nftContract.balanceOf(seller.address);
            expect(Number(newSellerNftbalance)).to.be.lessThan(Number(sellerNftbalance), "NFT should have been transferred from the seller.")

            nftOwner = await nftContract.ownerOf(tokenId);
            expect(nftOwner).to.equal(buyer.address, "NFT should have been transferred to the buyer.");
        }

        it("Should allow listing and buying erc721 by associated users", async function () {
            // Create and fund buyer account
            const buyerWallet = hre.ethers.Wallet.createRandom();
            const buyer = buyerWallet.connect(hre.ethers.provider);
            await sendFunds("0.5", buyer.address, deployer)

            await testNFTMarketplaceOrder(buyer, deployer, erc721token)
        });

        it("Should allow listing and buying erc721 by unassociated users", async function () {
            // Create and fund seller account
            const sellerWallet = hre.ethers.Wallet.createRandom();
            const seller = sellerWallet.connect(hre.ethers.provider);
            await sendFunds("0.5", seller.address, deployer)

            // Create and fund buyer account
            const buyerWallet = hre.ethers.Wallet.createRandom();
            const buyer = buyerWallet.connect(hre.ethers.provider);
            await sendFunds("0.5", buyer.address, deployer)

            await testNFTMarketplaceOrder(buyer, seller, erc721token);
        });
    })

    after(async function () {
        // Set the chain back to regular state
        console.log("Resetting")
        await execute(`seid config chain-id ${originalSeidConfig["chain-id"]}`)
        await execute(`seid config node ${originalSeidConfig["node"]}`)
        await execute(`seid config keyring-backend ${originalSeidConfig["keyring-backend"]}`)
    })
})
