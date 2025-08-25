// SPDX-License-Identifier: MIT
pragma solidity ^0.8.23;

import "forge-std/Test.sol";
import "../kinproof_zk_sync.sol";

contract KinProofTest is Test {
    KinProofZkSync kin;

    function setUp() public {
        kin = new KinProofZkSync(800);
    }

    function testSubmitMoodProof() public {
        bytes32 mood = keccak256("🔥Elevated");
        kin.submitMoodProof(mood);
        assertEq(kin.getMoodProof(address(this)), mood);
    }

    function testUpdateRoyalties() public {
        kin.updateRoyalties(1111);
        assertEq(kin.royaltyRate(), 1111);
    }
}
