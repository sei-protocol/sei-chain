// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.0;

import {Test, console2} from "forge-std/Test.sol";
import {CW1155ERC1155Pointer} from "../src/CW1155ERC1155Pointer.sol";
import {IWasmd} from "../src/precompiles/IWasmd.sol";
import {IJson} from "../src/precompiles/IJson.sol";
import {IAddr} from "../src/precompiles/IAddr.sol";
import "@openzeppelin/contracts/utils/Strings.sol";
import "@openzeppelin/contracts/interfaces/draft-IERC6093.sol";

address constant WASMD_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001002;
address constant JSON_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001003;
address constant ADDR_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001004;

address constant MockCallerEVMAddr = 0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266;
address constant MockOperatorEVMAddr = 0xF39fD6e51Aad88F6f4CE6AB8827279CFffb92267;
address constant MockZeroAddress = 0x0000000000000000000000000000000000000000;

string constant MockCallerSeiAddr = "sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw";
string constant MockOperatorSeiAddr = "sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe";
string constant MockCWContractAddress = "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m";

contract MockWasmd is IWasmd {

    // Transactions
    function instantiate(
        uint64,
        string memory,
        bytes memory,
        string memory,
        bytes memory
    ) external pure returns (string memory, bytes memory) {
        return (MockCWContractAddress, bytes(""));
    }

    function execute(
        string memory contractAddress,
        bytes memory,
        bytes memory
    ) external pure returns (bytes memory) {
        require(keccak256(abi.encodePacked(contractAddress)) == keccak256(abi.encodePacked(MockCWContractAddress)), "wrong CW contract address");
        return bytes("");
    }

    // Queries
    function query(string memory, bytes memory) external pure returns (bytes memory) {
        return bytes("");
    }
}

contract MockJson is IJson {
    function extractAsBytes(bytes memory, string memory) external pure returns (bytes memory) {
        return bytes("extracted bytes");
    }

    function extractAsBytesList(bytes memory, string memory) external pure returns (bytes[] memory) {
        return new bytes[](0);
    }

    function extractAsUint256(bytes memory input, string memory key) external view returns (uint256 response) {
        return 0;
    }
}

contract MockAddr is IAddr {
    function getSeiAddr(address addr) external pure returns (string memory) {
        if (addr == MockCallerEVMAddr) {
            return MockCallerSeiAddr;
        }
        return MockOperatorSeiAddr;
    }

    function getEvmAddr(string memory addr) external pure returns (address) {
        if (keccak256(abi.encodePacked(addr)) == keccak256(abi.encodePacked(MockCallerSeiAddr))) {
            return MockCallerEVMAddr;
        }
        return MockOperatorEVMAddr;
    }
}

contract CW1155ERC1155PointerTest is Test {

    event TransferSingle(address indexed operator, address indexed from, address indexed to, uint256 id, uint256 value);
    event TransferBatch(address indexed operator, address indexed from, address indexed to, uint256[] ids, uint256[] values);
    event ApprovalForAll(address indexed owner, address indexed operator, bool approved);

    CW1155ERC1155Pointer pointer;
    MockWasmd mockWasmd;
    MockJson mockJson;
    MockAddr mockAddr;

    function setUp() public {
        pointer = new CW1155ERC1155Pointer(MockCWContractAddress, "Test", "TEST");
        mockWasmd = new MockWasmd();
        mockJson = new MockJson();
        mockAddr = new MockAddr();
        vm.etch(WASMD_PRECOMPILE_ADDRESS, address(mockWasmd).code);
        vm.etch(JSON_PRECOMPILE_ADDRESS, address(mockJson).code);
        vm.etch(ADDR_PRECOMPILE_ADDRESS, address(mockAddr).code);
    }

    function testBalanceOf() public {
        bytes memory queryCall = bytes("{\"balance_of\":{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"token_id\":\"1\"}}");
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall),
            abi.encode("{\"balance\":\"1\"}")
        );
        vm.expectCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall)
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsUint256(bytes,string)", bytes("{\"balance\":\"1\"}"), "balance"),
            abi.encode(1)
        );
        assertEq(pointer.balanceOf(MockCallerEVMAddr, 1), 1);
    }
    
    function testBalanceOfZeroAddress() public {
        vm.expectRevert(bytes("ERC1155: cannot query balance of zero address"));
        pointer.balanceOf(MockZeroAddress, 1);
    }

    function testBalanceOfBatch() public {
        vm.mockCall(
            ADDR_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("getSeiAddr(string)", "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"),
            abi.encode(MockCallerSeiAddr)
        );
        bytes memory queryCall = bytes("{\"balance_of_batch\":{\"owner_tokens\":[{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"token_id\":\"1\"},{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"token_id\":\"2\"},{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"token_id\":\"3\"}]}}");
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall),
            abi.encode("{\"balances\":[ { \"owner\": \"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\", \"token_id\": \"1\", \"amount\": \"1\" }, { \"owner\": \"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\", \"token_id\": \"2\", \"amount\": \"2\" }, { \"owner\": \"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\", \"token_id\": \"3\", \"amount\": \"0\" } ]}")
        );
        vm.expectCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall)
        );

        bytes[] memory resp = new bytes[](3);
        resp[0] = bytes("{\"owner\": \"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\", \"token_id\": \"1\", \"amount\": \"1\"}");
        resp[1] = bytes("{\"owner\": \"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\", \"token_id\": \"2\", \"amount\": \"2\"}");
        resp[2] = bytes("{\"owner\": \"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\", \"token_id\": \"3\", \"amount\": \"0\"}");

        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsBytesList(bytes,string)",bytes("{\"balances\":[ { \"owner\": \"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\", \"token_id\": \"1\", \"amount\": \"1\" }, { \"owner\": \"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\", \"token_id\": \"2\", \"amount\": \"2\" }, { \"owner\": \"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\", \"token_id\": \"3\", \"amount\": \"0\" } ]}"), "balances"),
            abi.encode(resp)
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsUint256(bytes,string)", resp[0], "amount"),
            abi.encode(1)
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsUint256(bytes,string)", resp[1], "amount"),
            abi.encode(2)
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsUint256(bytes,string)", resp[2], "amount"),
            abi.encode(0)
        );
    
        address[] memory owners = new address[](3);
        uint256[] memory ids = new uint256[](3);
        owners[0] = MockCallerEVMAddr;
        owners[1] = MockCallerEVMAddr;
        owners[2] = MockCallerEVMAddr;
        ids[0] = 1;
        ids[1] = 2;
        ids[2] = 3;
        uint256[] memory expectedResp = new uint256[](3);
        expectedResp[0] = 1;
        expectedResp[1] = 2;
        expectedResp[2] = 0;
        assertEq(pointer.balanceOfBatch(owners, ids), expectedResp);
    }

    function testBalanceOfBatchBadLength() public {
        uint256 idsLength = 1;
        uint256 valuesLength = 0;
        vm.expectRevert(
            abi.encodeWithSelector(
                IERC1155Errors.ERC1155InvalidArrayLength.selector,
                idsLength,
                valuesLength
            )
        );
        pointer.balanceOfBatch(new address[](valuesLength), new uint256[](idsLength));
    }

    function testUri() public {
        bytes memory queryCall = bytes("{\"token_info\":{\"token_id\":\"1\"}}");
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall),
            abi.encode("{\"extension\": { \"animation_url\": null, \"attributes\": null, \"background_color\": null, \"description\": null, \"external_url\": null, \"image\": null, \"image_data\": null, \"name\": null, \"royalty_payment_address\": null, \"royalty_percentage\": null, \"youtube_url\": null }, \"token_uri\": \"test\" }")
        );
        vm.expectCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall)
        );
        
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsBytes(bytes,string)", bytes("{\"extension\": { \"animation_url\": null, \"attributes\": null, \"background_color\": null, \"description\": null, \"external_url\": null, \"image\": null, \"image_data\": null, \"name\": null, \"royalty_payment_address\": null, \"royalty_percentage\": null, \"youtube_url\": null }, \"token_uri\": \"test\" }"), "token_uri"),
            abi.encode(bytes("test"))
        );
        assertEq(pointer.uri(1), "test");
    }

    function testIsApprovedForAll() public {
        // test response for approved operator
        bytes memory queryCall1 = bytes("{\"is_approved_for_all\":{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"operator\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\"}}");
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall1),
            abi.encode("{\"approved\":true}")
        );
        vm.expectCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall1)
        );
        // test response for unapproved operator
        bytes memory queryCall2 = bytes("{\"is_approved_for_all\":{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"operator\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\"}}");
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall2),
            abi.encode("{\"approved\":false}")
        );
        vm.expectCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall2)
        );

        assertEq(pointer.isApprovedForAll(MockCallerEVMAddr, MockOperatorEVMAddr), true);
        assertEq(pointer.isApprovedForAll(MockCallerEVMAddr, MockCallerEVMAddr), false);
    }

    function testRoyaltyInfo() public {
        bytes memory queryCall1 = bytes("{\"extension\":{\"msg\":{\"check_royalties\":{}}}}");
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall1),
            abi.encode("{\"royalty_payments\":true}")
        );
        vm.expectCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall1)
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsBytes(bytes,string)", bytes("{\"royalty_payments\":true}"), "royalty_payments"),
            abi.encode("true")
        );
        bytes memory queryCall2 = bytes("{\"extension\":{\"msg\":{\"royalty_info\":{\"token_id\":\"1\",\"sale_price\":\"1000\"}}}}");
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall2),
            abi.encode("{\"address\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"royalty_amount\":\"10\"}")
        );
        vm.expectCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall2)
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsBytes(bytes,string)", bytes("{\"address\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"royalty_amount\":\"10\"}"), "address"),
            abi.encode("sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw")
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsUint256(bytes,string)", bytes("{\"address\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"royalty_amount\":\"10\"}"), "royalty_amount"),
            abi.encode(10)
        );
        vm.mockCall(
            ADDR_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("getEvmAddr(string)", "sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw"),
            abi.encode(address(MockCallerEVMAddr))
        );
        (address recipient, uint256 royalties) = pointer.royaltyInfo(1, 1000);
        assertEq(recipient, address(MockCallerEVMAddr));
        assertEq(royalties, 10);
    }

    function testSafeTransferFrom() public {
        bytes memory queryCall = bytes("{\"balance_of\":{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"token_id\":\"1\"}}");
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall),
            abi.encode("{\"balance\":\"1\"}")
        );
        vm.expectCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall)
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsUint256(bytes,string)", bytes("{\"balance\":\"1\"}"), "balance"),
            abi.encode(1)
        );
        vm.mockCall(
            ADDR_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("getEvmAddr(string)", "sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw"),
            abi.encode(address(MockCallerEVMAddr))
        );
        vm.mockCall(
            ADDR_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("getEvmAddr(string)", "sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe"),
            abi.encode(address(MockOperatorEVMAddr))
        );

        bytes memory executeCall = bytes("{\"send\":{\"from\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"to\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\",\"token_id\":\"1\",\"amount\":\"1\"}}");
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("execute(string,bytes,bytes)", MockCWContractAddress, executeCall),
            abi.encode(bytes(""))
        );
        vm.expectCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("execute(string,bytes,bytes)", MockCWContractAddress, executeCall, bytes("[]"))
        );

        vm.expectEmit();
        emit TransferSingle(MockCallerEVMAddr, MockCallerEVMAddr, MockOperatorEVMAddr, 1, 1);
        vm.startPrank(MockCallerEVMAddr);
        pointer.safeTransferFrom(MockCallerEVMAddr, MockOperatorEVMAddr, 1, 1, bytes(""));
        vm.stopPrank();
    }

    function testSafeTransferFromWithOperator() public {
        bytes memory queryCall1 = bytes("{\"balance_of\":{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"token_id\":\"1\"}}");
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall1),
            abi.encode("{\"balance\":\"1\"}")
        );
        vm.expectCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall1)
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsUint256(bytes,string)", bytes("{\"balance\":\"1\"}"), "balance"),
            abi.encode(1)
        );
        bytes memory queryCall2 = bytes("{\"is_approved_for_all\":{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"operator\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\"}}");
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall2),
            abi.encode("{\"approved\":true}")
        );
        vm.expectCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall2)
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsUint256(bytes,string)", bytes("{\"approved\":true}"), "approved"),
            abi.encode(true)
        );
        vm.mockCall(
            ADDR_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("getEvmAddr(string)", "sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw"),
            abi.encode(address(MockCallerEVMAddr))
        );
        vm.mockCall(
            ADDR_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("getEvmAddr(string)", "sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe"),
            abi.encode(address(MockOperatorEVMAddr))
        );
        bytes memory executeCall =  bytes("{\"send\":{\"from\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"to\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\",\"token_id\":\"1\",\"amount\":\"1\"}}");
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("execute(string,bytes,bytes)", MockCWContractAddress,executeCall),
            abi.encode(bytes(""))
        );
        vm.expectCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("execute(string,bytes,bytes)", MockCWContractAddress, executeCall, bytes("[]"))
        );

        vm.expectEmit();
        emit TransferSingle(MockOperatorEVMAddr, MockCallerEVMAddr, MockOperatorEVMAddr, 1, 1);
        vm.startPrank(MockOperatorEVMAddr);
        pointer.safeTransferFrom(MockCallerEVMAddr, MockOperatorEVMAddr, 1, 1, bytes(""));
        vm.stopPrank();
    }

    function testSafeBatchTransferFrom() public {
        bytes memory queryCall = bytes("{\"balance_of_batch\":{\"owner_tokens\":[{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"token_id\":\"1\"},{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"token_id\":\"2\"}]}}");
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall),
            abi.encode("{\"balances\":[ { \"owner\": \"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\", \"token_id\": \"1\", \"amount\": \"1\" }, { \"owner\": \"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\", \"token_id\": \"2\", \"amount\": \"2\" } ]}")
        );
        vm.expectCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, queryCall)
        );
        bytes[] memory resp = new bytes[](2);
        resp[0] = bytes("{\"owner\": \"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\", \"token_id\": \"1\", \"amount\": \"1\"}");
        resp[1] = bytes("{\"owner\": \"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\", \"token_id\": \"2\", \"amount\": \"2\"}");

        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsBytesList(bytes,string)",bytes("{\"balances\":[ { \"owner\": \"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\", \"token_id\": \"1\", \"amount\": \"1\" }, { \"owner\": \"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\", \"token_id\": \"2\", \"amount\": \"2\" } ]}"), "balances"),
            abi.encode(resp)
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsUint256(bytes,string)", resp[0], "amount"),
            abi.encode(1)
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsUint256(bytes,string)", resp[1], "amount"),
            abi.encode(2)
        );
    
        vm.mockCall(
            ADDR_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("getEvmAddr(string)", "sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw"),
            abi.encode(address(MockCallerEVMAddr))
        );
        vm.mockCall(
            ADDR_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("getEvmAddr(string)", "sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe"),
            abi.encode(address(MockOperatorEVMAddr))
        );

        bytes memory executeCall = bytes("{\"send_batch\":{\"from\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"to\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\",\"batch\":[{\"token_id\":\"1\",\"amount\":\"1\"},{\"token_id\":\"2\",\"amount\":\"2\"}]}}");
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("execute(string,bytes,bytes)", MockCWContractAddress, executeCall),
            abi.encode(bytes(""))
        );
        vm.expectCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("execute(string,bytes,bytes)", MockCWContractAddress, executeCall, bytes("[]"))
        );

        uint256[] memory ids = new uint256[](2);
        uint256[] memory amounts = new uint256[](2);
        ids[0] = 1;
        ids[1] = 2;
        amounts[0] = 1;
        amounts[1] = 2;
        vm.expectEmit();
        emit TransferBatch(MockCallerEVMAddr, MockCallerEVMAddr, MockOperatorEVMAddr, ids, amounts);
        vm.startPrank(MockCallerEVMAddr);
        pointer.safeBatchTransferFrom(MockCallerEVMAddr, MockOperatorEVMAddr, ids, amounts, bytes(""));
        vm.stopPrank();
    }

    function testSetApprovalForAll() public {
        bytes memory executeCall = bytes("{\"approve_all\":{\"operator\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\"}}");
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("execute(string,bytes,bytes)", MockCWContractAddress, executeCall, bytes("[]")),
            abi.encode(bytes(""))
        );
        vm.expectCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("execute(string,bytes,bytes)", MockCWContractAddress, executeCall, bytes("[]"))
        );
        vm.mockCall(
            ADDR_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("getEvmAddr(string)", "sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe"),
            abi.encode(address(MockOperatorEVMAddr))
        );
        vm.startPrank(MockCallerEVMAddr);
        vm.expectEmit();
        emit ApprovalForAll(MockCallerEVMAddr, MockOperatorEVMAddr, true);
        pointer.setApprovalForAll(MockOperatorEVMAddr, true);
        vm.stopPrank();
    }
}