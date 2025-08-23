// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "../key/KinKey.sol";
import "../nft/SoulSigilNFT.sol";
import "../vault/SeiVault.sol";

contract SeiWordController {
    KinKey public kinkey;
    SoulSigilNFT public sigil;
    SeiVault public vault;

    constructor(address _kinkey, address _sigil, address _vault) {
        kinkey = KinKey(_kinkey);
        sigil = SoulSigilNFT(_sigil);
        vault = SeiVault(_vault);
    }

    function fullSync(address user) external view returns (address key, bool ownsSigil, bytes32 vaultHash) {
        key = kinkey.getCurrentKey(user);
        ownsSigil = sigil.ownsSigil(user);
        vaultHash = vault.getVault(user).moodHash;
    }
}
