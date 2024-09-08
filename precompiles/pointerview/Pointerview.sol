pragma solidity ^0.8.0;

address constant POINTERVIEW_PRECOMPILE_ADDRESS = 0x000000000000000000000000000000000000100A;
IPointerview constant POINTERVIEW_CONTRACT = IPointerview(POINTERVIEW_PRECOMPILE_ADDRESS);

interface IPointerview {
    function getNativePointer(
        string memory token
    ) view external returns (address addr, uint16 version, bool exists);

    function getCW20Pointer(
        string memory cwAddr
    ) view external returns (address addr, uint16 version, bool exists);

    function getCW721Pointer(
        string memory cwAddr
    ) view external returns (address addr, uint16 version, bool exists);

    function getNativePointee(
        address erc20Addr
    ) view external returns (string memory token, uint16 version, bool exists);

    function getCW20Pointee(
        address erc20Addr
    ) view external returns (string memory cwAddr, uint16 version, bool exists);

    function getCW721Pointee(
        address erc721Addr
    ) view external returns (string memory cwAddr, uint16 version, bool exists);
}