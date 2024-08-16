const { expect } = require("chai");
const hre = require("hardhat");
const { abi: seaportAbi, bytecode: seaportBytecode }  = require('@opensea/seaport-js/src/artifacts/seaport/contracts/Seaport.sol/Seaport.json')
const { abi: conduitControllerAbi, bytecode: conduitControllerBytecode } = require('@opensea/seaport-js/src/artifacts/seaport-core/src/conduit/ConduitController.sol/ConduitController.json');
const { abi: erc721Abi, bytecode: erc721Bytecode } = require('@openzeppelin/contracts/build/contracts/ERC721.json');
const {deployEthersContract, sendFunds, estimateAndCall} = require("../utils");

const testChain = process.env.DAPP_TEST_ENV;

describe("Seaport", function () {
    let seaport, conduitController, deployer, erc20token, erc20supply, erc721token, user;

    before(async function () {
        const accounts = hre.config.networks[testChain].accounts
        const deployerWallet = hre.ethers.Wallet.fromMnemonic(accounts.mnemonic, accounts.path);
        deployer = deployerWallet.connect(hre.ethers.provider);

        // Fund user account
        const userWallet = hre.ethers.Wallet.createRandom();
        user = userWallet.connect(hre.ethers.provider);
        await sendFunds("5", user.address, deployer)

        // Deploy MockToken
        console.log("Deploying MockToken with the account:", deployer.address);
        erc20Supply = hre.ethers.utils.parseEther("1000");
        const erc20ContractArtifact = await hre.artifacts.readArtifact("MockERC20");
        erc20token = await deployEthersContract("MockERC20", erc20ContractArtifact.abi, erc20ContractArtifact.bytecode, deployer, ["MockERC20", "MKT", erc20Supply])

        const erc721ContractArtifact = await hre.artifacts.readArtifact("MockERC721");
        erc721token = await deployEthersContract("MockERC721", erc721ContractArtifact.abi, erc721ContractArtifact.bytecode, deployer, ["MockERC721", "MKTNFT"])

        // Mint 1 NFT
        await estimateAndCall(erc721token, "mint", [deployer.address]);
        // Deploy the core seaport contracts.
        conduitController = await deployEthersContract("ConduitController", conduitControllerAbi, conduitControllerBytecode, deployer)
        seaport = await deployEthersContract("Seaport", seaportAbi, seaportBytecode, deployer,[conduitController.address])
    });

    it("Should deploy Seaport with the correct ConduitController address", async function () {
        // Check that the ConduitController address is set correctly in Seaport
        const info = await seaport.information();
        console.log(info);

        expect(info.conduitController).to.equal(conduitController.address);
    });

    it("Deployer should have all the NFTs", async function () {
        const balance = await erc721token.balanceOf(deployer.address);
        console.log("Balances", balance);

        expect(Number(balance)).to.be.greaterThan(0);
    });

    describe("Orders", function () {
        it("Should allow associated user to create and fulfill an order", async function () {
            // Example: Deployer creates an order to sell a token to User
            const nftCost = hre.ethers.utils.parseEther("10");
            const nftId = 1;
            const order = {
                offerer: deployer.address,
                zone: hre.ethers.constants.AddressZero,  // No specific zone
                offer: [
                    {
                        itemType: 2,  // 2 indicates ERC721
                        token: erc721token.address,
                        identifierOrCriteria: nftId,  // NFT ID
                        startAmount: 1,
                        endAmount: 1
                    }
                ],
                consideration: [
                    {
                        itemType: 1,  // 1 indicates ERC20
                        token: erc20token.address,
                        identifierOrCriteria: 0,
                        startAmount: nftCost,
                        endAmount: nftCost,
                        recipient: deployer.address
                    }
                ],
                startTime: 0,  // Order can be executed immediately
                endTime: Math.floor(Date.now() / 1000) + 86400,  // Expires in 1 day
                orderType: 0,  // Full Open Order
                zoneHash: hre.ethers.constants.HashZero,
                salt: hre.ethers.utils.randomBytes(32),
                conduitKey: hre.ethers.constants.HashZero,
                totalOriginalConsiderationItems: 1
            };

            console.log("Approving?")
            // Approve the sale of the NFT by deployer.
            await estimateAndCall(erc721token, "setApprovalForAll", [seaport.address, true])
            console.log("Approved")
            // Sign the order
            const orderHash = await seaport.getOrderHash(order);
            console.log("OrderHash", orderHash)
            const signature = await deployer.signMessage(hre.ethers.utils.arrayify(orderHash));
            const signedOrder = {
                ...order,
                signature
            };

            console.log("signed", signedOrder);
            // Submit the order to Seaport (this would typically involve more complex logic)
            const tx = await seaport.createOrder(signedOrder);
            await tx.wait();
            console.log("Order submitted")

            // Send user some erc20 tokens
            await erc20token.transfer(user.address, nftCost)
            let deployerBal = erc20token.balanceOf(deployer.address);
            let expectedAmount = erc20Supply - nftCost;
            expect(deployerBal).to.equal(Number())

            let userBal = erc20token.balanceOf(user.address);
            expect(userBal).to.equal(nftCost);

            // Approve Seaport to transfer erc20 tokens on behalf of user
            await erc20token.connect(user).approve(seaport.address, nftCost);
            const allowance = await erc20token.allowance(user.address, seaport.address);
            expect(allowance).to.equal(nftCost.toString(), "erc20 allowance for user should be equal to value passed in")

            // Fulfill the order
            const fulfillTx = await seaport.connect(user).fulfillOrder(order);
            await fulfillTx.wait();

            // Check balances or ownership changes
            expect(await erc721token.ownerOf(1)).to.equal(user.address);
            expect(await erc20token.balanceOf(deployer.address)).to.equal(erc20supply);
        });
    })
});