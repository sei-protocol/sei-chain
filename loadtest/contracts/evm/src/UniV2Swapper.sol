// SPDX-License-Identifier: MIT
import "./univ2/UniswapV2Router.sol";
import "./univ2/interfaces/IERC20.sol";

pragma solidity ^0.8.0;

// UniV2Swapper facilitates swapping between two tokens on UniswapV2. It is used by our loadtest
// to easily allow any of our evm tx clients to execute a swap on UniswapV2. Without it, we would
// need to mint tokens and approve the UniV2Router contract for each of our evm tx clients, which
// scales linearly with the number of clients, making the setup process very slow.
contract UniV2Swapper {
    uint256 public constant BIG_NUMBER = 100000000000000000000000000000000000000000000000000; // 10^50
    address public t1;
    address public t2;
    address public uniV2Router;
    constructor(address t1_, address t2_, address uniV2Router_) {
        t1 = t1_;
        t2 = t2_;
        uniV2Router = uniV2Router_;
        IERC20(t1).mint(address(this), BIG_NUMBER);
        IERC20(t2).mint(address(this), BIG_NUMBER);
        IERC20(t1).approve(uniV2Router, BIG_NUMBER);
        IERC20(t2).approve(uniV2Router, BIG_NUMBER);
    }

    function swap() public {
        address[] memory tokenPath = new address[](2);
        uint256 randomNum = uint256(keccak256(abi.encodePacked(block.timestamp)));
        // randomly either swap t1 for t2 or t2 for t1
        if (randomNum % 2 == 0) {
            tokenPath[0] = t1;
            tokenPath[1] = t2;
        } else {
            tokenPath[0] = t2;
            tokenPath[1] = t1;
        }
        UniswapV2Router(uniV2Router).swapExactTokensForTokens(
            100,
            0,
            tokenPath,
            address(this),
            block.timestamp + 1
        );
    }
}