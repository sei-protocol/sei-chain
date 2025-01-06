pragma solidity ^0.8.0;

address constant CT_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001010;

ICT constant CT_CONTRACT = ICT(
    CT_PRECOMPILE_ADDRESS
);

interface ICT {
    // Transactions
    function initializeAccount(
        string fromAddress,
        string denom,
        bytes publicKey,
        string decryptableBalance,
        bytes pendingBalanceLo,
        bytes pendingBalanceHi,
        bytes availableBalance,
        bytes proofs
    ) external returns (bool success);

    function transfer(
        string fromAddress,
        string toAddress,
        string denom,
        bytes fromAmountLo,
        bytes fromAmountHi,
        bytes toAmountLo,
        bytes toAmountHi,
        bytes remainingBalance,
        bytes proofs
    ) external returns (bool success);

    function transferWithAuditors(
        string fromAddress,
        string toAddress,
        string denom,
        bytes fromAmountLo,
        bytes fromAmountHi,
        bytes toAmountLo,
        bytes toAmountHi,
        bytes remainingBalance,
        bytes proofs,
        Auditor[] auditors
    ) external returns (bool success);

    struct Auditor {
        string auditorAddress;
        bytes encryptedTransferAmountLo;
        bytes encryptedTransferAmountHi;
        bytes transferAmountLoValidityProof;
        bytes transferAmountHiValidityProof;
        bytes transferAmountLoEqualityProof;
        bytes transferAmountHiEqualityProof;
    }
}
