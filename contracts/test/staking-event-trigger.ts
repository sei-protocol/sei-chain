import {
  createPublicClient,
  createWalletClient,
  http,
  parseEther,
  formatEther,
  parseAbi,
} from "viem";
import { privateKeyToAccount } from "viem/accounts";

// Staking precompile address
const STAKING_PRECOMPILE_ADDRESS = "0x0000000000000000000000000000000000001005";

// Staking ABI
const STAKING_ABI = parseAbi([
  "function delegate(string valAddress) payable returns (bool success)",
  "function undelegate(string valAddress, uint256 amount) returns (bool success)",
  "function redelegate(string srcAddress, string dstAddress, uint256 amount) returns (bool success)",
]);

async function main() {
  console.log("🚀 Sei Staking Event Trigger\n");

  // Use the admin account which has an associated EVM address
  const privateKey =
    "0xd8ad3f4721da2cb78fac64351851e47bfbd27deb0692a26844b18b0fbb8d640a" as `0x${string}`;
  const account = privateKeyToAccount(privateKey);
  console.log("Using account:", account.address);

  // Define custom chain for Sei
  const seiLocalChain = {
    id: 713714, // EVM chain ID (0xae3f2 in hex)
    name: "Sei Local",
    network: "sei-local",
    nativeCurrency: {
      decimals: 18,
      name: "SEI",
      symbol: "SEI",
    },
    rpcUrls: {
      default: { http: ["http://localhost:8545"] },
      public: { http: ["http://localhost:8545"] },
    },
  };

  // Create clients
  const publicClient = createPublicClient({
    chain: seiLocalChain,
    transport: http("http://localhost:8545"),
  });

  const walletClient = createWalletClient({
    account,
    chain: seiLocalChain,
    transport: http("http://localhost:8545"),
  });

  // Check balance
  const balance = await publicClient.getBalance({ address: account.address });
  console.log("Account balance:", formatEther(balance), "SEI\n");

  // Get action from command line argument
  const action = process.argv[2] || "delegate";
  const amount = process.argv[3] || "10";

  // Default validator address
  const validatorAddress = "seivaloper1r02jjxy8stae4sy9v6ghexfxcp6vygkgud3pr2";

  try {
    switch (action) {
      case "delegate":
        console.log(`📤 Sending DELEGATE transaction...`);
        console.log(`   Amount: ${amount} SEI`);
        console.log(`   Validator: ${validatorAddress}`);

        const { request: delegateRequest } =
          await publicClient.simulateContract({
            account,
            address: STAKING_PRECOMPILE_ADDRESS,
            abi: STAKING_ABI,
            functionName: "delegate",
            args: [validatorAddress],
            value: parseEther(amount),
          });

        const delegateHash = await walletClient.writeContract(delegateRequest);
        console.log(`\n✅ Transaction sent!`);
        console.log(`   Hash: ${delegateHash}`);

        const delegateReceipt = await publicClient.waitForTransactionReceipt({
          hash: delegateHash,
        });
        console.log(`   Block: ${delegateReceipt.blockNumber}`);
        console.log(`   Status: ${delegateReceipt.status}`);
        console.log(`   Gas Used: ${delegateReceipt.gasUsed}`);
        break;

      case "undelegate":
        console.log(`📤 Sending UNDELEGATE transaction...`);
        console.log(`   Amount: ${amount} SEI`);
        console.log(`   Validator: ${validatorAddress}`);

        const { request: undelegateRequest } =
          await publicClient.simulateContract({
            account,
            address: STAKING_PRECOMPILE_ADDRESS,
            abi: STAKING_ABI,
            functionName: "undelegate",
            args: [validatorAddress, parseEther(amount)],
          });

        const undelegateHash = await walletClient.writeContract(
          undelegateRequest
        );
        console.log(`\n✅ Transaction sent!`);
        console.log(`   Hash: ${undelegateHash}`);

        const undelegateReceipt = await publicClient.waitForTransactionReceipt({
          hash: undelegateHash,
        });
        console.log(`   Block: ${undelegateReceipt.blockNumber}`);
        console.log(`   Status: ${undelegateReceipt.status}`);
        console.log(`   Gas Used: ${undelegateReceipt.gasUsed}`);
        break;

      case "redelegate":
        const destValidator = process.argv[4] || validatorAddress;
        console.log(`📤 Sending REDELEGATE transaction...`);
        console.log(`   Amount: ${amount} SEI`);
        console.log(`   From: ${validatorAddress}`);
        console.log(`   To: ${destValidator}`);

        const { request: redelegateRequest } =
          await publicClient.simulateContract({
            account,
            address: STAKING_PRECOMPILE_ADDRESS,
            abi: STAKING_ABI,
            functionName: "redelegate",
            args: [validatorAddress, destValidator, parseEther(amount)],
          });

        const redelegateHash = await walletClient.writeContract(
          redelegateRequest
        );
        console.log(`\n✅ Transaction sent!`);
        console.log(`   Hash: ${redelegateHash}`);

        const redelegateReceipt = await publicClient.waitForTransactionReceipt({
          hash: redelegateHash,
        });
        console.log(`   Block: ${redelegateReceipt.blockNumber}`);
        console.log(`   Status: ${redelegateReceipt.status}`);
        console.log(`   Gas Used: ${redelegateReceipt.gasUsed}`);
        break;

      default:
        console.log("❌ Unknown action:", action);
        console.log("\nUsage:");
        console.log("  npm run trigger delegate [amount]");
        console.log("  npm run trigger undelegate [amount]");
        console.log("  npm run trigger redelegate [amount] [destValidator]");
        process.exit(1);
    }

    console.log("\n✨ Done! Check the event listener for emitted events.");
  } catch (error) {
    console.error("\n❌ Transaction failed:", error);
    process.exit(1);
  }
}

main().catch(console.error);
