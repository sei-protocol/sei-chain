// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant MINT_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001012;

IMint constant MINT_CONTRACT = IMint(MINT_PRECOMPILE_ADDRESS);

interface IMint {
    // Queries
    function params() external view returns (MintParams memory);
    function minter() external view returns (Minter memory);

    // Structs
    struct ScheduledTokenRelease {
        string startDate;
        string endDate;
        uint64 tokenReleaseAmount;
    }

    struct MintParams {
        string mintDenom;
        ScheduledTokenRelease[] tokenReleaseSchedule;
    }

    struct Minter {
        string startDate;
        string endDate;
        string denom;
        uint64 totalMintAmount;
        uint64 remainingMintAmount;
        uint64 lastMintAmount;
        string lastMintDate;
        uint64 lastMintHeight;
    }
}
