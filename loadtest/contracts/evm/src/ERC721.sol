// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "../lib/creator-token-contracts/contracts/erc721c/ERC721C.sol";

contract MyNFT is ERC721 {
    function mint(address to, uint id) external {
        _mint(to, id);
    }

    function burn(uint id) external {
        require(msg.sender == _ownerOf[id], "not owner");
        _burn(id);
    }
}