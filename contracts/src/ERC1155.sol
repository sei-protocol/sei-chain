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

        mintForTest(_randomAddress1, 0);
        mintForTest(_randomAddress2, 4);
    }

    function mintForTest(address recipient, uint256 startId) public virtual {
        uint256[] memory tids = new uint256[](6);
        tids[0] = startId;
        tids[1] = startId + 1;
        tids[2] = startId + 2;
        tids[3] = startId + 3;
        tids[4] = startId + 4;
        tids[5] = startId + 5;
        uint256[] memory values = new uint256[](6);
        values[0] = 10;
        values[1] = 11;
        values[2] = 12;
        values[3] = 13;
        values[4] = 14;
        values[5] = 15;
        _mintBatch(recipient, tids, values, '0x0');
    }

    function supportsInterface(bytes4 interfaceId) public view override(ERC1155, ERC2981) returns (bool) {
        return super.supportsInterface(interfaceId);
    }

    function setDefaultRoyalty(address receiver) public {
        _setDefaultRoyalty(receiver, 500);
    }
}
