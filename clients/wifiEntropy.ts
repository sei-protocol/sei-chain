import { AbiCoder, arrayify, keccak256, verifyMessage } from "ethers";

export interface WifiInput {
  mac: string;
  ssid: string;
  nonce?: number;
  timestamp?: number;
}

export interface PresenceProof {
  wifiHash: string;
  signature: string;
  timestamp: number;
  nonce: number;
  validator: string;
}

const abiCoder = AbiCoder.defaultAbiCoder();
const WIFI_TYPES: string[] = ["string", "string", "uint256", "uint256"];

function encodeWifiData(mac: string, ssid: string, nonce: number, timestamp: number): string {
  return abiCoder.encode(WIFI_TYPES, [mac, ssid, nonce, timestamp]);
}

export function generateWifiHash(input: WifiInput): { wifiHash: string; nonce: number; timestamp: number } {
  const nonce = input.nonce ?? Math.floor(Math.random() * 100_000);
  const timestamp = input.timestamp ?? Math.floor(Date.now() / 1000);

  const encoded = encodeWifiData(input.mac, input.ssid, nonce, timestamp);
  const wifiHash = keccak256(encoded);
  return { wifiHash, nonce, timestamp };
}

export function assemblePresenceProof(
  mac: string,
  ssid: string,
  validator: string,
  signature: string,
  nonce: number,
  timestamp: number
): PresenceProof {
  const encoded = encodeWifiData(mac, ssid, nonce, timestamp);
  const wifiHash = keccak256(encoded);

  return {
    wifiHash,
    signature,
    validator,
    timestamp,
    nonce
  };
}

export function recoverValidatorAddress(user: string, wifiHash: string, sig: string): string {
  const digest = keccak256(abiCoder.encode(["address", "bytes32"], [user, wifiHash]));
  return verifyMessage(arrayify(digest), sig);
}
