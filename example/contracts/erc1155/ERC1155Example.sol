// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/token/common/ERC2981.sol";
import "@openzeppelin/contracts/token/ERC1155/extensions/ERC1155Supply.sol";

contract ERC1155Example is ERC1155Supply,ERC2981 {
    string public name = "DummyERC1155";

    string public symbol = "DUMMY";

    string private _uri = "https://example.com/{id}";

    address private _randomAddress1 = 0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266;
    address private _randomAddress2 = 0xF39fD6e51Aad88F6f4CE6AB8827279CFffb92267;

    constructor() ERC1155(_uri) {
        setDefaultRoyalty(_randomAddress1);

        uint256[] memory tids1 = new uint256[](6);
        tids1[0] = 0;
        tids1[1] = 1;
        tids1[2] = 2;
        tids1[3] = 3;
        tids1[4] = 4;
        tids1[5] = 5;
        uint256[] memory values1 = new uint256[](6);
        values1[0] = 10;
        values1[1] = 11;
        values1[2] = 12;
        values1[3] = 13;
        values1[4] = 14;
        values1[5] = 15;
        _mintBatch(_randomAddress1, tids1, values1, '0x0');

        uint256[] memory tids2 = new uint256[](6);
        tids2[0] = 4;
        tids2[1] = 5;
        tids2[2] = 6;
        tids2[3] = 7;
        tids2[4] = 8;
        tids2[5] = 9;
        uint256[] memory values2 = new uint256[](6);
        values2[0] = 10;
        values2[1] = 11;
        values2[2] = 12;
        values2[3] = 13;
        values2[4] = 14;
        values2[5] = 15;
        _mintBatch(_randomAddress2, tids2, values2, '0x0');
    }

    function supportsInterface(bytes4 interfaceId) public view override(ERC1155, ERC2981) returns (bool) {
        return super.supportsInterface(interfaceId);
    }

    function setDefaultRoyalty(address receiver) public {
        _setDefaultRoyalty(receiver, 500);
    }
}
