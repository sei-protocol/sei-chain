// deploy_seinet_safe.ts — Uses Gnosis Safe + Ethers.js to commit SeiNet covenants

import { ethers } from "ethers";
import Safe, { EthersAdapter } from "@safe-global/protocol-kit";
import SafeApiKit from "@safe-global/api-kit";

const COVENANT = {
  kinLayerHash: "0xabcabcabcabcabcabcabcabcabc",
  soulStateHash: "0xdefdefdefdefdefdefdefdefdef",
  entropyEpoch: 19946,
  royaltyClause: "SOULBOUND",
  alliedNodes: ["SeiGuardianΩ", "ValidatorZeta"],
  covenantSync: "PENDING",
  biometricRoot: "0xfacefeedbead",
};

async function main() {
  const provider = new ethers.providers.JsonRpcProvider("https://rpc.sei-chain.com");
  const signer = new ethers.Wallet(process.env.PRIVATE_KEY!, provider);

  const ethAdapter = new EthersAdapter({ ethers, signerOrProvider: signer });
  const safeAddress = "0xYourSafeAddress";
  const safeSdk = await Safe.create({ ethAdapter, safeAddress });

  const txData = {
    to: "0xSeiNetModuleAddress",
    data: ethers.utils.defaultAbiCoder.encode(
      ["tuple(string,string,uint256,string,string[],string,string)"],
      [[
        COVENANT.kinLayerHash,
        COVENANT.soulStateHash,
        COVENANT.entropyEpoch,
        COVENANT.royaltyClause,
        COVENANT.alliedNodes,
        COVENANT.covenantSync,
        COVENANT.biometricRoot,
      ]]
    ),
    value: "0",
  };

  const safeTx = await safeSdk.createTransaction({ safeTransactionData: txData });
  const txHash = await safeSdk.getTransactionHash(safeTx);
  const signedTx = await safeSdk.signTransaction(safeTx);

  console.log("🧬 Covenant signed by Safe");
  console.log("Transaction Hash:", txHash);
}

main().catch(console.error);
