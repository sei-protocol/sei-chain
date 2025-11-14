// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

// Interface for Wasmd Precompile
interface IWasmd {
    function execute(
        string memory contractAddress,
        bytes memory msg,
        bytes memory coins
    ) external returns (bytes memory);
}

contract EVMCaller {
    address constant WASMD_PRECOMPILE = 0x0000000000000000000000000000000000001002;

    event CalledCW(string contractAddress, bytes response);

    // This function calls a CosmWasm contract via the wasmd precompile
    // The CosmWasm contract will then try to call an EVM contract
    // This should trigger: EVM -> CW -> EVM error
    function callCosmWasm(
        string memory cwContractAddress,
        bytes memory executeMsg
    ) external returns (bytes memory) {
        IWasmd wasmd = IWasmd(WASMD_PRECOMPILE);

        // Call the CosmWasm contract with empty coins
        bytes memory emptyCoins = abi.encodePacked("[]");
        bytes memory response = wasmd.execute(cwContractAddress, executeMsg, emptyCoins);

        emit CalledCW(cwContractAddress, response);
        return response;
    }
}