const { expect } = require("chai");
const hre = require("hardhat");
const { ethers } = hre;
const {
    setupSigners,
    fundAddress,
    getSeiAddress,
    bankSend,
    delay,
    generateWallet,
    rawHttpDebugTraceWithCallTracer
} = require("./lib");

describe("FundForwarder", function () {
    let owner;
    let accounts;
    let destinationWallet;
    let fundForwarder;

    before(async function () {
        // Setup signers with funded accounts
        accounts = await setupSigners(await ethers.getSigners());
        owner = accounts[0].signer;

        // Create a random wallet as the destination
        destinationWallet = generateWallet();
    });

    describe("Deployment", function () {
        it("Should deploy with the correct destination address", async function () {
            const FundForwarder = await ethers.getContractFactory("FundForwarder");
            const contract = await FundForwarder.deploy(destinationWallet.address);
            await contract.waitForDeployment();

            expect(await contract.destinationAddress()).to.equal(destinationWallet.address);
        });

        it("Should reject zero address as destination", async function () {
            const FundForwarder = await ethers.getContractFactory("FundForwarder");
            await expect(
                FundForwarder.deploy(ethers.ZeroAddress)
            ).to.be.revertedWith("Destination cannot be zero address");
        });
    });

    describe("Fund Forwarding via Cosmos Bank Send", function () {
        let contractSeiAddress;
        const sendAmount = "10000000000"; // 10,000 SEI in usei (10^10 usei = 10,000 SEI)

        before(async function () {
            // Deploy a fresh FundForwarder contract
            const FundForwarder = await ethers.getContractFactory("FundForwarder");
            fundForwarder = await FundForwarder.deploy(destinationWallet.address);
            await fundForwarder.waitForDeployment();

            // Get the Sei address associated with the contract's EVM address
            const contractEvmAddress = await fundForwarder.getAddress();
            contractSeiAddress = await getSeiAddress(contractEvmAddress);

            console.log("Contract EVM Address:", contractEvmAddress);
            console.log("Contract Sei Address:", contractSeiAddress);
            console.log("Destination Address:", destinationWallet.address);
        });

        it("Should receive funds via Cosmos bank send", async function () {
            // Check initial balance
            const initialBalance = await fundForwarder.getBalance();
            expect(initialBalance).to.equal(0n);

            // Send funds to the contract using Cosmos bank send
            await bankSend(contractSeiAddress, "admin", sendAmount, "usei");
            await delay();

            // Verify the contract received the funds
            // Note: 1 SEI = 10^18 wei, so 10^10 usei = 10^10 * 10^12 = 10^22 wei
            const newBalance = await fundForwarder.getBalance();
            expect(newBalance).to.be.gt(0n);

            console.log("Contract balance after Cosmos send:", ethers.formatEther(newBalance), "SEI");
        });

        it("Should forward all funds to destination when SendFunds is called", async function () {
            // Get balances before
            const contractBalanceBefore = await fundForwarder.getBalance();
            const destinationBalanceBefore = await ethers.provider.getBalance(destinationWallet.address);

            // Anyone can call SendFunds - using owner here but could be any account
            const tx = await fundForwarder.connect(owner).SendFunds();
            const receipt = await tx.wait();

            console.log("SendFunds tx hash:", receipt.hash);
            console.log("Amount sent:", ethers.formatEther(contractBalanceBefore), "SEI");
            console.log("Destination:", destinationWallet.address);

            // Debug trace to verify internal transaction
            const traceResult = await rawHttpDebugTraceWithCallTracer(receipt.hash);
            console.log("\nDebug trace result:");
            console.log(JSON.stringify(traceResult.result, null, 2));

            // Verify the internal call exists in the trace
            expect(traceResult.result).to.not.be.undefined;
            expect(traceResult.result.type).to.equal("CALL");

            // The trace should show an internal call from the contract to the destination
            const contractAddress = await fundForwarder.getAddress();
            if (traceResult.result.calls && traceResult.result.calls.length > 0) {
                const internalCall = traceResult.result.calls[0];
                console.log("\nInternal transaction detected:");
                console.log("  Type:", internalCall.type);
                console.log("  From:", internalCall.from);
                console.log("  To:", internalCall.to);
                console.log("  Value:", internalCall.value);

                expect(internalCall.from.toLowerCase()).to.equal(contractAddress.toLowerCase());
                expect(internalCall.to.toLowerCase()).to.equal(destinationWallet.address.toLowerCase());
                expect(BigInt(internalCall.value)).to.equal(contractBalanceBefore);
            } else {
                // Some tracers include the value transfer in the top-level call
                console.log("\nValue transfer in top-level call:");
                console.log("  From:", traceResult.result.from);
                console.log("  To:", traceResult.result.to);
                console.log("  Value:", traceResult.result.value);
            }

            // Verify the FundsSent event was emitted
            const event = receipt.logs.find(
                log => log.fragment && log.fragment.name === "FundsSent"
            );
            expect(event).to.not.be.undefined;

            // Get balances after
            const contractBalanceAfter = await fundForwarder.getBalance();
            const destinationBalanceAfter = await ethers.provider.getBalance(destinationWallet.address);

            // Contract should now have zero balance
            expect(contractBalanceAfter).to.equal(0n);

            // Destination should have received the funds
            expect(destinationBalanceAfter).to.equal(destinationBalanceBefore + contractBalanceBefore);
        });

        it("Should fail SendFunds when contract has no balance", async function () {
            // Contract should have zero balance after previous test
            const balance = await fundForwarder.getBalance();
            expect(balance).to.equal(0n);

            // SendFunds should revert
            await expect(
                fundForwarder.SendFunds()
            ).to.be.revertedWith("No funds to send");
        });
    });

    describe("Permissionless SendFunds", function () {
        let newContract;
        let thirdPartyWallet;
        let newDestination;

        before(async function () {
            // Create new wallets
            newDestination = generateWallet();
            thirdPartyWallet = generateWallet();

            // Fund the third party wallet so they can pay for gas
            await fundAddress(thirdPartyWallet.address);
            await delay();

            // Deploy a new contract
            const FundForwarder = await ethers.getContractFactory("FundForwarder");
            newContract = await FundForwarder.deploy(newDestination.address);
            await newContract.waitForDeployment();

            // Fund the contract via Cosmos bank send
            const contractSeiAddress = await getSeiAddress(await newContract.getAddress());
            await bankSend(contractSeiAddress, "admin", "5000000000", "usei");
            await delay();
        });

        it("Should allow a third party to call SendFunds", async function () {
            const destinationBalanceBefore = await ethers.provider.getBalance(newDestination.address);
            const contractBalance = await newContract.getBalance();

            // Third party (not owner, not destination) calls SendFunds
            const tx = await newContract.connect(thirdPartyWallet).SendFunds();
            const receipt = await tx.wait();

            console.log("SendFunds tx hash:", receipt.hash);
            console.log("Amount sent:", ethers.formatEther(contractBalance), "SEI");
            console.log("Caller (third party):", thirdPartyWallet.address);
            console.log("Destination:", newDestination.address);

            // Debug trace to verify internal transaction
            const traceResult = await rawHttpDebugTraceWithCallTracer(receipt.hash);
            const contractAddress = await newContract.getAddress();

            if (traceResult.result.calls && traceResult.result.calls.length > 0) {
                const internalCall = traceResult.result.calls[0];
                console.log("\nInternal transaction detected:");
                console.log("  Type:", internalCall.type);
                console.log("  From:", internalCall.from);
                console.log("  To:", internalCall.to);
                console.log("  Value:", internalCall.value);

                expect(internalCall.from.toLowerCase()).to.equal(contractAddress.toLowerCase());
                expect(internalCall.to.toLowerCase()).to.equal(newDestination.address.toLowerCase());
                expect(BigInt(internalCall.value)).to.equal(contractBalance);
            }

            const destinationBalanceAfter = await ethers.provider.getBalance(newDestination.address);
            const contractBalanceAfter = await newContract.getBalance();

            // Contract should be empty
            expect(contractBalanceAfter).to.equal(0n);

            // Destination received the funds
            expect(destinationBalanceAfter).to.equal(destinationBalanceBefore + contractBalance);
        });
    });

    describe("Direct EVM Transfers Rejected", function () {
        let contract;

        before(async function () {
            const dest = generateWallet();

            const FundForwarder = await ethers.getContractFactory("FundForwarder");
            contract = await FundForwarder.deploy(dest.address);
            await contract.waitForDeployment();
        });

        it("Should reject direct EVM transfers (no receive/fallback)", async function () {
            const sendAmount = ethers.parseEther("1.0");
            const contractAddress = await contract.getAddress();

            // Direct EVM transfer to the contract should fail
            await expect(
                owner.sendTransaction({
                    to: contractAddress,
                    value: sendAmount
                })
            ).to.be.reverted;
        });
    });
});
