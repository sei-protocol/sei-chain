const { execSync } = require('child_process');
const { expect } = require("chai");
const fs = require('fs');
const path = require('path');

describe("EVM Test", function () {
    describe("EVM Precompile Tester", function () {
        describe("EVM Bank Precompile Tester", function () {
            let contractAddress;
            let erc20;
            let owner;
            let owner2;
            let signer;
            let signer2
            before(async function() {
                contractAddress = readDeploymentOutput('erc20_deploy_addr.txt');
                await sleep(1000);
    
                // Create a signer
                [signer, signer2] = await ethers.getSigners();
                owner = await signer.getAddress();
                owner2 = await signer2.getAddress();
    
                // TODO: create a contract object
                // contractArtifact.abi
                const contractABI = [{"type":"constructor","inputs":[{"name":"denom_","type":"string","internalType":"string"},{"name":"name_","type":"string","internalType":"string"},{"name":"symbol_","type":"string","internalType":"string"},{"name":"decimals_","type":"uint8","internalType":"uint8"}],"stateMutability":"nonpayable"},{"type":"function","name":"BankPrecompile","inputs":[],"outputs":[{"name":"","type":"address","internalType":"contract IBank"}],"stateMutability":"view"},{"type":"function","name":"allowance","inputs":[{"name":"owner","type":"address","internalType":"address"},{"name":"spender","type":"address","internalType":"address"}],"outputs":[{"name":"","type":"uint256","internalType":"uint256"}],"stateMutability":"view"},{"type":"function","name":"approve","inputs":[{"name":"spender","type":"address","internalType":"address"},{"name":"value","type":"uint256","internalType":"uint256"}],"outputs":[{"name":"","type":"bool","internalType":"bool"}],"stateMutability":"nonpayable"},{"type":"function","name":"balanceOf","inputs":[{"name":"account","type":"address","internalType":"address"}],"outputs":[{"name":"","type":"uint256","internalType":"uint256"}],"stateMutability":"view"},{"type":"function","name":"ddecimals","inputs":[],"outputs":[{"name":"","type":"uint8","internalType":"uint8"}],"stateMutability":"view"},{"type":"function","name":"decimals","inputs":[],"outputs":[{"name":"","type":"uint8","internalType":"uint8"}],"stateMutability":"view"},{"type":"function","name":"denom","inputs":[],"outputs":[{"name":"","type":"string","internalType":"string"}],"stateMutability":"view"},{"type":"function","name":"name","inputs":[],"outputs":[{"name":"","type":"string","internalType":"string"}],"stateMutability":"view"},{"type":"function","name":"nname","inputs":[],"outputs":[{"name":"","type":"string","internalType":"string"}],"stateMutability":"view"},{"type":"function","name":"ssymbol","inputs":[],"outputs":[{"name":"","type":"string","internalType":"string"}],"stateMutability":"view"},{"type":"function","name":"symbol","inputs":[],"outputs":[{"name":"","type":"string","internalType":"string"}],"stateMutability":"view"},{"type":"function","name":"totalSupply","inputs":[],"outputs":[{"name":"","type":"uint256","internalType":"uint256"}],"stateMutability":"view"},{"type":"function","name":"transfer","inputs":[{"name":"to","type":"address","internalType":"address"},{"name":"value","type":"uint256","internalType":"uint256"}],"outputs":[{"name":"","type":"bool","internalType":"bool"}],"stateMutability":"nonpayable"},{"type":"function","name":"transferFrom","inputs":[{"name":"from","type":"address","internalType":"address"},{"name":"to","type":"address","internalType":"address"},{"name":"value","type":"uint256","internalType":"uint256"}],"outputs":[{"name":"","type":"bool","internalType":"bool"}],"stateMutability":"nonpayable"},{"type":"event","name":"Approval","inputs":[{"name":"owner","type":"address","indexed":true,"internalType":"address"},{"name":"spender","type":"address","indexed":true,"internalType":"address"},{"name":"value","type":"uint256","indexed":false,"internalType":"uint256"}],"anonymous":false},{"type":"event","name":"Transfer","inputs":[{"name":"from","type":"address","indexed":true,"internalType":"address"},{"name":"to","type":"address","indexed":true,"internalType":"address"},{"name":"value","type":"uint256","indexed":false,"internalType":"uint256"}],"anonymous":false},{"type":"error","name":"ERC20InsufficientAllowance","inputs":[{"name":"spender","type":"address","internalType":"address"},{"name":"allowance","type":"uint256","internalType":"uint256"},{"name":"needed","type":"uint256","internalType":"uint256"}]},{"type":"error","name":"ERC20InsufficientBalance","inputs":[{"name":"sender","type":"address","internalType":"address"},{"name":"balance","type":"uint256","internalType":"uint256"},{"name":"needed","type":"uint256","internalType":"uint256"}]},{"type":"error","name":"ERC20InvalidApprover","inputs":[{"name":"approver","type":"address","internalType":"address"}]},{"type":"error","name":"ERC20InvalidReceiver","inputs":[{"name":"receiver","type":"address","internalType":"address"}]},{"type":"error","name":"ERC20InvalidSender","inputs":[{"name":"sender","type":"address","internalType":"address"}]},{"type":"error","name":"ERC20InvalidSpender","inputs":[{"name":"spender","type":"address","internalType":"address"}]}];
    
                // Get a contract instance
                erc20 = new ethers.Contract(contractAddress, contractABI, signer);
                console.log("end of before");
            });
    
            it("Transfer function", async function() {
                const receiver = '0x70997970C51812dc3A010C7d01b50e0d17dc79C8';
                const beforeBalance = await erc20.balanceOf(owner);
                const tx = await erc20.transfer(receiver, 1);
                const receipt = await tx.wait();
                expect(receipt.status).to.equal(1);
                const afterBalance = await erc20.balanceOf(owner);
                const diff = beforeBalance - afterBalance;
                expect(diff).to.equal(1);
            });
    
            it("Approve and TransferFrom functions", async function() {
                const receiver = '0x70997970C51812dc3A010C7d01b50e0d17dc79C8';
                // lets have owner approve the transfer and have owner2 do the transferring
                const approvalAmount = await erc20.allowance(owner, owner2);
                expect(approvalAmount).to.equal(0);
                const approveTx = await erc20.approve(owner2, 100);
                const approveReceipt = await approveTx.wait();
                expect(approveReceipt.status).to.equal(1);
                expect(await erc20.allowance(owner, owner2)).to.equal(100);

                const erc20AsOwner2 = erc20.connect(signer2); 

    
                // transfer from owner to owner2
                const balanceBefore = await erc20.balanceOf(receiver);
                const transferFromTx = await erc20AsOwner2.transferFrom(owner, receiver, 100);
    
                // await sleep(3000);
                const transferFromReceipt = await transferFromTx.wait();
                expect(transferFromReceipt.status).to.equal(1);
                const balanceAfter = await erc20.balanceOf(receiver);
                const diff = balanceAfter - balanceBefore;
                expect(diff).to.equal(100);
            });
    
            it("Balance of function", async function() {
                const balance = await erc20.balanceOf(owner);
                expect(balance).to.be.greaterThan(Number(0));
            });
    
            it("Name function", async function () {
                const name = await erc20.name()
                expect(name).to.equal('UATOM');
            });
    
            it("Symbol function", async function () {
                const symbol = await erc20.symbol()
                // expect symbol to be 'UATOM'
                expect(symbol).to.equal('UATOM');
            });
        });

        // TODO: Update when we add gov query precompiles
        describe("EVM Gov Precompile Tester", function () {
            let govProposal;
            // TODO: Import this
            const GovPrecompileContract = '0x0000000000000000000000000000000000001006';
            before(async function() {
                govProposal = readDeploymentOutput('gov_proposal_output.txt');
                await sleep(1000);
    
                // Create a proposal
                const [signer, _] = await ethers.getSigners();
                owner = await signer.getAddress();
    
                // contractArtifact.abi
                const contractABIPath = path.join(__dirname, '../../precompiles/gov/abi.json');
                const contractABI = require(contractABIPath);
                // Get a contract instance
                gov = new ethers.Contract(GovPrecompileContract, contractABI, signer);
                console.log("end of before");
            });
    
            it("Gov deposit", async function () {
                const depositAmount = ethers.parseEther('0.01');
                const deposit = await gov.deposit(govProposal, {
                    value: depositAmount,
                })
                const receipt = await deposit.wait();
                expect(receipt.status).to.equal(1);
                // TODO: Add gov query precompile here
            });
        });

        // TODO: Update when we add distribution query precompiles
        describe("EVM Distribution Precompile Tester", function () {
            // TODO: Import this
            const DistributionPrecompileContract = '0x0000000000000000000000000000000000001007';
            before(async function() {
                const [signer, signer2] = await ethers.getSigners();
                owner = await signer.getAddress();
                owner2 = await signer2.getAddress();

                // contractArtifact.abi
                const contractABIPath = path.join(__dirname, '../../precompiles/distribution/abi.json');
                const contractABI = require(contractABIPath);
                // Get a contract instance
                distribution = new ethers.Contract(DistributionPrecompileContract, contractABI, signer);
                console.log("end of before");
            });

            it("Distribution set withdraw address", async function () {
                const setWithdraw = await distribution.setWithdrawAddress(owner)
                const receipt = await setWithdraw.wait();
                expect(receipt.status).to.equal(1);
                // TODO: Add distribution query precompile here
            });
        });

        // TODO: Update when we add staking query precompiles
        describe("EVM Staking Precompile Tester", function () {
            const StakingPrecompileContract = '0x0000000000000000000000000000000000001005';
            before(async function() {
                validatorAddr = readDeploymentOutput('validator_address.txt');
                await sleep(1000);
                const [signer, _] = await ethers.getSigners();
                owner = await signer.getAddress();

                // contractArtifact.abi
                const contractABIPath = path.join(__dirname, '../../precompiles/staking/abi.json');
                const contractABI = require(contractABIPath);
                // Get a contract instance
                staking = new ethers.Contract(StakingPrecompileContract, contractABI, signer);
                console.log("end of before");
            });

            it("Staking delegate", async function () {
                const delegateAmount = ethers.parseEther('0.01');
                const delegate = await staking.delegate(validatorAddr, {
                    value: delegateAmount,
                });
                const receipt = await delegate.wait();
                expect(receipt.status).to.equal(1);
                // TODO: Add staking query precompile here
            });
        });
    });
});

function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

function readDeploymentOutput(fileName) {
    let fileContent;
    try {
        if (fs.existsSync(fileName)) {
            fileContent = fs.readFileSync(fileName, 'utf8').trim();
            console.log("Output from file:", fileContent);
        } else {
            console.error("File not found:", fileName);
        }
    } catch (error) {
        console.error(`Error reading file: ${error}`);
    }
    return fileContent;
}
