// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

interface IMultiRewardDistributor {
    function updateMarketBorrowIndexAndDisburseBorrowerRewards(
        address tToken,
        address borrower,
        bool autoClaim
    ) external;

    function updateMarketSupplyIndexAndDisburseSupplierRewards(
        address tToken,
        address supplier,
        bool autoClaim
    ) external;
}

/// @title MultiRewardHookAdapter
/// @notice Helper contract that forwards borrow/mint lifecycle hooks to a MultiRewardDistributor.
/// @dev Deploy alongside a Comptroller or hook-enabled market to keep reward indices fresh.
contract MultiRewardHookAdapter {
    /// @notice Immutable reference to the rewards distributor.
    address public immutable distributor;

    error InvalidDistributor();

    constructor(address distributor_) {
        if (distributor_ == address(0)) revert InvalidDistributor();
        distributor = distributor_;
    }

    /// @notice Forward the borrow verify hook to the distributor so borrower rewards stay current.
    /// @param tToken Address of the market that triggered the hook.
    /// @param borrower Account whose debt changed.
    /// @param /* borrowAmount */ uint256 borrowAmount Ignored but preserved for Comptroller signature compatibility.
    function borrowVerify(address tToken, address borrower, uint256 /* borrowAmount */) external {
        IMultiRewardDistributor(distributor).updateMarketBorrowIndexAndDisburseBorrowerRewards(
            tToken,
            borrower,
            true
        );
    }

    /// @notice Forward the mint verify hook to the distributor so supplier rewards stay current.
    /// @param tToken Address of the market that triggered the hook.
    /// @param minter Account whose supply changed.
    /// @param /* mintAmount */ uint256 mintAmount Ignored but preserved for Comptroller signature compatibility.
    /// @param /* tokensMinted */ uint256 tokensMinted Ignored but preserved for Comptroller signature compatibility.
    function mintVerify(address tToken, address minter, uint256 /* mintAmount */, uint256 /* tokensMinted */) external {
        IMultiRewardDistributor(distributor).updateMarketSupplyIndexAndDisburseSupplierRewards(
            tToken,
            minter,
            true
        );
    }
}
