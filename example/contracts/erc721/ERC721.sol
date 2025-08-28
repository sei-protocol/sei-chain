// SPDX-License-Identifier: MIT
// OpenZeppelin Contracts (last updated v5.0.0) (token/ERC721/IERC721.sol)

pragma solidity ^0.8.20;

interface IERC165 {
    function supportsInterface(bytes4 interfaceId) external view returns (bool);
}

interface IERC721Receiver {
    function onERC721Received(
        address operator,
        address from,
        uint256 tokenId,
        bytes calldata data
    ) external returns (bytes4);
}

interface IERC721 is IERC165 {
    event Transfer(address indexed from, address indexed to, uint256 indexed tokenId);
    event Approval(address indexed owner, address indexed approved, uint256 indexed tokenId);
    event ApprovalForAll(address indexed owner, address indexed operator, bool approved);

    function balanceOf(address owner) external view returns (uint256 balance);
    function ownerOf(uint256 tokenId) external view returns (address owner);
    function safeTransferFrom(address from, address to, uint256 tokenId, bytes calldata data) external;
    function safeTransferFrom(address from, address to, uint256 tokenId) external;
    function transferFrom(address from, address to, uint256 tokenId) external;
    function approve(address to, uint256 tokenId) external;
    function setApprovalForAll(address operator, bool approved) external;
    function getApproved(uint256 tokenId) external view returns (address operator);
    function isApprovedForAll(address owner, address operator) external view returns (bool);
}

// NOT A REAL IMPLEMENTATION -- DO NOT USE IN PROD
contract DummyERC721 is IERC721 {
    mapping(uint256 => address) private _tokenOwners;
    mapping(address => uint256) private _tokenBalances;
    mapping(uint256 => address) private _tokenApprovals;
    mapping(address => mapping(address => bool)) private _operatorApprovals;

    address public randomAddress = 0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266;

    function name() external pure returns (string memory) {
        return "DummyERC721";
    }

    function symbol() external pure returns (string memory) {
        return "DUMMY";
    }

    function supportsInterface(bytes4 interfaceId) external view returns (bool) {
        return true;
    }

    function balanceOf(address owner) external view override returns (uint256 balance) {
        return 50;
    }

    function totalSupply() public view returns (uint256 supply) {
        supply = 101;
    }

    function ownerOf(uint256 tokenId) public view override returns (address owner) {
        return randomAddress;
    }

    function tokenURI(uint256 tokenId) public view returns (string memory) {
        return "https://example.com";
    }

    function royaltyInfo(uint256 tokenId, uint256 salePrice) external view returns (address receiver, uint256 royaltyAmount) {
        receiver = randomAddress;
        royaltyAmount = (salePrice * 500) / 10_000;
    }

    function transferFrom(address from, address to, uint256 tokenId) public override {}

    function safeTransferFrom(address from, address to, uint256 tokenId) public override {}

    function safeTransferFrom(address from, address to, uint256 tokenId, bytes calldata data) public override {}

    function approve(address to, uint256 tokenId) external override {}

    function getApproved(uint256 tokenId) public view override returns (address operator) {}

    function setApprovalForAll(address operator, bool approved) external override { }

    function isApprovedForAll(address owner, address operator) public view override returns (bool) {
        return true;
    }

    function _exists(uint256 tokenId) internal view returns (bool) {
        return true;
    }

    function _transfer(address from, address to, uint256 tokenId) internal {}

    function _checkOnERC721Received(address from, address to, uint256 tokenId, bytes memory data) internal returns (bool) {
        return true;
    }
}
