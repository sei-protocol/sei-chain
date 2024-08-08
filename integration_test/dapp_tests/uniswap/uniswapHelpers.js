const hre = require("hardhat");
const {BigNumber} = require("ethers"); // Require Hardhat Runtime Environment
const { ABI, deployErc20PointerForCw20, deployWasm } = require("../../../contracts/test/lib.js");

async function deployTokenPool(managerContract, firstTokenAddr, secondTokenAddr, swapRatio=1, fee=3000) {
    const sqrtPriceX96 = BigInt(Math.sqrt(swapRatio) * (2 ** 96)); // Initial price (1:1)

    // token0 addr must be < token1 addr
    const [token0, token1] = tokenOrder(firstTokenAddr, secondTokenAddr);
    const poolTx = await managerContract.createAndInitializePoolIfNecessary(
        token0.address,
        token1.address,
        fee,
        sqrtPriceX96
    );
    await poolTx.wait();
    console.log("Pool created and initialized");
}

// Supplies liquidity to then given pools. The signer connected to the contracts must have the prerequisite tokens or this will fail.
async function supplyLiquidity(managerContract, recipientAddr, firstTokenContract, secondTokenContract, firstTokenAmt=100, secondTokenAmt=100) {
    // Define the amount of tokens to be approved and added as liquidity
    console.log("Supplying liquidity to pool")

    const [token0, token1] = tokenOrder(firstTokenContract.address, secondTokenContract.address, firstTokenAmt, secondTokenAmt);

    // // Wrap ETH to WETH by depositing ETH into the WETH9 contract
    // const txWrap = await weth9.deposit({ value: amountETH });
    // await txWrap.wait();
    // console.log(`Deposited ${amountETH.toString()} ETH to WETH9`);

    // Approve the NonfungiblePositionManager to spend the specified amount of firstToken
    const approveFirstTokenTx = await firstTokenContract.approve(managerContract.address, firstTokenAmt);
    await approveFirstTokenTx.wait();

    // Approve the NonfungiblePositionManager to spend the specified amount of secondToken
    const approveSecondTokenTx = await secondTokenContract.approve(managerContract.address, secondTokenAmt);
    await approveSecondTokenTx.wait();

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
        deadline: Math.floor(Date.now() / 1000) + 60 * 10 // 10 minutes from now
    });

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

async function deployCw20WithPointer(deployerObj, time) {
    cw20Address = await deployWasm('./uniswap/cw20_base.wasm', deployerObj.seiAddress, "cw20", {
        name: `testCw20${time}`,
        symbol: "TEST",
        decimals: 6,
        initial_balances: [
            { address: deployerObj.seiAddress, amount: "1000000000" }
        ],
        mint: {
            "minter": deployerObj.seiAddress, "cap": "99900000000"
        }
    });

    const pointerAddr = await deployErc20PointerForCw20(hre.ethers.provider, cw20Address);
    return new hre.ethers.Contract(pointerAddr, ABI.ERC20, deployerObj.signer);
}

module.exports = {
    deployTokenPool,
    supplyLiquidity,
    deployCw20WithPointer
}