import { spawn } from "child_process";
import dotenv from "dotenv";

dotenv.config();

async function runTest() {
  console.log("🚀 Starting Staking Event Test\n");
  console.log("This will:");
  console.log("1. Start the event listener");
  console.log("2. Wait for it to initialize");
  console.log("3. Trigger a delegate transaction");
  console.log("4. Wait for the event to be captured");
  console.log("5. Clean up and exit\n");

  // Check for required environment variable
  if (!process.env.TEST_ADMIN_PRIVATE_KEY) {
    console.error(
      "❌ Error: TEST_ADMIN_PRIVATE_KEY environment variable not set"
    );
    console.error("\nFor local testing with the default admin account:");
    console.error("  export TEST_ADMIN_PRIVATE_KEY=0x.....");
    console.error(
      "\nThis is ONLY for local testing - NEVER use real private keys!"
    );
    process.exit(1);
  }

  // Start the listener
  console.log("📡 Starting event listener...");
  const listener = spawn("tsx", ["test/staking-event-listener.ts"], {
    stdio: ["ignore", "pipe", "pipe"],
    env: { ...process.env }, // Pass environment variables to child process
  });

  let eventCaptured = false;

  // Capture listener output
  listener.stdout.on("data", (data) => {
    const output = data.toString();
    process.stdout.write(`[LISTENER] ${output}`);

    // Check if event was received - look for various patterns
    if (
      output.includes("Event Received!") ||
      output.includes("Event Type: Delegate") ||
      output.includes("Delegate Event Details:") ||
      (output.includes("Found") && output.includes("event(s) in recent blocks"))
    ) {
      eventCaptured = true;
    }
  });

  // Wait for listener to start
  await new Promise((resolve) => setTimeout(resolve, 3000));

  // Trigger a delegation
  console.log("\n💸 Triggering delegate transaction...\n");
  const trigger = spawn(
    "tsx",
    ["test/staking-event-trigger.ts", "delegate", "10"],
    {
      stdio: ["ignore", "pipe", "pipe"],
      env: { ...process.env }, // Pass environment variables to child process
    }
  );

  // Capture trigger output
  trigger.stdout.on("data", (data) => {
    process.stdout.write(`[TRIGGER] ${data}`);
  });

  // Wait for trigger to complete
  await new Promise<void>((resolve) => {
    trigger.on("exit", () => {
      console.log("\n[TRIGGER] Transaction completed\n");
      resolve();
    });
  });

  // Wait a bit more for the event to be captured
  console.log("⏳ Waiting for event to be captured...\n");
  await new Promise((resolve) => setTimeout(resolve, 10000)); // Increased to 10 seconds

  // Check results
  if (eventCaptured) {
    console.log("\n✅ SUCCESS: Event was captured by the listener!");
    console.log("The staking event test completed successfully.\n");
  } else {
    console.log(
      "\n⚠️  WARNING: Event was not captured within the timeout period."
    );
    console.log("This might be due to network delays or other issues.\n");
  }

  // Clean up
  console.log("🧹 Cleaning up...");
  listener.kill();

  process.exit(eventCaptured ? 0 : 1);
}

runTest().catch((error) => {
  console.error("Test failed:", error);
  process.exit(1);
});
