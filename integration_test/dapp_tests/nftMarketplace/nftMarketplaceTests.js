const { expect } = require("chai");
const hre = require("hardhat");

const {
    sendFunds, estimateAndCall, mintCw721, deployAndReturnContractsForNftTests,
    queryLatestNftIds, setDaemonConfig
} = require("../utils");
const { getSeiAddress, execute } = require("../../../contracts/test/lib.js");

const testChain = process.env.DAPP_TEST_ENV;
const isFastTrackEnabled = process.env.IS_FAST_TRACK;

describe("NFT Marketplace", function () {
    console.log('NFT Tests are being run');
    let marketplace, deployer, erc721token, erc721PointerToken, cw721Address, originalSeidConfig, nftId1;

    before(async function () {
        const accounts = hre.config.networks[testChain].accounts
        const deployerWallet = hre.ethers.Wallet.fromMnemonic(accounts.mnemonic, accounts.path);
        deployer = deployerWallet.connect(hre.ethers.provider);
        originalSeidConfig = await setDaemonConfig(testChain);

        ({
            marketplace,
            erc721token,
            cw721Address,
            erc721PointerToken
        } = await deployAndReturnContractsForNftTests(deployer, testChain, accounts, isFastTrackEnabled));

        const deployerSeiAddr = await getSeiAddress(deployer.address);
        nftId1 = (await queryLatestNftIds(cw721Address, testChain)) + 1;
        const numCwNftsToMint = 2;

        for (let i = nftId1; i <= nftId1 + numCwNftsToMint; i++) {
            await mintCw721(cw721Address, deployerSeiAddr, i);
            console.log('nfts minted');
        }
    })

    describe("Orders", function () {
        async function testNFTMarketplaceOrder(buyer, seller, nftContract, nftId = "", expectTransferFail = false) {
            let tokenId;
            // If nftId is manually supplied (for pointer contract), ensure that deployer owns that token.
            if (nftId) {
                const nftOwner = await nftContract.ownerOf(nftId);
                expect(nftOwner).to.equal(deployer.address);
                tokenId = nftId;
            } else {
                // Refers to the first token owned by the deployer.
                tokenId = await nftContract.tokenOfOwnerByIndex(deployer.address, 0);
            }

            if (seller.address !== deployer.address) {
                if (expectTransferFail) {
                    // Transfer to unassociated address should fail if seller is not associated.
                    expect(nftContract.transferFrom(deployer.address, seller.address, tokenId)).to.be.reverted;

                    // Associate the seller from here.
                    await sendFunds("0.01", seller.address, seller);
                }

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
            if (expectTransferFail) {
                // We expect a revert here if the buyer address is not associated, since pointer tokens cant be transferred to buyer.
                expect(marketplace.connect(buyer).buyItem(nftContract.address, tokenId, {value: nftPrice})).to.be.reverted;

                // Associate buyer here.
                await sendFunds('0.01', buyer.address, buyer);
            }
            await estimateAndCall(marketplace.connect(buyer), "buyItem", [nftContract.address, tokenId], nftPrice);

            const newSellerNftbalance = await nftContract.balanceOf(seller.address);
            expect(Number(newSellerNftbalance)).to.be.lessThan(Number(sellerNftbalance), "NFT should have been transferred from the seller.")

            let nftOwner = await nftContract.ownerOf(tokenId);
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

        it("Should allow listing and buying erc721 pointer by associated users", async function () {
            // Create and fund buyer account
            const buyerWallet = hre.ethers.Wallet.createRandom();
            const buyer = buyerWallet.connect(hre.ethers.provider);
            await sendFunds("0.5", buyer.address, deployer)
            await sendFunds('0.01', buyer.address, buyer)
            await testNFTMarketplaceOrder(buyer, deployer, erc721PointerToken, `${nftId1}`);
        });

        it("Currently does not allow listing or buying erc721 pointer by unassociated users", async function () {
            // Create and fund seller account
            const sellerWallet = hre.ethers.Wallet.createRandom();
            const seller = sellerWallet.connect(hre.ethers.provider);
            await sendFunds("0.5", seller.address, deployer)

            // Create and fund buyer account
            const buyerWallet = hre.ethers.Wallet.createRandom();
            const buyer = buyerWallet.connect(hre.ethers.provider);
            await sendFunds("0.5", buyer.address, deployer)

            await testNFTMarketplaceOrder(buyer, seller, erc721PointerToken, `${nftId1 + 1}`, true);
        });
    })

    after(async function () {
        // Set the chain back to regular state
        console.log("Resetting");
        await execute(`seid config chain-id ${originalSeidConfig["chain-id"]}`)
        await execute(`seid config node ${originalSeidConfig["node"]}`)
        await execute(`seid config keyring-backend ${originalSeidConfig["keyring-backend"]}`)
    })
})
