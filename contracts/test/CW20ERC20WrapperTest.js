const { expect } = require("chai");
const {isBigNumber} = require("hardhat/common");
const { exec } = require("child_process"); // Importing exec from child_process
const { cons } = require("fp-ts/lib/NonEmptyArray2v");

// Run instructions
// Should be run on a local chain using: `npx hardhat test --network seilocal test/CW20ERC20WrapperTest.js`

async function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function delay() {
  await sleep(3000)
}

function debug(msg) {
  //console.log(msg)
}

const CW20_BASE_WASM_LOCATION = "wasm/cw20_base.wasm";
const secondAnvilAddrETH = "0x70997970C51812dc3A010C7d01b50e0d17dc79C8";
const secondAnvilAddrSEI = "sei1cjzphr67dug28rw9ueewrqllmxlqe5f0awulvy";
const thirdAnvilAddrETH = "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC";
const thirdAnvilAddrSEI = "sei183zvmhdk4yq0526cthffncpaztay9yauk6y0ue"
const secondAnvilPk = "0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d"; // Replace with the spender's private key
const secondAnvilWallet = new ethers.Wallet(secondAnvilPk);
const secondAnvilSigner = secondAnvilWallet.connect(ethers.provider);

describe("CW20ERC20WrapperTest", function () {
    let adminAddrSei;
    let contractAddress;
    let deployerAddrETH;
    let deployerAddrSEI;
    let cW20ERC20Wrapper;

    before(async function () {
        // fund addresses with SEI
        console.log("funding addresses with SEI...")
        await fundwithSei(deployerAddrETH);
        await fundwithSei(secondAnvilAddrETH);
        await fundwithSei(thirdAnvilAddrETH);

        let signers = await hre.ethers.getSigners();
        const deployer = signers[0];
        deployerAddrETH = await deployer.getAddress();
        deployerAddrSEI = await getSeiAddr(deployerAddrETH);
        console.log("deployer address ETH = ", deployerAddrETH);
        console.log("deployer address SEI = ", deployerAddrSEI);

        console.log("deploying wasm...")
        let codeId = await deployWasm();
        console.log(`codeId: ${codeId}`);
        console.log("getting admin addr...")
        adminAddrSei = await getAdmin();
        console.log(`seid admin address: ${adminAddrSei}`);
        console.log("instantiating wasm...")
        contractAddress = await instantiateWasm(codeId, deployerAddrSEI);
        console.log(`CW20 Sei contract address: ${contractAddress}`)
        console.log(
            "Deploying contracts with the account:",
           deployerAddrETH 
        );

        await delay();
        const CW20ERC20Wrapper = await ethers.getContractFactory("CW20ERC20Wrapper");
        await delay();
        console.log("deploying cw20 erc20 wrapper...")
        cW20ERC20Wrapper = await CW20ERC20Wrapper.deploy(contractAddress, "BTOK", "TOK");
        await cW20ERC20Wrapper.waitForDeployment();
        console.log("CW20ERC20Wrapper address = ", cW20ERC20Wrapper.target)
    });

    describe("name", function () {
        it("name should work", async function () {
            const name = await cW20ERC20Wrapper.name();
            console.log(`Name: ${name}`);
            expect(name).to.equal("BTOK");
        });
    });

    describe("symbol", function () {
        it("symbol should work", async function () {
            const symbol = await cW20ERC20Wrapper.symbol();
            console.log(`Symbol: ${symbol}`);
            expect(symbol).to.equal("TOK"); // Replace "TOK" with the expected symbol
        });
    });

    describe("decimals", function () {
        it("decimals should work", async function () {
            const decimals = await cW20ERC20Wrapper.decimals();
            console.log(`Decimals: ${decimals}`);
            expect(Number(decimals)).to.be.greaterThan(0);
        });
    });

    describe("balanceOf", function () {
        it("balanceOf should work", async function () {
            let addressToCheck = secondAnvilAddrETH;
            console.log(`addressToCheck: ${addressToCheck}`);
            let secondAnvilAddrBalance = await cW20ERC20Wrapper.balanceOf(addressToCheck);
            console.log(`Balance of ${addressToCheck}: ${secondAnvilAddrBalance}`); // without this line the test fails more frequently
            expect(Number(secondAnvilAddrBalance)).to.be.greaterThan(0);
        });
    });

    describe("totalSupply", function () {
        it("totalSupply should work", async function () {
            let totalSupply = await cW20ERC20Wrapper.totalSupply();
            console.log(`Total supply: ${totalSupply}`);
            // expect total supply to be great than 0
            expect(Number(totalSupply)).to.be.greaterThan(0);
        });
    });

    describe("allowance", function () {
        it("increase allowance should work", async function () {
            let owner = deployerAddrETH; // Replace with the owner's address
            let spender = deployerAddrETH; // Replace with the spender's address
            let allowance = await cW20ERC20Wrapper.allowance(owner, spender);
            console.log(`Allowance for ${spender} from ${owner}: ${allowance}`);
            expect(Number(allowance)).to.equal(0); // Replace with the expected allowance
        });
    });

    describe("approve", function () {
        it("increasing approval should work", async function () {
            let spender = secondAnvilAddrETH;
            let amount = 1000000; // Replace with the amount to approve
            const tx = await cW20ERC20Wrapper.approve(spender, amount);
            await tx.wait();
            const allowance = await cW20ERC20Wrapper.allowance(deployerAddrETH, spender);
            console.log(`Allowance for ${spender} from ${deployerAddrETH}: ${allowance}`);
            expect(Number(allowance)).to.equal(amount);
        });

        it("decreasing approval should work", async function () {
            let spender = secondAnvilAddrETH;
            let amount = 10; // Replace with the amount to approve

            // check that current allowance is greater than amount
            const currentAllowance = await cW20ERC20Wrapper.allowance(deployerAddrETH, spender);
            expect(Number(currentAllowance)).to.be.greaterThan(amount);

            // decrease allowance
            const tx = await cW20ERC20Wrapper.approve(spender, amount);
            await tx.wait();
            const allowance = await cW20ERC20Wrapper.allowance(deployerAddrETH, spender);
            console.log(`Allowance for ${spender} from ${deployerAddrETH}: ${allowance}`);
            expect(Number(allowance)).to.equal(amount);
        });
    });

    describe("transfer", function () {
        it("transfer should work", async function () {
            let recipient = secondAnvilAddrETH;
            let amount = 8; // Replace with the amount to transfer

            // check that balanceOf sender address has enough ERC20s to send
            let balanceOfDeployer = await cW20ERC20Wrapper.balanceOf(deployerAddrETH);
            expect(Number(balanceOfDeployer)).to.be.greaterThan(amount);
            console.log("transfer: deployerAddr balance = ", balanceOfDeployer);

            // capture recipient balance before the transfer
            let balanceOfRecipientBefore = await cW20ERC20Wrapper.balanceOf(recipient);
            console.log("transfer: recipient balance before = ", balanceOfRecipientBefore);

            // do the transfer
            const tx = await cW20ERC20Wrapper.transfer(recipient, amount);
            await tx.wait();

            // compare recipient balance before and after the transfer
            let balanceOfRecipientAfter = await cW20ERC20Wrapper.balanceOf(recipient);
            let diff = balanceOfRecipientAfter - balanceOfRecipientBefore;
            expect(diff).to.equal(amount);
        });

        it("transfer should fail if sender has insufficient balance", async function () {
            const balanceOfDeployer = await cW20ERC20Wrapper.balanceOf(deployerAddrETH);

            const recipient = secondAnvilAddrETH;
            const amount = balanceOfDeployer + BigInt(1); // This should be more than the sender's balance

            await expect(cW20ERC20Wrapper.transfer(recipient, amount)).to.be.revertedWith("CosmWasm execute failed");
        });
    });

    describe("transferFrom", function () {
        it("transferFrom should work", async function () {
            const amountToTransfer = 10;
            const spender = secondAnvilAddrETH;
            const recipient = thirdAnvilAddrETH;
            // check balanceOf deployer
            console.log("transferFrom: checking balanceOf deployer...")
            const balanceOfDeployer = await cW20ERC20Wrapper.balanceOf(deployerAddrETH);
            expect(Number(balanceOfDeployer)).to.be.greaterThanOrEqual(amountToTransfer);

            // give allowance of deployer to spender (third party)
            console.log("transferFrom: doing approve...")
            const tx = await cW20ERC20Wrapper.approve(spender, amountToTransfer);
            await tx.wait();

            // check allownce of deployer to spender
            console.log("transferFrom: checking allowance...")
            const allowanceBefore = await cW20ERC20Wrapper.allowance(deployerAddrETH, spender);
            expect(Number(allowanceBefore)).to.be.greaterThanOrEqual(amountToTransfer);

            // check that spender has gas
            console.log("transferFrom: checking spender has gas...")
            const spenderGas = await ethers.provider.getBalance(spender);
            expect(Number(spenderGas)).to.be.greaterThan(0);
            console.log("transferFrom: spender gas = ", spenderGas)

            // capture recipient balance before transfer
            console.log("transferFrom: checking balanceOf recipient before transfer...")
            const balanceOfRecipientBefore = await cW20ERC20Wrapper.balanceOf(recipient);

            // check balanceOf sender (deployerAddr) to ensure it went down
            const balanceOfSenderBefore = await cW20ERC20Wrapper.balanceOf(deployerAddrETH);

            // have deployer transferFrom spender to recipient
            console.log("transferFrom: doing actual transferFrom...")
            const tfTx = await cW20ERC20Wrapper.connect(secondAnvilSigner).transferFrom(deployerAddrETH, recipient, amountToTransfer);
            await tfTx.wait();

            // check balance diff to ensure transfer went through
            console.log("transferFrom: checking balanceOf recipient after transfer...")
            const balanceOfRecipientAfter = await cW20ERC20Wrapper.balanceOf(recipient);
            const diff = balanceOfRecipientAfter - balanceOfRecipientBefore;
            expect(diff).to.equal(amountToTransfer);

            // check balanceOf sender (deployerAddr) to ensure it went down
            const balanceOfSenderAfter = await cW20ERC20Wrapper.balanceOf(deployerAddrETH);
            const diff2 = balanceOfSenderBefore - balanceOfSenderAfter;
            expect(diff2).to.equal(amountToTransfer);

            // check that allowance has gone down by amountToTransfer
            const allowanceAfter = await cW20ERC20Wrapper.allowance(deployerAddrETH, spender);
            expect(Number(allowanceBefore) - Number(allowanceAfter)).to.equal(amountToTransfer);
        });

        it("transferFrom should fail if sender has insufficient balance", async function() {
            const fromAddrBalance = await cW20ERC20Wrapper.balanceOf(deployerAddrETH);
            const spender = secondAnvilAddrETH;
            const recipient = thirdAnvilAddrETH;

            // try to transfer more than the balance
            const amountToTransfer = fromAddrBalance + BigInt(1);

            const tx = await cW20ERC20Wrapper.approve(spender, amountToTransfer);
            await tx.wait();

            await expect(cW20ERC20Wrapper.connect(secondAnvilSigner).transferFrom(deployerAddrETH, recipient, amountToTransfer))
                .to.be.revertedWith("CosmWasm execute failed");
        });

        it("transferFrom should fail if proper allowance not given", async function () {
            const fromAddrBalance = await cW20ERC20Wrapper.balanceOf(deployerAddrETH);
            const spender = secondAnvilAddrETH;
            const recipient = thirdAnvilAddrETH;

            const amountToTransfer = 1

            // set approval to 0
            const tx = await cW20ERC20Wrapper.approve(spender, 0);
            await tx.wait();

            await expect(cW20ERC20Wrapper.connect(secondAnvilSigner).transferFrom(deployerAddrETH, recipient, amountToTransfer))
                .to.be.revertedWith("CosmWasm execute failed");
        });

    });
});

async function getSeiAddr(ethAddr) {
    let seiAddr = await new Promise((resolve, reject) => {
        exec(`seid q evm sei-addr ${ethAddr}`, (error, stdout, stderr) => {
            if (error) {
                console.log(`error: ${error.message}`);
                reject(error);
                return;
            }
            if (stderr) {
                console.log(`stderr: ${stderr}`);
                reject(new Error(stderr));
                return;
            }
            debug(`stdout: ${stdout}`)
            resolve(stdout.trim());
        });
    });
    return seiAddr.replace("sei_address: ", "");;
}

async function fundwithSei(deployerAddress) {
    // Wrap the exec function in a Promise
    await new Promise((resolve, reject) => {
        exec(`seid tx evm send ${deployerAddress} 10000000000000000000 --from admin`, (error, stdout, stderr) => {
            if (error) {
                console.log(`error: ${error.message}`);
                reject(error);
                return;
            }
            if (stderr) {
                console.log(`stderr: ${stderr}`);
                reject(new Error(stderr));
                return;
            }
            debug(`stdout: ${stdout}`)
            resolve();
        });
    });
}

async function deployWasm() {
    // Wrap the exec function in a Promise
    let codeId = await new Promise((resolve, reject) => {
        exec(`seid tx wasm store ${CW20_BASE_WASM_LOCATION} --from admin --gas=5000000 --fees=1000000usei -y --broadcast-mode block`, (error, stdout, stderr) => {
            if (error) {
                console.log(`error: ${error.message}`);
                reject(error);
                return;
            }
            if (stderr) {
                console.log(`stderr: ${stderr}`);
                reject(new Error(stderr));
                return;
            }
            debug(`stdout: ${stdout}`)

            // Regular expression to find the 'code_id' value
            const regex = /key: code_id\s+value: "(\d+)"/;

            // Searching for the pattern in the string
            const match = stdout.match(regex);

            let cId = null;
            if (match && match[1]) {
                // The captured group is the code_id value
                cId = match[1];
            }

            console.log(`cId: ${cId}`);
            resolve(cId);
        });
    });

    return codeId;
}

async function getAdmin() {
    // Wrap the exec function in a Promise
    let adminAddr = await new Promise((resolve, reject) => {
        exec(`seid keys show admin -a`, (error, stdout, stderr) => {
            if (error) {
                console.log(`error: ${error.message}`);
                reject(error);
                return;
            }
            if (stderr) {
                console.log(`stderr: ${stderr}`);
                reject(new Error(stderr));
                return;
            }
            debug(`stdout: ${stdout}`)
            resolve(stdout.trim());
        });
    });
    return adminAddr;
}

async function instantiateWasm(codeId, adminAddr) {
    // Wrap the exec function in a Promise
    console.log("instantiateWasm: will fund admin addr = ", adminAddr);
    console.log("instantiateWasm: will fund secondAnvilAddr = ", secondAnvilAddrSEI);
    let contractAddress = await new Promise((resolve, reject) => {
        const cmd = `seid tx wasm instantiate ${codeId} '{ "name": "BTOK", "symbol": "BTOK", "decimals": 6, "initial_balances": [ { "address": "${adminAddr}", "amount": "1000000" }, { "address": "${secondAnvilAddrSEI}", "amount": "1000000"} ], "mint": { "minter": "${adminAddr}", "cap": "99900000000" } }' --label cw20-test --admin ${adminAddr} --from admin --gas=5000000 --fees=1000000usei -y --broadcast-mode block`;
        exec(cmd, (error, stdout, stderr) => {
            if (error) {
                console.log(`error: ${error.message}`);
                reject(error);
                return;
            }
            if (stderr) {
                console.log(`stderr: ${stderr}`);
                reject(new Error(stderr));
                return;
            }
            debug(`stdout: ${stdout}`)
            const regex = /_contract_address\s*value:\s*(\w+)/;
            const match = stdout.match(regex);
            if (match && match[1]) {
                resolve(match[1]);
            } else {
                reject(new Error('Contract address not found'));
            }
        });
    });
    return contractAddress;
}
