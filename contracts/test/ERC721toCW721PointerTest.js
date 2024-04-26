const {setupSigners, deployErc721PointerForCw721, getAdmin, storeWasm, instantiateWasm} = require("./lib");
const {expect} = require("chai");

const CW20_BASE_WASM_LOCATION = "../contracts/wasm/cw721_base.wasm";

const erc721Abi = [
    "function name() view returns (string)",
    "function symbol() view returns (string)",
    "function totalSupply() view returns (uint256)",
    "function balanceOf(address owner) view returns (uint256 balance)",
    "function ownerOf(uint256 tokenId) view returns (address owner)",
    "function getApproved(uint256 tokenId) view returns (address operator)",
    "function isApprovedForAll(address owner, address operator) view returns (bool)",
    "function approve(address to, uint256 tokenId) returns (bool)",
    "function setApprovalForAll(address operator, bool _approved) returns (bool)",
    "function transferFrom(address from, address to, uint256 tokenId) returns (bool)"
];


describe("ERC721 to CW721 Pointer", function () {
    let accounts;
    let pointer;
    let cw721Address;
    let admin;

    before(async function () {
        accounts = await setupSigners(await hre.ethers.getSigners())
        admin = await getAdmin()

        const codeId = await storeWasm(CW20_BASE_WASM_LOCATION)
        cw721Address = await instantiateWasm(codeId, accounts[0].seiAddress, "cw721", {
            name: "Test",
            symbol: "TEST",
            minter: admin.seiAddress
        })

        console.log("cw721Address = ", cw721Address)

        // deploy TestToken
        const pointerAddr = await deployErc721PointerForCw721(hre.ethers.provider, cw721Address)
        const contract = new hre.ethers.Contract(pointerAddr, erc721Abi, hre.ethers.provider);
        pointer = contract.connect(accounts[0].signer)
    })

    describe("read", function(){
        it("get name", async function () {
            const name = await pointer.name();
            expect(name).to.equal("Test");
        });
    })
})