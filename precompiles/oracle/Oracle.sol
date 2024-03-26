// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant ORACLE_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001008;

IOracle constant ORACLE_CONTRACT = IOracle(ORACLE_PRECOMPILE_ADDRESS);

interface IOracle {
    // Queries
    function getExchangeRates() external view returns (DenomOracleExchangeRatePair[] memory);
    function getOracleTwaps(uint64 lookback_seconds) external view returns (OracleTwap[] memory);

    // Structs
    struct OracleExchangeRate {
        string exchangeRate;
        string lastUpdate;
        int64 lastUpdateTimestamp;
    }

    struct DenomOracleExchangeRatePair {
        string denom;
        OracleExchangeRate oracleExchangeRateVal;
    }

    struct OracleTwap {
        string denom;
        string twap;
        int64 lookbackSeconds;
    }
}
