import {
  createPublicClient,
  http,
  parseAbi,
  decodeEventLog,
  formatEther,
} from "viem";

// Staking precompile address
const STAKING_PRECOMPILE_ADDRESS = "0x0000000000000000000000000000000000001005";

// Staking ABI
const STAKING_ABI = parseAbi([
  "event Delegate(address indexed delegator, string validator, uint256 amount)",
  "event Undelegate(address indexed delegator, string validator, uint256 amount)",
  "event Redelegate(address indexed delegator, string srcValidator, string dstValidator, uint256 amount)",
  "event ValidatorCreated(address indexed creator, string validatorAddress, string moniker)",
  "event ValidatorEdited(address indexed editor, string validatorAddress, string moniker)",
]);

async function main() {
  console.log("🎧 Starting Sei Staking Event Listener...\n");

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

  // Create public client
  const publicClient = createPublicClient({
    chain: seiLocalChain,
    transport: http("http://localhost:8545"),
  });

  console.log("📡 Connected to Sei EVM RPC at http://localhost:8545");
  console.log(
    "👀 Watching for events from Staking Precompile:",
    STAKING_PRECOMPILE_ADDRESS
  );
  console.log("\nListening for:");
  console.log("  - Delegate events");
  console.log("  - Undelegate events");
  console.log("  - Redelegate events");
  console.log("  - ValidatorCreated events");
  console.log("  - ValidatorEdited events");
  console.log("\n⏳ Waiting for events...\n");

  // Track the last processed block to avoid querying future blocks
  let lastProcessedBlock = await publicClient.getBlockNumber();
  let errorCount = 0;
  const maxErrors = 10;

  // Use manual polling to avoid "fromBlock is after toBlock" errors
  const pollInterval = setInterval(async () => {
    try {
      const currentBlock = await publicClient.getBlockNumber();

      // Only query if new blocks exist
      if (currentBlock > lastProcessedBlock) {
        const logs = await publicClient.getLogs({
          address: STAKING_PRECOMPILE_ADDRESS,
          fromBlock: lastProcessedBlock + 1n,
          toBlock: currentBlock,
        });

        if (logs.length > 0) {
          console.log(`\n📥 Received ${logs.length} log(s)`);

          for (const log of logs) {
            try {
              const decodedEvent = decodeEventLog({
                abi: STAKING_ABI,
                data: log.data,
                topics: log.topics as any,
              });

              console.log("🎉 Event Received!");
              console.log("=====================================");
              console.log("Event Type:", decodedEvent.eventName);
              console.log("Block Number:", log.blockNumber);
              console.log("Transaction Hash:", log.transactionHash);
              console.log("Log Index:", log.logIndex);

              const args = decodedEvent.args as any;

              switch (decodedEvent.eventName) {
                case "Delegate":
                  console.log("\nDelegate Event Details:");
                  console.log("  Delegator:", args.delegator);
                  console.log("  Validator:", args.validator);
                  console.log("  Amount:", formatEther(args.amount), "SEI");
                  break;

                case "Undelegate":
                  console.log("\nUndelegate Event Details:");
                  console.log("  Delegator:", args.delegator);
                  console.log("  Validator:", args.validator);
                  console.log("  Amount:", formatEther(args.amount), "SEI");
                  break;

                case "Redelegate":
                  console.log("\nRedelegate Event Details:");
                  console.log("  Delegator:", args.delegator);
                  console.log("  Source Validator:", args.srcValidator);
                  console.log("  Destination Validator:", args.dstValidator);
                  console.log("  Amount:", formatEther(args.amount), "SEI");
                  break;

                case "ValidatorCreated":
                  console.log("\nValidator Created Event Details:");
                  console.log("  Creator:", args.creator);
                  console.log("  Validator Address:", args.validatorAddress);
                  console.log("  Moniker:", args.moniker);
                  break;

                case "ValidatorEdited":
                  console.log("\nValidator Edited Event Details:");
                  console.log("  Editor:", args.editor);
                  console.log("  Validator Address:", args.validatorAddress);
                  console.log("  Moniker:", args.moniker);
                  break;
              }

              console.log("=====================================\n");
            } catch (error) {
              console.error("Error decoding event:", error);
            }
          }
        }

        // Update last processed block
        lastProcessedBlock = currentBlock;
        errorCount = 0; // Reset error count on success
      }
    } catch (error: any) {
      // Only log non-"fromBlock after toBlock" errors
      if (
        !error.message?.includes("fromBlock") ||
        !error.message?.includes("after toBlock")
      ) {
        errorCount++;
        console.error(
          `⚠️  Error polling for events (${errorCount}/${maxErrors}):`,
          error.message
        );
        if (errorCount >= maxErrors) {
          console.error("❌ Too many errors, stopping event listener");
          clearInterval(pollInterval);
        }
      }
    }
  }, 1000); // Poll every second

  // Keep the process running
  process.on("SIGINT", () => {
    console.log("\n\n👋 Stopping event listener...");
    clearInterval(pollInterval);
    process.exit(0);
  });

  // Log periodic heartbeat to show the listener is still running
  setInterval(() => {
    console.log("💓 Listener is active... (Press Ctrl+C to stop)");
  }, 30000); // Every 30 seconds
}

main().catch((error) => {
  console.error("Fatal error:", error);
  process.exit(1);
});
