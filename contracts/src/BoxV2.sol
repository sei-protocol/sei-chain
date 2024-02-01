// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract BoxV2 {
    uint256 private _value;
    uint256 private _value2;

    event ValueChanged(uint256 value);

    function store(uint256 value) public {
        _value = value;
        emit ValueChanged(value);
    }

    function retrieve() public view returns (uint256) {
        return _value;
    }

    function boxV2Incr() public {
        _value = _value + 1;
        emit ValueChanged(_value);
    }

    function store2(uint256 value) public {
        _value2 = value;
        emit ValueChanged(value);
    }

    function retrieve2() public view returns (uint256) {
        return _value2;
    }
}