const {setupSigners, deployErc721PointerForCw721, getAdmin, storeWasm, instantiateWasm,  executeWasm} = require("./lib");
const {expect} = require("chai");

const CW721_BASE_WASM_LOCATION = "../contracts/wasm/cw721_base.wasm";

const erc721Abi = [
    "function name() view returns (string)",
    "function symbol() view returns (string)",
    "function totalSupply() view returns (uint256)",
    "function tokenURI(uint256 tokenId) view returns (string)",
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

        const codeId = await storeWasm(CW721_BASE_WASM_LOCATION)
        cw721Address = await instantiateWasm(codeId, admin.seiAddress, "cw721", {
            name: "Test",
            symbol: "TEST",
            minter: admin.seiAddress
        })

        console.log("cw721Address = ", cw721Address)

        // mint some to admin
        mintRes = await executeWasm(cw721Address,  { mint : { token_id : "1", owner : admin.seiAddress }});
        mintRes = await executeWasm(cw721Address,  { mint : { token_id : "2", owner : accounts[0].seiAddress }});
        console.log("mintRes = ", mintRes)

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

        it("get symbol", async function () {
            const symbol = await pointer.symbol();
            expect(symbol).to.equal("TEST");
        });

        // haven't minted any
        it("owner of", async function () {
            const owner = await pointer.ownerOf(1);
            expect(owner).to.equal(admin.evmAddress);
        });

        it("token uri", async function () {
            const uri = await pointer.tokenURI(1);
            expect(uri).to.equal("null");
        });

        it("balance of", async function () {
            const balance = await pointer.balanceOf(admin.evmAddress);
            expect(balance).to.equal(1);
        });

        it("get approved", async function () {
            const approved = await pointer.getApproved(1);
            expect(approved).to.equal("0x0000000000000000000000000000000000000000");
        });

        it("is approved for all", async function () {
            const approved = await pointer.isApprovedForAll(admin.evmAddress, admin.evmAddress);
            expect(approved).to.equal(false);
        });
    })

    // TODO: do some unhappy paths!

    describe("write", function(){
        it("approve", async function () {
            res = await pointer.approve(accounts[1].evmAddress, 2);
            console.log("approve res = ", res)
            const approved = await pointer.getApproved(2);
            expect(approved).to.equal(accounts[1].evmAddress);
        });

        it("cannot approve token you don't own", async function () {
            await expect(pointer.approve(accounts[1].evmAddress, 1)).to.be.reverted;
        });

        // TODO: set token uri and test that you can read the new one

        // it("set approval for all", async function () {
        //     await pointer.setApprovalForAll(accounts[0].seiAddress, true);
        //     const approved = await pointer.isApprovedForAll(admin.evmAddress, accounts[0].evmAddress);
        //     expect(approved).to.equal(true);
        // });

        // it("transfer from", async function () {
        //     await pointer.transferFrom(admin.evmAddress, accounts[0].evmAddress, 1);
        //     const balance = await pointer.balanceOf(accounts[0].evmAddress);
        //     expect(balance).to.equal(1);
        // });
    })
})