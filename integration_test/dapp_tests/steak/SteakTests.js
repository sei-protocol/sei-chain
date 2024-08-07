const {
  getAdmin,
  setupSigners,
  storeWasm,
  deployErc20PointerForCw20,
  ABI,
} = require("../../../contracts/test/lib.js");
const { getValidators, instantiateHubContract } = require("./utils.js");

const { expect } = require("chai");

const STEAK_HUB_WASM = "./steak/contracts/steak_hub.wasm";
const STEAK_TOKEN_WASM = "./steak/contracts/steak_token.wasm";

describe("Steak", async function () {
  let accounts;
  let admin;
  let hubAddress;
  let tokenAddress;
  let tokenPointer;

  before(async function () {
    accounts = await setupSigners(await hre.ethers.getSigners());
    admin = await getAdmin();

    // Store cw20 token wasm
    const tokenCodeId = await storeWasm(STEAK_TOKEN_WASM);

    // Store hub contract
    const hubCodeId = await storeWasm(STEAK_HUB_WASM);

    // Instantiate hub and token contracts
    const adminAddress = accounts[0].seiAddress;
    const validators = await getValidators();
    const instantiateMsg = {
      cw20_code_id: parseInt(tokenCodeId),
      owner: adminAddress,
      name: "Steak",
      symbol: "STEAK",
      decimals: 6,
      epoch_period: 259200,
      unbond_period: 1814400,
      validators: validators.slice(0, 3),
    };
    const contractAddresses = await instantiateHubContract(
      hubCodeId,
      adminAddress,
      instantiateMsg,
      "steakhub"
    );
    hubAddress = contractAddresses.hubContract;
    tokenAddress = contractAddresses.tokenContract;

    // Deploy pointer for token contract
    const pointerAddr = await deployErc20PointerForCw20(
      hre.ethers.provider,
      tokenAddress
    );
    const pointerContract = new hre.ethers.Contract(
      pointerAddr,
      ABI.ERC20,
      hre.ethers.provider
    );
    tokenPointer = pointerContract.connect(accounts[0].signer);
  });

  describe("Bonding", async function () {
    it("Unassociated account should be able to bond", async function () {
      console.log("bonding");
    });
    it("Associated account should be able to bond", async function () {
      console.log("bonding");
    });
  });

  //   describe("Harvesting", async function () {
  //     it("Unassociated account should be able to harvest", async function () {
  //       console.log("harvest");
  //     });
  //     it("Associated account should be able to harvest", async function () {
  //       console.log("harvest");
  //     });
  //   });

  //   describe("Unbonding", async function () {
  //     it("Unassociated account should be able to unbond", async function () {
  //       console.log("harvest");
  //     });
  //     it("Associated account should be able to unbond", async function () {
  //       console.log("unbond");
  //     });
  //   });

  //   describe("Swaps", async function () {
  //     it("Associated account should swap successfully", async function () {
  //       let currSeiBal = await user.getBalance();
  //       console.log(
  //         `Funded user account ${
  //           user.address
  //         } with ${hre.ethers.utils.formatEther(currSeiBal)} sei`
  //       );

  //       const fee = 3000; // Fee tier (0.3%)

  //       // Perform a Swap
  //       const amountIn = hre.ethers.utils.parseEther("1");
  //       const amountOutMin = hre.ethers.utils.parseEther("0"); // Minimum amount of MockToken expected

  //       const gasLimit = hre.ethers.utils.hexlify(1000000); // Example gas limit
  //       const gasPrice = await hre.ethers.provider.getGasPrice();

  //       const deposit = await weth9
  //         .connect(user)
  //         .deposit({ value: amountIn, gasLimit, gasPrice });
  //       await deposit.wait();

  //       const weth9balance = await weth9.connect(user).balanceOf(user.address);
  //       expect(weth9balance).to.equal(
  //         amountIn.toString(),
  //         "weth9 balance should be equal to value passed in"
  //       );

  //       const approval = await weth9
  //         .connect(user)
  //         .approve(router.address, amountIn, { gasLimit, gasPrice });
  //       await approval.wait();

  //       const allowance = await weth9.allowance(user.address, router.address);
  //       // Change to expect
  //       expect(allowance).to.equal(
  //         amountIn.toString(),
  //         "weth9 allowance for router should be equal to value passed in"
  //       );

  //       const tx = await router.connect(user).exactInputSingle(
  //         {
  //           tokenIn: weth9.address,
  //           tokenOut: token.address,
  //           fee,
  //           recipient: user.address,
  //           deadline: Math.floor(Date.now() / 1000) + 60 * 10, // 10 minutes from now
  //           amountIn,
  //           amountOutMinimum: amountOutMin,
  //           sqrtPriceLimitX96: 0,
  //         },
  //         { gasLimit, gasPrice }
  //       );

  //       await tx.wait();

  //       // Check User's MockToken Balance
  //       const balance = BigInt(await token.balanceOf(user.address));
  //       // Check that it's more than 0 (no specified amount since there might be slippage)
  //       expect(Number(balance)).to.greaterThan(
  //         0,
  //         "mocktoken should have been swapped successfully."
  //       );
  //     });

  //     it("Unassociated account should receive tokens successfully", async function () {
  //       const unassocUserWallet = ethers.Wallet.createRandom();
  //       const unassocUser = unassocUserWallet.connect(ethers.provider);

  //       // Fund the user account
  //       await fundAddress(unassocUser.address);

  //       const currSeiBal = await unassocUser.getBalance();

  //       const fee = 3000; // Fee tier (0.3%)

  //       // Perform a Swap
  //       const amountIn = hre.ethers.utils.parseEther("1");
  //       const amountOutMin = hre.ethers.utils.parseEther("0"); // Minimum amount of MockToken expected

  //       const deposit = await weth9.deposit({ value: amountIn });
  //       await deposit.wait();

  //       const weth9balance = await weth9.balanceOf(deployer.address);

  //       // Check that deployer has amountIn amount of weth9
  //       expect(weth9balance).to.equal(
  //         amountIn,
  //         "weth9 balance should be received by user"
  //       );

  //       const approval = await weth9.approve(router.address, amountIn);
  //       await approval.wait();

  //       const allowance = await weth9.allowance(deployer.address, router.address);

  //       // Check that deployer has approved amountIn amount of weth9 to be used by router
  //       expect(allowance).to.equal(
  //         amountIn,
  //         "weth9 allowance to router should be set correctly by user"
  //       );

  //       const tx = await router.exactInputSingle({
  //         tokenIn: weth9.address,
  //         tokenOut: token.address,
  //         fee,
  //         recipient: unassocUser.address,
  //         deadline: Math.floor(Date.now() / 1000) + 60 * 10, // 10 minutes from now
  //         amountIn,
  //         amountOutMinimum: amountOutMin,
  //         sqrtPriceLimitX96: 0,
  //       });

  //       await tx.wait();

  //       // Check User's MockToken Balance
  //       const balance = await token.balanceOf(unassocUser.address);
  //       // Check that it's more than 0 (no specified amount since there might be slippage)
  //       expect(Number(balance)).to.greaterThan(
  //         0,
  //         "User should have received some mocktoken"
  //       );
  //     });
  //   });
});
