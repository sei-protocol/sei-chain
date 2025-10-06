// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

import "forge-std/Test.sol";

import {ISoulMoodOracle} from "../src/interfaces/ISoulMoodOracle.sol";
import {SeiMeshPresenceEntangler} from "../src/SeiMeshPresenceEntangler.sol";

contract MockSoulMoodOracle is ISoulMoodOracle {
    error MoodUnset();

    mapping(address => string) private _moods;

    function setMood(address soul, string memory mood) external {
        _moods[soul] = mood;
    }

    function moodOf(address soul) external view override returns (string memory) {
        string memory mood = _moods[soul];
        if (bytes(mood).length == 0) revert MoodUnset();
        return mood;
    }
}

contract SeiMeshPresenceEntanglerTest is Test {
    MockSoulMoodOracle private oracle;
    SeiMeshPresenceEntangler private entangler;

    address private constant VALIDATOR = address(0xBEEF);
    address private constant SOUL = address(0xCAFE);
    bytes32 private constant SSID_HASH = keccak256("meshSSIDHash");

    function setUp() public {
        oracle = new MockSoulMoodOracle();
        entangler = new SeiMeshPresenceEntangler(VALIDATOR, SSID_HASH, oracle);
    }

    function testValidatorEntanglesAndProofVerifies() public {
        oracle.setMood(SOUL, "serene");

        vm.prank(VALIDATOR);
        bytes32 proofId = entangler.entangle(SOUL, 1);

        (SeiMeshPresenceEntangler.Entanglement memory active, bool isActive) =
            entangler.currentEntanglement();

        assertTrue(isActive, "entanglement should be active");
        assertEq(active.soul, SOUL, "soul mismatch");
        assertEq(active.nonce, 1, "nonce mismatch");
        assertEq(active.proofId, proofId, "proof mismatch");
        assertEq(active.mood, "serene", "mood mismatch");
        assertTrue(entangler.verifyProof(SOUL, "serene", 1), "proof should verify");
    }

    function testNonValidatorCannotEntangle() public {
        oracle.setMood(SOUL, "focused");
        vm.expectRevert(SeiMeshPresenceEntangler.Unauthorized.selector);
        entangler.entangle(SOUL, 7);
    }

    function testValidatorCannotEntangleTwiceWithoutRelease() public {
        oracle.setMood(SOUL, "calm");
        vm.prank(VALIDATOR);
        entangler.entangle(SOUL, 2);

        vm.prank(VALIDATOR);
        vm.expectRevert(SeiMeshPresenceEntangler.AlreadyEntangled.selector);
        entangler.entangle(SOUL, 3);
    }

    function testReleaseClearsActiveState() public {
        oracle.setMood(SOUL, "joyful");
        vm.prank(VALIDATOR);
        entangler.entangle(SOUL, 4);

        vm.prank(VALIDATOR);
        entangler.release();

        (, bool isActive) = entangler.currentEntanglement();
        assertFalse(isActive, "entanglement should be cleared");
        assertTrue(entangler.verifyProof(SOUL, "joyful", 4), "proof remains committed");
    }

    function testProofCannotBeReused() public {
        oracle.setMood(SOUL, "vibrant");
        vm.prank(VALIDATOR);
        entangler.entangle(SOUL, 10);

        vm.prank(VALIDATOR);
        entangler.release();

        oracle.setMood(SOUL, "vibrant");
        vm.prank(VALIDATOR);
        vm.expectRevert(SeiMeshPresenceEntangler.ProofAlreadyCommitted.selector);
        entangler.entangle(SOUL, 10);
    }
}
