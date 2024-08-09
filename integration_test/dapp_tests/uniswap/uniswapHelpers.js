const hre = require("hardhat");
const {BigNumber} = require("ethers"); // Require Hardhat Runtime Environment
const { ABI, deployErc20PointerForCw20, deployWasm, execute } = require("../../../contracts/test/lib.js");

async function deployTokenPool(managerContract, firstTokenAddr, secondTokenAddr, swapRatio=1, fee=3000) {
    const sqrtPriceX96 = BigInt(Math.sqrt(swapRatio) * (2 ** 96)); // Initial price (1:1)

    const gasPrice = await hre.ethers.provider.getGasPrice();
    const [token0, token1] = tokenOrder(firstTokenAddr, secondTokenAddr);

    let gasLimit = await managerContract.estimateGas.createAndInitializePoolIfNecessary(
        token0.address,
        token1.address,
        fee,
        sqrtPriceX96,
    );

    gasLimit = gasLimit.mul(12).div(10)
    console.log("EST GAS", gasLimit)
    // token0 addr must be < token1 addr
    const poolTx = await managerContract.createAndInitializePoolIfNecessary(
        token0.address,
        token1.address,
        fee,
        sqrtPriceX96,
        {gasLimit, gasPrice}
    );
    await poolTx.wait();
    console.log("Pool created and initialized");
}

// Supplies liquidity to then given pools. The signer connected to the contracts must have the prerequisite tokens or this will fail.
async function supplyLiquidity(managerContract, recipientAddr, firstTokenContract, secondTokenContract, firstTokenAmt=100, secondTokenAmt=100) {
    // Define the amount of tokens to be approved and added as liquidity
    console.log("Supplying liquidity to pool")
    const [token0, token1] = tokenOrder(firstTokenContract.address, secondTokenContract.address, firstTokenAmt, secondTokenAmt);
    const gasPrice = await hre.ethers.provider.getGasPrice();

    let gasLimit = await firstTokenContract.estimateGas.approve(managerContract.address, firstTokenAmt);
    gasLimit = gasLimit.mul(12).div(10)

    // Approve the NonfungiblePositionManager to spend the specified amount of firstToken
    const approveFirstTokenTx = await firstTokenContract.approve(managerContract.address, firstTokenAmt, {gasLimit, gasPrice});
    await approveFirstTokenTx.wait();

    gasLimit = await secondTokenContract.estimateGas.approve(managerContract.address, secondTokenAmt);
    gasLimit = gasLimit.mul(12).div(10)

    // Approve the NonfungiblePositionManager to spend the specified amount of secondToken
    const approveSecondTokenTx = await secondTokenContract.approve(managerContract.address, secondTokenAmt, {gasLimit, gasPrice});
    await approveSecondTokenTx.wait();


    gasLimit = await managerContract.estimateGas.mint({
        token0: token0.address,
        token1: token1.address,
        fee: 3000, // Fee tier (0.3%)
        tickLower: -887220,
        tickUpper: 887220,
        amount0Desired: token0.amount,
        amount1Desired: token1.amount,
        amount0Min: 0,
        amount1Min: 0,
        recipient: recipientAddr,
        deadline: Math.floor(Date.now() / 1000) + 60 * 10, // 10 minutes from now
    })

    gasLimit = gasLimit.mul(12).div(10)
    console.log("Liquidity Gas Limit", gasLimit);

    // Add liquidity to the pool
    const liquidityTx = await managerContract.mint({
        token0: token0.address,
        token1: token1.address,
        fee: 3000, // Fee tier (0.3%)
        tickLower: -887220,
        tickUpper: 887220,
        amount0Desired: token0.amount,
        amount1Desired: token1.amount,
        amount0Min: 0,
        amount1Min: 0,
        recipient: recipientAddr,
        deadline: Math.floor(Date.now() / 1000) + 60 * 10, // 10 minutes from now
    }, {gasLimit, gasPrice});

    await liquidityTx.wait();
    console.log("Liquidity added");
}

// Orders the 2 addresses sequentially, since this is required by uniswap.
function tokenOrder(firstTokenAddr, secondTokenAddr, firstTokenAmount=0, secondTokenAmount=0) {
    let token0;
    let token1;
    if (parseInt(firstTokenAddr, 16) < parseInt(secondTokenAddr, 16)) {
        token0= {address: firstTokenAddr, amount: firstTokenAmount};
        token1 = {address: secondTokenAddr, amount: secondTokenAmount};
    } else {
        token0 = {address: secondTokenAddr, amount: secondTokenAmount};
        token1 = {address: firstTokenAddr, amount: firstTokenAmount};
    }
    return [token0, token1]
}

async function deployCw20WithPointer(deployerSeiAddr, signer, time) {
    const cw20Address = await deployWasm('../dapp_tests/uniswap/cw20_base.wasm', deployerSeiAddr, "cw20", {
        name: `testCw20${time}`,
        symbol: "TEST",
        decimals: 6,
        initial_balances: [
            { address: deployerSeiAddr, amount: "1000000000" }
        ],
        mint: {
            "minter": deployerSeiAddr, "cap": "99900000000"
        }
    });

    const pointerAddr = await deployErc20PointerForCw20(hre.ethers.provider, cw20Address);
    const pointerContract = new hre.ethers.Contract(pointerAddr, ABI.ERC20, signer);
    return {"pointerContract": pointerContract, "cw20Address": cw20Address}
}

async function deployEthersContract(name, abi, bytecode, deployer, deployParams=[]) {
    const contract = new hre.ethers.ContractFactory(abi, bytecode, deployer);
    const deployed = await contract.deploy(...deployParams);
    await deployed.deployed();
    console.log(`${name} deployed to:`, deployed.address);
    return deployed;
}

async function addDeployerKey(keyName, mnemonic, path) {
    try {
        const output = await execute(`seid keys show ${keyName} --output json`);
        const parsed = JSON.parse(output);
        console.log("OUTOPUT", parsed);
        return parsed.address;
    } catch (e) {
        console.log(e)
    }

    try {
        await execute(`printf "${mnemonic}" | seid keys add ${keyName} --recover --hd-path "${path}" --keyring-backend test`)
    }
    catch (e) {
        console.log(e);
    }

    const output = await execute(`seid keys show ${keyName} --output json`);
    const parsed = JSON.parse(output);

    return parsed.address;
}

async function doesTokenFactoryDenomExist(denom) {
    const output = await execute(`seid q tokenfactory denom-authority-metadata ${denom} --output json`);
    const parsed = JSON.parse(output);

    return parsed.authority_metadata.admin !== "";
}

async function sendFunds(amountSei, recipient, signer) {
    const fundUser= await signer.sendTransaction({
        to: recipient,
        value: hre.ethers.utils.parseEther(amountSei)
    })

    await fundUser.wait();
}

module.exports = {
    deployTokenPool,
    supplyLiquidity,
    deployCw20WithPointer,
    deployEthersContract,
    addDeployerKey,
    doesTokenFactoryDenomExist,
    sendFunds
}