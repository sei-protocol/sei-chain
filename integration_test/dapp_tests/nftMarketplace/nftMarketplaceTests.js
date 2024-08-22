const { expect } = require("chai");
const hre = require("hardhat");

const {sendFunds, deployEthersContract, estimateAndCall, deployCw721WithPointer, setupAccountWithMnemonic,
    mintCw721
} = require("../utils");
const { fundAddress, getSeiAddress, execute } = require("../../../contracts/test/lib.js");
const {evmRpcUrls, chainIds, rpcUrls} = require("../constants");

const testChain = process.env.DAPP_TEST_ENV;
console.log("testChain", testChain);
describe("NFT Marketplace", function () {

    let marketplace, deployer, erc721token, erc721PointerToken, cw721Address, originalSeidConfig;

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

        const numNftsToMint = 50
        await estimateAndCall(erc721token, "batchMint", [deployer.address, numNftsToMint]);

        // Deploy CW721 token with ERC721 pointer
        const time = Date.now().toString();
        const deployerSeiAddr = await getSeiAddress(deployer.address);
        const cw721Details = await deployCw721WithPointer(deployerSeiAddr, deployer, time, evmRpcUrls[testChain])
        erc721PointerToken = cw721Details.pointerContract;
        cw721Address = cw721Details.cw721Address;
        console.log("CW721 Address", cw721Address);
        const numCwNftsToMint = 2;
        for (let i = 1; i <= numCwNftsToMint; i++) {
            await mintCw721(cw721Address, deployerSeiAddr, i)
        }
        const cwbal = await erc721PointerToken.balanceOf(deployer.address);
        expect(cwbal).to.equal(numCwNftsToMint)

        const nftMarketplaceArtifact = await hre.artifacts.readArtifact("NftMarketplace");
        marketplace = await deployEthersContract("NftMarketplace", nftMarketplaceArtifact.abi, nftMarketplaceArtifact.bytecode, deployer)
    })

    describe("Orders", function () {
        async function testNFTMarketplaceOrder(buyer, seller, nftContract, nftId="", expectTransferFail=false) {
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

            nftOwner = await nftContract.ownerOf(tokenId);
            expect(nftOwner).to.equal(buyer.address, "NFT should have been transferred to the buyer.");
        }

        it("Should allow listing and buying erc721 by associated users", async function () {
            // Create and fund buyer account
            const buyerWallet = hre.ethers.Wallet.createRandom();
            const buyer = buyerWallet.connect(hre.ethers.provider);
            await sendFunds("1", buyer.address, deployer)

            await testNFTMarketplaceOrder(buyer, deployer, erc721token)
        });

        it("Should allow listing and buying erc721 by unassociated users", async function () {
            // Create and fund seller account
            const sellerWallet = hre.ethers.Wallet.createRandom();
            const seller = sellerWallet.connect(hre.ethers.provider);
            await sendFunds("1", seller.address, deployer)

            // Create and fund buyer account
            const buyerWallet = hre.ethers.Wallet.createRandom();
            const buyer = buyerWallet.connect(hre.ethers.provider);
            await sendFunds("1", buyer.address, deployer)

            await testNFTMarketplaceOrder(buyer, seller, erc721token);
        });

        it("Should allow listing and buying erc721 pointer by associated users", async function () {
            // Create and fund buyer account
            const buyerWallet = hre.ethers.Wallet.createRandom();
            const buyer = buyerWallet.connect(hre.ethers.provider);
            await sendFunds("1", buyer.address, deployer)
            await sendFunds('0.01', buyer.address, buyer)
            await testNFTMarketplaceOrder(buyer, deployer, erc721PointerToken, '1');
        });

        it("Currently does not allow listing or buying erc721 pointer by unassociated users", async function () {
            // Create and fund seller account
            const sellerWallet = hre.ethers.Wallet.createRandom();
            const seller = sellerWallet.connect(hre.ethers.provider);
            await sendFunds("1", seller.address, deployer)

            // Create and fund buyer account
            const buyerWallet = hre.ethers.Wallet.createRandom();
            const buyer = buyerWallet.connect(hre.ethers.provider);
            await sendFunds("1", buyer.address, deployer)

            await testNFTMarketplaceOrder(buyer, seller, erc721PointerToken, '2', true);
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
