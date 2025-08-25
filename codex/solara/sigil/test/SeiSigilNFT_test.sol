// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Test.sol";
import "../../sei/protocol/kinproof_zk_sync.sol";
import "../SeiSigilNFT.sol";

contract SeiSigilNFTTest is Test {
    KinProofZkSync kin;
    SeiSigilNFT sigil;

    function setUp() public {
        kin = new KinProofZkSync(900);
        sigil = new SeiSigilNFT();
        sigil.setKinProof(address(kin));
        kin.submitMoodProof(keccak256("🔥 Ascended"));
    }

    function testMint() public {
        sigil.mint("ipfs://sigil-metadata.json");
        assertEq(sigil.ownerOf(0), address(this));
    }

    function testRoyaltyRate() public {
        assertEq(sigil.getRoyaltyRate(), 900);
    }
}
