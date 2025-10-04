# RFC-004: Vault Enforcement — Guardian Flows for Royalty Custody

## Summary
Captures the operational controls for the SeiKin Royalty Vault, ensuring compliant custody and disbursement of protocol royalties. The vault acts as the canonical store for attributed proceeds collected by SeiKinSettlement.

## Vault Controls
- **Guardian Committee:** Multi-signature guardians authorize withdrawals based on published schedules.
- **Escrow Monitoring:** Automated checks confirm escrow balances before approving new settlement routes.
- **Dispute Resolution:** Evidence of breach routes through the guardian committee with on-chain transparency.

## Operational Hooks
Guardian scripts integrate with `SeiKinVaultBalanceCheck.sh` and other monitoring utilities to broadcast vault status. Deviations from configured thresholds must trigger remediation workflows.
