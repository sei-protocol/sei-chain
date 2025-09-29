// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface IERC20 {
    function transferFrom(address sender, address recipient, uint256 amount) external returns (bool);
    function transfer(address recipient, uint256 amount) external returns (bool);
    function approve(address spender, uint256 amount) external returns (bool);
}

interface IRoyaltyPaymaster {
    function enforceRoyalty(uint256 amount) external returns (uint256 royaltyAmount);
}

contract KinVaultRouter {
    address public immutable usdcToken;
    address public royaltyPaymaster;
    address public owner;

    mapping(address => bool) public allowedDestinations;
    bool private _routing;

    event Routed(address indexed from, address indexed to, uint256 netAmount, uint256 royaltyAmount);
    event AllowedDestinationUpdated(address indexed destination, bool allowed);
    event RoyaltyPaymasterUpdated(address indexed newPaymaster);

    error Unauthorized();
    error InvalidAmount();
    error DestinationNotAllowed();
    error ReentrantCall();
    error CallFailed(bytes returndata);

    modifier onlyOwner() {
        if (msg.sender != owner) {
            revert Unauthorized();
        }
        _;
    }

    modifier nonReentrant() {
        if (_routing) {
            revert ReentrantCall();
        }
        _routing = true;
        _;
        _routing = false;
    }

    constructor(address _usdc, address _royaltyPaymaster) {
        require(_usdc != address(0), "USDC required");
        require(_royaltyPaymaster != address(0), "Paymaster required");
        usdcToken = _usdc;
        royaltyPaymaster = _royaltyPaymaster;
        owner = msg.sender;
    }

    function route(address recipient, uint256 amount) external nonReentrant {
        _route(recipient, amount, "");
    }

    function routeWithCalldata(address target, uint256 amount, bytes calldata data) external nonReentrant {
        require(data.length > 0, "Data required");
        _route(target, amount, data);
    }

    function updatePaymaster(address newPaymaster) external onlyOwner {
        require(newPaymaster != address(0), "Invalid paymaster");
        royaltyPaymaster = newPaymaster;
        emit RoyaltyPaymasterUpdated(newPaymaster);
    }

    function setAllowedDestination(address destination, bool allowed) external onlyOwner {
        allowedDestinations[destination] = allowed;
        emit AllowedDestinationUpdated(destination, allowed);
    }

    function _route(address target, uint256 amount, bytes memory data) internal {
        if (amount == 0) {
            revert InvalidAmount();
        }
        if (!allowedDestinations[target]) {
            revert DestinationNotAllowed();
        }

        IERC20 usdc = IERC20(usdcToken);

        require(usdc.transferFrom(msg.sender, address(this), amount), "USDC in failed");

        uint256 royalty = IRoyaltyPaymaster(royaltyPaymaster).enforceRoyalty(amount);
        uint256 netAmount = amount - royalty;

        if (royalty > 0) {
            require(usdc.transfer(royaltyPaymaster, royalty), "Royalty transfer failed");
        }

        if (data.length == 0) {
            require(usdc.transfer(target, netAmount), "USDC out failed");
        } else {
            _safeApprove(usdc, target, netAmount);
            (bool success, bytes memory returndata) = target.call(data);
            _safeApprove(usdc, target, 0);
            if (!success) {
                revert CallFailed(returndata);
            }
        }

        emit Routed(msg.sender, target, netAmount, royalty);
    }

    function _safeApprove(IERC20 token, address spender, uint256 amount) private {
        require(token.approve(spender, 0), "Approve reset failed");
        if (amount > 0) {
            require(token.approve(spender, amount), "Approve failed");
        }
    }
}
