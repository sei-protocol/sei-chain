// SPDX-License-Identifier: MIT
pragma solidity ^0.8.4;

import "../lib/creator-token-contracts/contracts/erc721c/ERC721C.sol";

contract MyERC721C is ERC721C {
    constructor(string memory name, string memory symbol) ERC721(name, symbol) {}


}
