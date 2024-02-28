const { execSync } = require('child_process');
const { expect } = require("chai");

describe("EVM Test", function () {
    describe("EVM Precompile Tester", function () {
        describe("EVM Bank Precompile Tester", function () {
            let contractAddress;
            let erc20;
            let owner;
            let owner2;
            const setupScriptPath = './test/deploy_atom_erc20.sh';
            before(async function() {
                contractAddress = runSetupScript(setupScriptPath, 'ERC20_DEPLOY_ADDR');
                await sleep(1000);
    
                // TODO: create a contract object
                // Create a signer
                const [signer, signer2] = await ethers.getSigners();
                owner = await signer.getAddress();
                owner2 = await signer2.getAddress();
    
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
    
                // transfer from owner to owner2
                const balanceBefore = await erc20.balanceOf(receiver);
                const transferFromTx = await erc20.transferFrom(owner, receiver, 100, {from: owner2});
    
                console.log("transferFromTx = ", transferFromTx);
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

        describe("EVM Gov Precompile Tester", function () {
            let govProposal;
            const setupScriptPath = './test/send_gov_deposit.sh';
            before(async function() {
                govProposal = runSetupScript(setupScriptPath, 'GOV_PROPOSAL_ID');
                await sleep(1000);
    
                // Create a proposal
                const [signer, _] = await ethers.getSigners();
                owner = await signer.getAddress();
    
                // contractArtifact.abi
                const contractABI = require('../precompiles/gov/abi.json')
                // Get a contract instance
                gov = new ethers.Contract(contractAddress, contractABI, signer);
                console.log("end of before");
            });
    
            it("Gov deposit", async function () {
                const deposit = await gov.deposit(govProposal, 100000)
                expect(deposit).to.equal(true);
            });
        });

        describe("EVM Distribution Precompile Tester", function () {
            before(async function() {
                const [signer, signer2] = await ethers.getSigners();
                owner = await signer.getAddress();
                owner2 = await signer2.getAddress();

                // contractArtifact.abi
                const contractABI = require('../precompiles/distribution/abi.json')
                // Get a contract instance
                distribution = new ethers.Contract(contractAddress, contractABI, signer);
                console.log("end of before");
            });

            it("Distribution set withdraw address", async function () {
                const setWithdraw = await distribution.setWithdrawAddress(owner2)
                expect(setWithdraw).to.equal(true);
            });
        });

        describe("EVM Staking Precompile Tester", function () {
            const setupScriptPath = './test/get_validator_address.sh';

            before(async function() {
                validatorAddr = runSetupScript(setupScriptPath, 'VALIDATOR_ADDR');
                await sleep(1000);
                const [signer, _] = await ethers.getSigners();
                owner = await signer.getAddress();

                // contractArtifact.abi
                const contractABI = require('../precompiles/staking/abi.json')
                // Get a contract instance
                staking = new ethers.Contract(contractAddress, contractABI, signer);
                console.log("end of before");
            });

            it("Staking delegate", async function () {
                const delegate = await staking.delegate(validatorAddr, 100)
                expect(delegate).to.equal(true);
            });
        });
    });
});

function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

function runSetupScript(setupScriptPath, variableName) {
    let scriptOutput;
    try {
        const output = execSync('bash ' + setupScriptPath).toString();

        // Parse the output to find the contract address
        console.log("output:", output);
        const regexPattern = new RegExp(variableName + '=(\\S+)');
        const match = output.match(regexPattern);
        scriptOutput = match ? match[1] : undefined;

        if (scriptOutput === undefined) {
            console.error("script output not found");
        } else {
            console.log("EVMPrecompileTester setup complete with output:", scriptOutput);
        }
    } catch (error) {
        console.error(`Error executing bash script: ${error}`);
    }
    return scriptOutput;
}