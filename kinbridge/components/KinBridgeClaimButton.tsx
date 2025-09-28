import React, { useCallback, useMemo, useState } from "react";
import { BrowserProvider, Contract, formatUnits } from "ethers";
import useSoulSigilProof from "../hooks/useSoulSigilProof";

const ROYALTY_BPS = 430n;
const BASIS_POINTS = 10_000n;

const ROUTER_ABI = [
  "function routeERC20Claim(bytes32 claimId, address token, address claimant, uint256 amount) external",
  "function routeNativeClaim(bytes32 claimId, address claimant) external payable",
];

export interface KinBridgeClaimButtonProps {
  provider?: BrowserProvider;
  account?: string | null;
  chainId?: number | null;
  routerAddress: string;
  claimAmount: bigint;
  decimals?: number;
  tokenSymbol?: string;
  /**
   * ERC20 token that represents the bridged asset. When omitted the button
   * assumes the claim is denominated in the native token of the connected chain.
   */
  tokenAddress?: string;
  disabled?: boolean;
  className?: string;
  onClaimed?: (txHash: string) => void;
  onError?: (error: unknown) => void;
}

const formatAmount = (amount: bigint, decimals: number): string => {
  try {
    return formatUnits(amount, decimals);
  } catch (err) {
    return amount.toString();
  }
};

const KinBridgeClaimButton: React.FC<KinBridgeClaimButtonProps> = ({
  provider,
  account,
  chainId,
  routerAddress,
  claimAmount,
  decimals = 18,
  tokenSymbol = "SEI",
  tokenAddress,
  disabled,
  className,
  onClaimed,
  onError,
}) => {
  const { fingerprint, payload, refresh } = useSoulSigilProof(account, chainId);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [txHash, setTxHash] = useState<string | null>(null);

  const royaltyAmount = useMemo(() => {
    return (claimAmount * ROYALTY_BPS) / BASIS_POINTS;
  }, [claimAmount]);

  const netAmount = useMemo(() => {
    const net = claimAmount - royaltyAmount;
    return net > 0n ? net : 0n;
  }, [claimAmount, royaltyAmount]);

  const isReady = !!provider && !!account && !!fingerprint;

  const handleError = useCallback(
    (cause: unknown) => {
      const message =
        cause instanceof Error ? cause.message : typeof cause === "string" ? cause : "Claim failed";
      setError(message);
      onError?.(cause);
    },
    [onError]
  );

  const handleClaim = useCallback(async () => {
    if (!provider || !account || !fingerprint) {
      handleError("Wallet context unavailable");
      return;
    }

    setSubmitting(true);
    setError(null);

    try {
      const signer = await provider.getSigner();
      const router = new Contract(routerAddress, ROUTER_ABI, signer);
      let tx;

      if (tokenAddress) {
        tx = await router.routeERC20Claim(fingerprint, tokenAddress, account, claimAmount);
      } else {
        tx = await router.routeNativeClaim(fingerprint, account, {
          value: claimAmount,
        });
      }

      const receipt = await tx.wait();
      const hash = receipt?.hash ?? tx.hash;
      setTxHash(hash);
      onClaimed?.(hash);
    } catch (cause) {
      handleError(cause);
    } finally {
      refresh();
      setSubmitting(false);
    }
  }, [account, claimAmount, fingerprint, handleError, onClaimed, provider, refresh, routerAddress, tokenAddress]);

  return (
    <div className={className}>
      <button onClick={handleClaim} disabled={!isReady || submitting || disabled}>
        {submitting ? "Routing claim..." : `Claim ${formatAmount(netAmount, decimals)} ${tokenSymbol}`}
      </button>
      {payload && (
        <p className="kinbridge-proof">Proof: {fingerprint}</p>
      )}
      <p className="kinbridge-royalty">
        Royalty ({Number(ROYALTY_BPS) / 100}%): {formatAmount(royaltyAmount, decimals)} {tokenSymbol}
      </p>
      {txHash && <p className="kinbridge-tx">Claimed in transaction: {txHash}</p>}
      {error && <p className="kinbridge-error">{error}</p>}
    </div>
  );
};

export default KinBridgeClaimButton;
