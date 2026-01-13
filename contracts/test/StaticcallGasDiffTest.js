const fs = require("fs");
const path = require("path");
const { expect } = require("chai");
const { ethers } = require("hardhat");
const {
  setupSigners,
  getAdmin,
  deployWasm,
  deployErc20PointerForCw20,
  executeWasm,
  getSeiAddress,
  rawHttpDebugTraceWithCallTracer,
  WASM,
} = require("./lib");

const WASMD_PRECOMPILE = "0x0000000000000000000000000000000000001002";
const JSON_PRECOMPILE = "0x0000000000000000000000000000000000001003";
const ADDR_PRECOMPILE = "0x0000000000000000000000000000000000001004";
const ADDR_ABI = [
  "function getSeiAddr(address addr) view returns (string)",
  "function associatePubKey(string pubKeyHex) returns (string,address)",
];
// Paste STEP 1 tx hashes here before running STEP 2.
const STEP2_TX_HASHES = {
  probeTotalSupply: "0xd4a94a78ed71e67091b88915cc0beb1c5a236d5431656f1967e64e383f801acd",
  harvest: "0xa54e61d6b0f6d95eedbdd0bf4b244abeab9a144dae82a8b07a8bd18a04169558",
};
// Paste STEP 1 gas-used summaries here before running STEP 2.
const STEP2_V62_EXPECTED_GAS = {
  probeTotalSupply: {
    receiptGasUsed: "",
    trace: {
      rootGasUsed: "",
      wasmdGasUsed: [],
      jsonGasUsed: [],
    },
  },
  harvest: {
    receiptGasUsed: "",
    trace: {
      rootGasUsed: "",
      wasmdGasUsed: [],
      jsonGasUsed: [],
    },
  },
};
const DEFAULT_SNAPSHOT_PATH = path.resolve(
  __dirname,
  "..",
  "cache",
  "staticcall-gas-diff.json"
);
const SNAPSHOT_PATH =
  process.env.STATICCALL_GAS_DIFF_FILE || DEFAULT_SNAPSHOT_PATH;

function writeSnapshot(snapshot) {
  const dir = path.dirname(SNAPSHOT_PATH);
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(SNAPSHOT_PATH, JSON.stringify(snapshot, null, 2));
}

function readSnapshot() {
  if (!fs.existsSync(SNAPSHOT_PATH)) {
    return null;
  }
  const raw = fs.readFileSync(SNAPSHOT_PATH, "utf8");
  return JSON.parse(raw);
}

function parseAmount(name, fallback) {
  const raw = process.env[name] || fallback;
  try {
    return BigInt(raw);
  } catch (err) {
    throw new Error(`${name} must be a base-10 integer string`);
  }
}

function parseOptionalGasLimit(value) {
  if (!value) {
    return null;
  }
  return BigInt(value);
}

async function ensureAssociated(signer) {
  const addrContract = new ethers.Contract(ADDR_PRECOMPILE, ADDR_ABI, signer);
  const evmAddress = await signer.getAddress();
  try {
    await addrContract.getSeiAddr(evmAddress);
    return;
  } catch (err) {
  }
  const message = "associate";
  const signature = await signer.signMessage(message);
  const digest = ethers.hashMessage(message);
  const pubKey = ethers.SigningKey.recoverPublicKey(digest, signature);
  const compressed = ethers.SigningKey.computePublicKey(pubKey, true).slice(2);
  await addrContract.associatePubKey(compressed);
}

function collectCalls(call, out = []) {
  if (!call || typeof call !== "object") {
    return out;
  }
  out.push(call);
  const nested = Array.isArray(call.calls) ? call.calls : [];
  for (const child of nested) {
    collectCalls(child, out);
  }
  return out;
}

function gasToString(value) {
  if (value === undefined || value === null) {
    return null;
  }
  if (typeof value === "bigint") {
    return value.toString();
  }
  if (typeof value === "number") {
    return BigInt(value).toString();
  }
  const str = String(value).trim();
  if (str.length === 0) {
    return null;
  }
  return BigInt(str).toString();
}

function summarizeCalls(calls, addr) {
  const target = addr.toLowerCase();
  const filtered = calls.filter(
    (c) => typeof c.to === "string" && c.to.toLowerCase() === target
  );
  return {
    count: filtered.length,
    gasUsed: filtered.map((c) => gasToString(c.gasUsed)),
    errors: filtered.map((c) => c.error || null),
  };
}

function summarizeGasUsed(summary, receipt) {
  return {
    receiptGasUsed: gasToString(receipt.gasUsed),
    trace: {
      rootGasUsed: summary.rootGasUsed,
      wasmdGasUsed: summary.wasmd.gasUsed,
      jsonGasUsed: summary.json.gasUsed,
    },
  };
}

describe("Staticcall gas probe (wasmd -> json)", function () {
  function summarizeTrace(trace) {
    if (!trace || !trace.result) {
      throw new Error("debug_traceTransaction returned no result");
    }
    const calls = collectCalls(trace.result);
    return {
      error: trace.result.error || null,
      rootGasUsed: gasToString(trace.result.gasUsed),
      wasmd: summarizeCalls(calls, WASMD_PRECOMPILE),
      json: summarizeCalls(calls, JSON_PRECOMPILE),
    };
  }

  async function traceAndSummarize(txHash) {
    const trace = await rawHttpDebugTraceWithCallTracer(txHash);
    if (trace.error) {
      throw new Error(`debug_traceTransaction error: ${trace.error.message || trace.error}`);
    }
    return summarizeTrace(trace);
  }

  async function logTraceSummary(txHash, label) {
    try {
      const trace = await rawHttpDebugTraceWithCallTracer(txHash);
      if (trace.error) {
        console.log(`${label} debug_traceTransaction error:`, trace.error);
        return;
      }
      const summary = summarizeTrace(trace);
      console.log(`${label} trace summary:\n${JSON.stringify(summary, null, 2)}`);
    } catch (err) {
      console.log(`${label} trace failed:`, err?.message || err);
    }
  }

  it("STEP 1 (v6.2): runs probes and prints tx hashes", async function () {
    const accounts = await setupSigners(await ethers.getSigners());
    const admin = await getAdmin();
    const harvestLoopCount = Number(process.env.HARVEST_LOOP_COUNT || "50");
    if (!Number.isFinite(harvestLoopCount) || harvestLoopCount < 1) {
      throw new Error("HARVEST_LOOP_COUNT must be >= 1");
    }
    const harvestDoTransfer = ["1", "true", "yes"].includes(
      String(process.env.HARVEST_DO_TRANSFER || "").toLowerCase()
    );
    const harvestGasLimit = parseOptionalGasLimit(process.env.HARVEST_GAS_LIMIT);
    const harvestTransferAmount = parseAmount(
      "HARVEST_TRANSFER_AMOUNT",
      "100000"
    );
    const account0InitialBalance = harvestTransferAmount + 2000000n;
    const adminInitialBalance = harvestTransferAmount + 3000000n;
    const cw20Cap = (adminInitialBalance + account0InitialBalance).toString();

    const cw20Address = await deployWasm(WASM.CW20, accounts[0].seiAddress, "cw20", {
      name: "GasProbe",
      symbol: "GAS",
      decimals: 6,
      initial_balances: [
        { address: admin.seiAddress, amount: adminInitialBalance.toString() },
        { address: accounts[0].seiAddress, amount: account0InitialBalance.toString() },
      ],
      mint: { minter: admin.seiAddress, cap: cw20Cap },
    });

    const pointerAddr = await deployErc20PointerForCw20(ethers.provider, cw20Address);

    const ProbeFactory = await ethers.getContractFactory("PrecompileStaticcallGasProbe");
    const probe = await ProbeFactory.deploy();
    await probe.waitForDeployment();

    const totalSupplyTx = await probe.probeTotalSupply(pointerAddr);
    const totalSupplyReceipt = await totalSupplyTx.wait();
    expect(totalSupplyReceipt.status).to.equal(1);
    console.log(`STEP 1 tx hash (probeTotalSupply): ${totalSupplyReceipt.hash}`);
    const totalSupplySummary = await traceAndSummarize(totalSupplyReceipt.hash);
    if (totalSupplySummary.wasmd.count === 0) {
      throw new Error("probeTotalSupply did not hit wasmd precompile");
    }

    const HarvestFactory = await ethers.getContractFactory(
      "PrecompileStaticcallHarvestProbe"
    );
    const harvester = await HarvestFactory.deploy(
      pointerAddr,
      harvestLoopCount,
      harvestDoTransfer
    );
    await harvester.waitForDeployment();
    await ensureAssociated(accounts[0].signer);
    console.log(`STEP 1 harvest transfer amount: ${harvestTransferAmount}`);
    console.log(`STEP 1 harvest transfer enabled: ${harvestDoTransfer}`);
    if (harvestDoTransfer) {
      const harvesterSeiAddress = await getSeiAddress(
        await harvester.getAddress()
      );
      await executeWasm(cw20Address, {
        transfer: {
          recipient: harvesterSeiAddress,
          amount: harvestTransferAmount.toString(),
        },
      });
    }

    console.log(`STEP 1 harvest loop count: ${harvestLoopCount}`);
    let harvestReceipt;
    try {
      const harvestTx = await harvester.harvest(
        harvestGasLimit ? { gasLimit: harvestGasLimit } : {}
      );
      harvestReceipt = await harvestTx.wait();
    } catch (err) {
      const txHash =
        err?.receipt?.hash ||
        err?.receipt?.transactionHash ||
        err?.transactionHash;
      if (txHash) {
        await logTraceSummary(txHash, "HARVEST failure");
      }
      throw err;
    }
    expect(harvestReceipt.status).to.equal(1);
    console.log(`STEP 1 tx hash (harvest): ${harvestReceipt.hash}`);
    const harvestSummary = await traceAndSummarize(harvestReceipt.hash);
    if (harvestSummary.wasmd.count === 0) {
      throw new Error("harvest did not hit wasmd precompile");
    }

    const step1Hashes = {
      probeTotalSupply: totalSupplyReceipt.hash,
      harvest: harvestReceipt.hash,
    };
    const step1ExpectedGas = {
      probeTotalSupply: summarizeGasUsed(totalSupplySummary, totalSupplyReceipt),
      harvest: summarizeGasUsed(harvestSummary, harvestReceipt),
    };
    const snapshot = {
      generatedAt: new Date().toISOString(),
      harvestLoopCount,
      harvestGasLimit: harvestGasLimit ? harvestGasLimit.toString() : null,
      harvestTransferAmount: harvestTransferAmount.toString(),
      txHashes: step1Hashes,
      expectedGas: step1ExpectedGas,
    };
    writeSnapshot(snapshot);
    console.log(
      `STEP 1 tx hashes to paste into STEP2_TX_HASHES:\n${JSON.stringify(
        step1Hashes,
        null,
        2
      )}`
    );
    console.log(
      `STEP 1 gas used to paste into STEP2_V62_EXPECTED_GAS:\n${JSON.stringify(
        step1ExpectedGas,
        null,
        2
      )}`
    );
    console.log(`STEP 1 snapshot saved to ${SNAPSHOT_PATH}`);
  });

  it("STEP 2 (v6.3): traces provided txs and compares gas usage", async function () {
    const snapshot = readSnapshot();
    if (snapshot && (!snapshot.txHashes || !snapshot.expectedGas)) {
      throw new Error(`Snapshot at ${SNAPSHOT_PATH} is missing txHashes or expectedGas`);
    }
    const step2Hashes = snapshot?.txHashes || STEP2_TX_HASHES;
    const step2ExpectedGas = snapshot?.expectedGas || STEP2_V62_EXPECTED_GAS;
    if (snapshot) {
      console.log(`STEP 2 using snapshot from ${SNAPSHOT_PATH}`);
    }
    const targets = [
      { name: "probeTotalSupply", hash: step2Hashes.probeTotalSupply },
      { name: "harvest", hash: step2Hashes.harvest },
    ];

    for (const target of targets) {
      const txHash =
        typeof target.hash === "string" ? target.hash.trim() : "";
      if (!txHash) {
        throw new Error(
          `Missing tx hash for ${target.name}; run STEP 1 or set STEP2_TX_HASHES.${target.name}`
        );
      }
      const expected = step2ExpectedGas[target.name];
      if (
        !expected ||
        !expected.trace ||
        !expected.trace.rootGasUsed ||
        !expected.receiptGasUsed
      ) {
        throw new Error(
          `Missing expected gas for ${target.name}; run STEP 1 or set STEP2_V62_EXPECTED_GAS.${target.name}`
        );
      }
      const receipt = await ethers.provider.getTransactionReceipt(txHash);
      if (!receipt) {
        throw new Error(`No receipt found for ${txHash}`);
      }
      const actualTrace = await traceAndSummarize(txHash);
      const actual = summarizeGasUsed(actualTrace, receipt);
      console.log(
        `STEP 2 expected v6.2 gas (${target.name}):\n${JSON.stringify(
          expected,
          null,
          2
        )}`
      );
      console.log(
        `STEP 2 actual v6.3 gas (${target.name}):\n${JSON.stringify(actual, null, 2)}`
      );
      expect(actual).to.deep.equal(expected);
    }
  });
});
