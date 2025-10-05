/**
 * WiFi-native payment flow using MAC address + local entropy.
 * Auto-detects SeiMesh SSID, constructs ephemeral vault transaction stub.
 */

import {
  JsonRpcProvider,
  Wallet,
  keccak256,
  parseEther,
  toUtf8Bytes,
} from "ethers";

export interface TapAndPayOptions {
  provider?: JsonRpcProvider;
  vaultFactory?: typeof createStreamingVault;
}

export interface TapAndPayResult {
  txHash: string;
  vaultAddress: string;
  entropy: string;
}

const SEIMESH_PREFIX = "seimesh";

export function deriveEntropy(mac: string, ssid: string, timestamp: number = Date.now()): string {
  const normalizedMac = normalizeMac(mac);
  const normalizedSsid = ssid.trim();
  const payload = `${normalizedMac}-${normalizedSsid}-${timestamp}`;
  return keccak256(toUtf8Bytes(payload));
}

export function normalizeMac(mac: string): string {
  const cleaned = mac.replace(/[^0-9a-fA-F]/g, "").toLowerCase();
  if (cleaned.length !== 12) {
    throw new Error(`Invalid MAC address: ${mac}`);
  }
  return cleaned.match(/.{1,2}/g)!.join(":");
}

export function isSeiMeshNetwork(ssid: string): boolean {
  return ssid.toLowerCase().startsWith(SEIMESH_PREFIX);
}

export async function tapAndPay(
  mac: string,
  ssid: string,
  amount: string,
  signer: Wallet,
  options: TapAndPayOptions = {}
): Promise<TapAndPayResult> {
  if (!isSeiMeshNetwork(ssid)) {
    throw new Error(`SSID ${ssid} is not a SeiMesh network`);
  }

  if (!signer.provider && !options.provider) {
    throw new Error("Signer must be connected to a provider");
  }

  const entropy = deriveEntropy(mac, ssid);
  const vaultCreator = options.vaultFactory ?? createStreamingVault;
  const vaultAddress = await vaultCreator(signer.address, entropy, amount, options.provider);

  const value = parseEther(amount);
  const tx = await signer.sendTransaction({
    to: vaultAddress,
    value,
  });

  return {
    txHash: tx.hash,
    vaultAddress,
    entropy,
  };
}

async function createStreamingVault(
  user: string,
  entropy: string,
  amount: string,
  provider?: JsonRpcProvider
): Promise<string> {
  void user;
  void entropy;
  void amount;
  void provider;
  return "0xSeiMeshVaultAddress";
}
