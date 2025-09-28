import { useCallback, useEffect, useMemo, useState } from "react";
import { keccak256, solidityPacked } from "ethers";

export interface SoulSigilProofState {
  fingerprint: string | null;
  payload: {
    account: string;
    chainId: number;
    timestamp: number;
  } | null;
  refresh: () => void;
}

export interface SoulSigilProofOptions {
  /**
   * Provide a custom timestamp if you want deterministic proofs.
   * When omitted, the hook will maintain the timestamp internally
   * and update it every time {@link SoulSigilProofState.refresh} is called.
   */
  timestamp?: number;
}

const DEFAULT_TIMESTAMP = () => Math.floor(Date.now() / 1000);

/**
 * Generates an x402-style keccak256 fingerprint that can be used to authorise
 * vault claim routes and NFT mints. The fingerprint is derived from the
 * connected account, the chain identifier, and a unix timestamp.
 */
export const useSoulSigilProof = (
  account?: string | null,
  chainId?: number | null,
  options: SoulSigilProofOptions = {}
): SoulSigilProofState => {
  const [internalTimestamp, setInternalTimestamp] = useState<number>(
    options.timestamp ?? DEFAULT_TIMESTAMP()
  );

  useEffect(() => {
    if (typeof options.timestamp === "number") {
      setInternalTimestamp(options.timestamp);
    }
  }, [options.timestamp]);

  useEffect(() => {
    if (typeof options.timestamp === "number") {
      return;
    }

    setInternalTimestamp(DEFAULT_TIMESTAMP());
  }, [account, chainId, options.timestamp]);

  const refresh = useCallback(() => {
    if (typeof options.timestamp === "number") {
      return;
    }

    setInternalTimestamp(DEFAULT_TIMESTAMP());
  }, [options.timestamp]);

  const payload = useMemo(() => {
    if (!account || !chainId) {
      return null;
    }

    return {
      account: account.toLowerCase(),
      chainId,
      timestamp: internalTimestamp,
    };
  }, [account, chainId, internalTimestamp]);

  const fingerprint = useMemo(() => {
    if (!payload) {
      return null;
    }

    const packed = solidityPacked(
      ["address", "uint256", "uint256"],
      [payload.account, payload.chainId, payload.timestamp]
    );

    return keccak256(packed);
  }, [payload]);

  return useMemo(
    () => ({
      fingerprint,
      payload,
      refresh,
    }),
    [fingerprint, payload, refresh]
  );
};

export default useSoulSigilProof;
