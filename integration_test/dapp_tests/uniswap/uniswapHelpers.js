const hre = require("hardhat");
const { ABI, deployErc20PointerForCw20, deployWasm, execute, delay } = require("../../../contracts/test/lib.js");
const path = require('path')

console.log("PWD", process.cwd())
console.log("dirname", __dirname)
const CW20_BASE_PATH = path.resolve(__dirname, '../uniswap/cw20_base.wasm')
const CW20_BASE_PATH = '../integration_test/dapp_tests/uniswap/cw20_base.wasm'
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
    let allowance = await firstTokenContract.allowance(recipientAddr, managerContract.address);
    let balance = await firstTokenContract.balanceOf(recipientAddr);

    gasLimit = await secondTokenContract.estimateGas.approve(managerContract.address, secondTokenAmt);
    gasLimit = gasLimit.mul(12).div(10)

    // Approve the NonfungiblePositionManager to spend the specified amount of secondToken
    const approveSecondTokenTx = await secondTokenContract.approve(managerContract.address, secondTokenAmt, {gasLimit, gasPrice});
    await approveSecondTokenTx.wait();

    allowance = await secondTokenContract.allowance(recipientAddr, managerContract.address);
    balance = await secondTokenContract.balanceOf(recipientAddr);

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

async function deployCw20WithPointer(deployerSeiAddr, signer, time, evmRpc="") {
    const cw20Address = await deployWasm(CW20_BASE_PATH, deployerSeiAddr, "cw20", {
        name: `testCw20${time}`,
        symbol: "TEST",
        decimals: 6,
        initial_balances: [
            { address: deployerSeiAddr, amount: hre.ethers.utils.parseEther("1000000").toString() }
        ],
        mint: {
            "minter": deployerSeiAddr, "cap": hre.ethers.utils.parseEther("10000000").toString()
        }
    }, deployerSeiAddr);
    const pointerAddr = await deployErc20PointerForCw20(hre.ethers.provider, cw20Address, 10, deployerSeiAddr, evmRpc);
    const pointerContract = new hre.ethers.Contract(pointerAddr, ABI.ERC20, signer);
    return {"pointerContract": pointerContract, "cw20Address": cw20Address}
}

async function deployEthersContract(name, abi, bytecode, deployer, deployParams=[]) {
    const contract = new hre.ethers.ContractFactory(abi, bytecode, deployer);
    const deployTx = contract.getDeployTransaction(...deployParams);
    const gasEstimate = await deployer.estimateGas(deployTx);
    const gasPrice = await deployer.getGasPrice();
    const deployed = await contract.deploy(...deployParams, {gasPrice, gasLimit: gasEstimate});
    await deployed.deployed();
    console.log(`${name} deployed to:`, deployed.address);
    return deployed;
}

async function doesTokenFactoryDenomExist(denom) {
    const output = await execute(`seid q tokenfactory denom-authority-metadata ${denom} --output json`);
    const parsed = JSON.parse(output);

    return parsed.authority_metadata.admin !== "";
}

async function sendFunds(amountSei, recipient, signer) {
    const gasLimit = await signer.estimateGas({
        to: recipient,
        value: hre.ethers.utils.parseEther(amountSei)
    })

    // Get current gas price from the network
    const gasPrice = await signer.getGasPrice();

    const fundUser = await signer.sendTransaction({
        to: recipient,
        value: hre.ethers.utils.parseEther(amountSei),
        gasLimit: gasLimit.mul(12).div(10),
        gasPrice: gasPrice,
    })

    await fundUser.wait();
}

async function pollBalance(erc20Contract, address, criteria, maxAttempts=3) {
    let bal = 0;
    let attempt = 1;
    while (attempt === 1 || attempt <= maxAttempts) {
        bal = await erc20Contract.balanceOf(address);
        attempt++;
        if (criteria(bal)) {
            return bal;
        }
        await delay();
    }

    return bal;
}

module.exports = {
    deployTokenPool,
    supplyLiquidity,
    deployCw20WithPointer,
    deployEthersContract,
    doesTokenFactoryDenomExist,
    pollBalance,
    sendFunds
}