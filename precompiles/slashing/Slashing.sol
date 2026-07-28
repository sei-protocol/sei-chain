// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant SLASHING_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001014;

ISlashing constant SLASHING_CONTRACT = ISlashing(SLASHING_PRECOMPILE_ADDRESS);

interface ISlashing {
    // Queries
    function params() external view returns (SlashingParams memory params);

    function signingInfo(
        string memory consAddress
    ) external view returns (SigningInfo memory signingInfo);

    function signingInfos(
        bytes memory pageKey
    ) external view returns (SigningInfosResponse memory response);

    // Structs
    struct SlashingParams {
        int64 signedBlocksWindow;
        string minSignedPerWindow;
        uint64 downtimeJailDuration;
        string slashFractionDoubleSign;
        string slashFractionDowntime;
    }

    struct SigningInfo {
        string validatorAddress;
        int64 startHeight;
        int64 indexOffset;
        int64 jailedUntil;
        bool tombstoned;
        int64 missedBlocksCounter;
    }

    struct SigningInfosResponse {
        SigningInfo[] signingInfos;
        bytes nextKey;
    }
}
