import React, { useCallback, useEffect, useMemo, useState } from "react";
import { BrowserProvider } from "ethers";
import KinBridgeClaimButton from "./KinBridgeClaimButton";

export interface KinBridgeWidgetProps {
  routerAddress: string;
  /** Gross amount including royalties expressed as the smallest token unit. */
  claimAmount: bigint;
  decimals?: number;
  tokenSymbol?: string;
  tokenAddress?: string;
  className?: string;
}

declare global {
  interface Window {
    ethereum?: unknown;
  }
}

const KinBridgeWidget: React.FC<KinBridgeWidgetProps> = ({
  routerAddress,
  claimAmount,
  decimals,
  tokenSymbol,
  tokenAddress,
  className,
}) => {
  const [provider, setProvider] = useState<BrowserProvider | undefined>();
  const [account, setAccount] = useState<string | null>(null);
  const [chainId, setChainId] = useState<number | null>(null);

  useEffect(() => {
    if (typeof window === "undefined" || !window.ethereum) {
      return;
    }

    const detectedProvider = new BrowserProvider(window.ethereum as any);
    setProvider(detectedProvider);

    const syncWallet = async () => {
      try {
        const signer = await detectedProvider.getSigner();
        const address = await signer.getAddress();
        setAccount(address);

        const network = await detectedProvider.getNetwork();
        setChainId(Number(network.chainId));
      } catch (err) {
        // User might not be connected yet.
      }
    };

    syncWallet();

    const handleAccountsChanged = (accounts: string[]) => {
      setAccount(accounts[0] ?? null);
    };

    const handleChainChanged = (chainHex: string) => {
      setChainId(parseInt(chainHex, 16));
    };

    const ethereum = window.ethereum as any;

    if (ethereum?.on) {
      ethereum.on("accountsChanged", handleAccountsChanged);
      ethereum.on("chainChanged", handleChainChanged);
    }

    return () => {
      if (ethereum?.removeListener) {
        ethereum.removeListener("accountsChanged", handleAccountsChanged);
        ethereum.removeListener("chainChanged", handleChainChanged);
      }
    };
  }, []);

  const requestConnection = useCallback(async () => {
    if (!provider) {
      return;
    }

    const accounts = await provider.send("eth_requestAccounts", []);
    setAccount(accounts[0] ?? null);

    const network = await provider.getNetwork();
    setChainId(Number(network.chainId));
  }, [provider]);

  const isConnected = useMemo(() => Boolean(provider && account), [account, provider]);

  return (
    <div className={className}>
      {!isConnected && (
        <button onClick={requestConnection}>Connect Wallet</button>
      )}

      <KinBridgeClaimButton
        provider={provider}
        account={account}
        chainId={chainId}
        routerAddress={routerAddress}
        claimAmount={claimAmount}
        decimals={decimals}
        tokenSymbol={tokenSymbol}
        tokenAddress={tokenAddress}
        disabled={!isConnected}
        className="kinbridge-claim"
      />
    </div>
  );
};

export default KinBridgeWidget;
