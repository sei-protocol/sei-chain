// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant EVIDENCE_PRECOMPILE_ADDRESS = 0x000000000000000000000000000000000000100f;

IEvidence constant EVIDENCE_CONTRACT = IEvidence(EVIDENCE_PRECOMPILE_ADDRESS);

interface IEvidence {
    // Queries
    // Returns the JSON encoding of the evidence with the given hash.
    function evidence(bytes memory evidenceHash) external view returns (bytes memory);

    // Returns all evidence, each entry JSON-encoded, with pagination.
    function allEvidence(bytes memory pageKey) external view returns (AllEvidenceResponse memory response);

    // Structs
    struct AllEvidenceResponse {
        bytes[] evidenceList;
        bytes nextKey;
    }
}
