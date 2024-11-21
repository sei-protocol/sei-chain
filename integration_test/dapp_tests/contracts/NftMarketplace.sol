// SPDX-License-Identifier: MIT
pragma solidity 0.8.20;

import {IERC721} from "@openzeppelin/contracts/token/ERC721/IERC721.sol";

/*
 * @title NftMarketplace
 * @auth Patrick Collins
 * @notice This contract allows users to list NFTs for sale
 * @notice This is the reference
 */
contract NftMarketplace {
    error NftMarketplace__PriceNotMet(address nftAddress, uint256 tokenId, uint256 price);
    error NftMarketplace__NotListed(address nftAddress, uint256 tokenId);
    error NftMarketplace__NoProceeds();
    error NftMarketplace__NotOwner();
    error NftMarketplace__PriceMustBeAboveZero();
    error NftMarketplace__TransferFailed();

    event ItemListed(address indexed seller, address indexed nftAddress, uint256 indexed tokenId, uint256 price);
    event ItemUpdated(address indexed seller, address indexed nftAddress, uint256 indexed tokenId, uint256 price);
    event ItemCanceled(address indexed seller, address indexed nftAddress, uint256 indexed tokenId);
    event ItemBought(address indexed buyer, address indexed nftAddress, uint256 indexed tokenId, uint256 price);

    mapping(address nftAddress => mapping(uint256 tokenId => Listing)) private s_listings;
    mapping(address seller => uint256 proceedAmount) private s_proceeds;

    struct Listing {
        uint256 price;
        address seller;
    }

    /*//////////////////////////////////////////////////////////////
                               FUNCTIONS
    //////////////////////////////////////////////////////////////*/
    /*
     * @notice Method for listing NFT
     * @param nftAddress Address of NFT contract
     * @param tokenId Token ID of NFT
     * @param price sale price for each item
     */
    function listItem(address nftAddress, uint256 tokenId, uint256 price) external {
        // Checks
        if (price <= 0) {
            revert NftMarketplace__PriceMustBeAboveZero();
        }

        // Effects (Internal)
        s_listings[nftAddress][tokenId] = Listing(price, msg.sender);
        emit ItemListed(msg.sender, nftAddress, tokenId, price);

        // Interactions (External)
        IERC721(nftAddress).safeTransferFrom(msg.sender, address(this), tokenId);
    }

    /*
     * @notice Method for cancelling listing
     * @param nftAddress Address of NFT contract
     * @param tokenId Token ID of NFT
     *
     * @audit-known seller can front-run a bought NFT and cancel the listing
     */
    function cancelListing(address nftAddress, uint256 tokenId) external {
        // Checks
        if (msg.sender != s_listings[nftAddress][tokenId].seller) {
            revert NftMarketplace__NotOwner();
        }

        // Effects (Internal)
        delete s_listings[nftAddress][tokenId];
        emit ItemCanceled(msg.sender, nftAddress, tokenId);

        // Interactions (External)
        IERC721(nftAddress).safeTransferFrom(address(this), msg.sender, tokenId);
    }

    /*
     * @notice Method for buying listing
     * @notice The owner of an NFT could unapprove the marketplace,
     * @param nftAddress Address of NFT contract
     * @param tokenId Token ID of NFT
     */
    function buyItem(address nftAddress, uint256 tokenId) external payable {
        Listing memory listedItem = s_listings[nftAddress][tokenId];
        // Checks
        if (listedItem.seller == address(0)) {
            revert NftMarketplace__NotListed(nftAddress, tokenId);
        }
        if (msg.value < listedItem.price) {
            revert NftMarketplace__PriceNotMet(nftAddress, tokenId, listedItem.price);
        }

        // Effects (Internal)
        s_proceeds[listedItem.seller] += msg.value;
        delete s_listings[nftAddress][tokenId];
        emit ItemBought(msg.sender, nftAddress, tokenId, listedItem.price);

        // Interactions (External)
        IERC721(nftAddress).safeTransferFrom(address(this), msg.sender, tokenId);
    }

    /*
     * @notice Method for updating listing
     * @param nftAddress Address of NFT contract
     * @param tokenId Token ID of NFT
     * @param newPrice Price in Wei of the item
     *
     * @audit-known seller can front-run a bought NFT and update the listing
     */
    function updateListing(address nftAddress, uint256 tokenId, uint256 newPrice) external {
        // Checks
        if (newPrice <= 0) {
            revert NftMarketplace__PriceMustBeAboveZero();
        }
        if (msg.sender != s_listings[nftAddress][tokenId].seller) {
            revert NftMarketplace__NotOwner();
        }

        // Effects (Internal)
        s_listings[nftAddress][tokenId].price = newPrice;
        emit ItemUpdated(msg.sender, nftAddress, tokenId, newPrice);
    }

    /*
     * @notice Method for withdrawing proceeds from sales
     *
     * @audit-known, we should emit an event for withdrawing proceeds
     */
    function withdrawProceeds() external {
        uint256 proceeds = s_proceeds[msg.sender];
        // Checks
        if (proceeds <= 0) {
            revert NftMarketplace__NoProceeds();
        }
        // Effects (Internal)
        s_proceeds[msg.sender] = 0;

        // Interactions (External)
        (bool success,) = payable(msg.sender).call{value: proceeds}("");
        if (!success) {
            revert NftMarketplace__TransferFailed();
        }
    }

    function onERC721Received(address, /*operator*/ address, /*from*/ uint256, /*tokenId*/ bytes calldata /*data*/ )
        external
        pure
        returns (bytes4)
    {
        return this.onERC721Received.selector;
    }

    /*//////////////////////////////////////////////////////////////
                          VIEW/PURE FUNCTIONS
    //////////////////////////////////////////////////////////////*/
    function getListing(address nftAddress, uint256 tokenId) external view returns (Listing memory) {
        return s_listings[nftAddress][tokenId];
    }

    function getProceeds(address seller) external view returns (uint256) {
        return s_proceeds[seller];
    }
}