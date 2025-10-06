#!/usr/bin/env node
try {
  // eslint-disable-next-line global-require
  require("dotenv").config();
} catch (error) {
  if (error.code !== "MODULE_NOT_FOUND") {
    throw error;
  }
}

const { randomInt } = require("node:crypto");
const {
  AbiCoder,
  Wallet,
  getAddress,
  getBytes,
  keccak256,
  solidityPackedKeccak256,
} = require("ethers");

let qrcode;
try {
  // eslint-disable-next-line global-require, import/no-extraneous-dependencies
  qrcode = require("qrcode-terminal");
} catch (error) {
  qrcode = {
    generate: (text) => {
      console.warn(
        "qrcode-terminal not installed. Install it to display ASCII QR codes. Payload:",
        text
      );
    },
  };
}

const PRIVATE_KEY = process.env.VALIDATOR_PRIVKEY;

if (!PRIVATE_KEY) {
  throw new Error("Missing VALIDATOR_PRIVKEY environment variable");
}

const signer = new Wallet(PRIVATE_KEY);
const abiCoder = AbiCoder.defaultAbiCoder();

function getWifiHash(mac, ssid, nonce, timestamp) {
  const payload = abiCoder.encode(
    ["string", "string", "uint256", "uint256"],
    [mac, ssid, nonce, timestamp]
  );
  return keccak256(payload);
}

function buildBeaconPayload({ validator, user, wifiHash, signature, timestamp, nonce }) {
  return {
    validator,
    user,
    wifiHash,
    signature,
    timestamp,
    nonce,
  };
}

async function signAndBroadcast(mac, ssid, userAddress) {
  if (!mac || !ssid || !userAddress) {
    throw new Error("Expected MAC, SSID, and user address arguments");
  }

  const normalizedUserAddress = getAddress(userAddress);

  const timestamp = Math.floor(Date.now() / 1000);
  const nonce = randomInt(1_000_000);
  const wifiHash = getWifiHash(mac, ssid, nonce, timestamp);

  const digest = solidityPackedKeccak256([
    "address",
    "bytes32",
  ], [
    normalizedUserAddress,
    wifiHash,
  ]);

  const signature = await signer.signMessage(getBytes(digest));

  const payload = buildBeaconPayload({
    validator: signer.address,
    user: normalizedUserAddress,
    wifiHash,
    signature,
    timestamp,
    nonce,
  });

  console.log("\n📡 Broadcasting WiFi Beacon\n", payload);
  qrcode.generate(JSON.stringify(payload), { small: true });
}

if (require.main === module) {
  const [mac, ssid, userAddress] = process.argv.slice(2);
  signAndBroadcast(mac, ssid, userAddress).catch((error) => {
    console.error("Failed to broadcast beacon:", error);
    process.exit(1);
  });
}

module.exports = {
  getWifiHash,
  signAndBroadcast,
};
