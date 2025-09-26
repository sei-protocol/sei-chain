// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "forge-std/Test.sol";
import {SeiKinSettlement} from "../src/SeiKinSettlement.sol";
import {Client} from "../src/ccip/Client.sol";
import {TestToken} from "../src/TestToken.sol";

contract SeiKinSettlementTest is Test {
    SeiKinSettlement private settlement;
    TestToken private token;

    address private constant ROYALTY_VAULT = address(0x9999);
    address private constant CCIP_ROUTER = address(0xAAAA);
    address private constant CCIP_SENDER = address(0xBBBB);
    address private constant CCTP_CALLER = address(0xCCCC);

    function setUp() external {
        settlement = new SeiKinSettlement(CCIP_ROUTER, ROYALTY_VAULT, CCIP_SENDER, CCTP_CALLER);
        token = new TestToken("Test", "TST");
    }

    function testCctpSettlementTransfersRoyaltyAndNetAmount() external {
        address user = address(0x1234);
        uint256 amount = 1_000_000;

        token.setBalance(address(this), amount);
        token.transfer(address(settlement), amount);

        vm.prank(CCTP_CALLER);
        settlement.onCCTPReceived(address(token), user, amount, bytes("cctp"));

        (uint256 royaltyAmount, uint256 netAmount) = settlement.royaltyInfo(amount);
        assertEq(token.balanceOf(ROYALTY_VAULT), royaltyAmount, "royalty vault should receive 8.5%");
        assertEq(token.balanceOf(user), netAmount, "user should receive net amount");
        assertEq(token.balanceOf(address(settlement)), 0, "settlement contract should be emptied");
    }

    function testCcipReceiveTransfersToOrigin() external {
        address origin = address(0xBEEF);
        uint256 amount = 500_000;
        token.setBalance(address(this), amount);
        token.transfer(address(settlement), amount);

        Client.Any2EVMMessage memory message;
        message.sender = abi.encode(CCIP_SENDER);
        message.data = abi.encode(address(token));

        vm.prank(CCIP_ROUTER, origin);
        settlement.ccipReceive(message);

        (uint256 royaltyAmount, uint256 netAmount) = settlement.royaltyInfo(amount);
        assertEq(token.balanceOf(ROYALTY_VAULT), royaltyAmount, "royalty vault should receive 8.5%");
        assertEq(token.balanceOf(origin), netAmount, "origin should receive net amount");
        assertEq(token.balanceOf(address(settlement)), 0, "settlement contract should be emptied");
    }

    function testRevertsForUntrustedCctpCaller() external {
        token.setBalance(address(this), 100);
        token.transfer(address(settlement), 100);

        vm.expectRevert(bytes("Untrusted sender"));
        settlement.onCCTPReceived(address(token), address(1), 100, "");
    }

    function testRevertsForZeroCctpAmount() external {
        vm.expectRevert(bytes("Zero amount"));
        vm.prank(CCTP_CALLER);
        settlement.onCCTPReceived(address(token), address(0x1234), 0, "");
    }

    function testRevertsForZeroCctpBeneficiary() external {
        vm.expectRevert(bytes("Zero address"));
        vm.prank(CCTP_CALLER);
        settlement.onCCTPReceived(address(token), address(0), 1, "");
    }

    function testRevertsForZeroCctpToken() external {
        vm.expectRevert(bytes("Zero token"));
        vm.prank(CCTP_CALLER);
        settlement.onCCTPReceived(address(0), address(0x1234), 1, "");
    }

    function testRevertsForUntrustedCcipSender() external {
        token.setBalance(address(this), 1000);
        token.transfer(address(settlement), 1000);

        Client.Any2EVMMessage memory message;
        message.sender = abi.encode(address(0xDEAD));
        message.data = abi.encode(address(token));

        vm.prank(CCIP_ROUTER);
        vm.expectRevert(bytes("Untrusted sender"));
        settlement.ccipReceive(message);
    }
}
