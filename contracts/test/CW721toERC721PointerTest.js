const {setupSigners, deployEvmContract, getAdmin, deployWasm, executeWasm, queryWasm} = require("./lib");
const {expect} = require("chai");

const CW721_POINTER_WASM = "../example/cosmwasm/cw721/artifacts/cwerc721.wasm";

describe("CW721 to ERC721 Pointer", function () {
    let accounts;
    let erc721;
    let pointer;
    let admin;

    before(async function () {
        accounts = await setupSigners(await hre.ethers.getSigners())
        erc721 = await deployEvmContract("MyNFT")
        admin = await getAdmin()
        pointer = await deployWasm(CW721_POINTER_WASM,
            accounts[0].seiAddress,
            "cw721-erc721",
            {erc721_address: await erc721.getAddress() }
        )

        await (await erc721.mint(accounts[0].evmAddress, 1)).wait()
        await (await erc721.mint(accounts[1].evmAddress, 2)).wait()
        await (await erc721.mint(admin.evmAddress, 3)).wait()

        await (await erc721.approve(accounts[1].evmAddress, 1)).wait();
        await (await erc721.setApprovalForAll(admin.evmAddress, true)).wait();
    })

    describe("query", function(){

        it("should query the owner of a token", async function () {
            const result = await queryWasm(pointer, "owner_of", { token_id: "1" });
            expect(result).to.deep.equal({data:{
                owner:accounts[0].seiAddress,
                approvals:[
                    {spender:accounts[1].seiAddress,expires:{never:{}}}
                ]
            }});
        });

        it("should confirm an approval exists for a specific spender and token", async function () {
            const result = await queryWasm(pointer, "approval", { token_id: "1", spender: accounts[1].seiAddress });
            expect(result).to.deep.equal({data:{
                approval:{spender:accounts[1].seiAddress, expires:{never:{}}}
            }});
        });

        it("should list all approvals for a token", async function () {
            const result = await queryWasm(pointer, "approvals", { token_id: "1" });
            expect(result).to.deep.equal({data:{
                approvals:[
                    {spender: accounts[1].seiAddress, expires:{never:{}}}
                ]}});
        });

        it("should verify if an operator is approved for all tokens of an owner", async function () {
            const result = await queryWasm(pointer, "operator", { owner: accounts[0].seiAddress, operator: admin.seiAddress });
            expect(result).to.deep.equal({
                data: {
                    approval: {
                        spender: admin.seiAddress,
                        expires: {never:{}}
                    }
                }
            });
        });

        it("should retrieve contract information", async function () {
            const result = await queryWasm(pointer, "contract_info", {});
            expect(result).to.deep.equal({data:{name:"MyNFT",symbol:"MYNFT"}});
        });

        it("should fetch NFT info based on token ID", async function () {
            const result = await queryWasm(pointer, "nft_info", { token_id: "1" });
            expect(result).to.deep.equal({ data: { token_uri: 'https://sei.io/token/1', extension: '' } });
        });

        it("should fetch all information about an NFT", async function () {
            const result = await queryWasm(pointer, "all_nft_info", { token_id: "1" });
            expect(result).to.deep.equal({
                data: {
                    access: {
                        owner: accounts[0].seiAddress,
                        approvals: [
                            {
                                spender: accounts[1].seiAddress,
                                expires: {
                                    never: {}
                                }
                            }
                        ]
                    },
                    info: {
                        token_uri: "https://sei.io/token/1",
                        extension: ""
                    }
                }
            });
        });

    })

    describe("execute operations", function () {
        it("should transfer an NFT to another address", async function () {
            await executeWasm(pointer, { transfer_nft: { recipient: accounts[1].seiAddress, token_id: "3" }});
            const ownerResult = await queryWasm(pointer, "owner_of", { token_id: "3" });
            expect(ownerResult).to.deep.equal({ data: { owner: accounts[1].seiAddress, approvals: [] } });
            await (await erc721.connect(accounts[1].signer).transferFrom(accounts[1].evmAddress, admin.evmAddress, 3)).wait();
            const ownerResult2 = await queryWasm(pointer, "owner_of", { token_id: "3" });
            expect(ownerResult2).to.deep.equal({ data: { owner: admin.seiAddress, approvals: [] } });
        });

        it("should not transfer an NFT if not owned", async function () {
            await executeWasm(pointer, { transfer_nft: { recipient: accounts[1].seiAddress, token_id: "2" }});
            const ownerResult = await queryWasm(pointer, "owner_of", { token_id: "2" });
            expect(ownerResult).to.deep.equal({ data: { owner: accounts[1].seiAddress, approvals: [] } });
        });

        it("should approve a spender for a specific token", async function () {
            // Approve accounts[1] to manage token ID 3
            await executeWasm(pointer, { approve: { spender: accounts[1].seiAddress, token_id: "3" }});
            const approvalResult = await queryWasm(pointer, "approval", { token_id: "3", spender: accounts[1].seiAddress });
            expect(approvalResult).to.deep.equal({ data: { approval: { spender: accounts[1].seiAddress, expires: { never: {} } } } });
            // allowed to transfer (does not revert)
            await (await erc721.connect(accounts[1].signer).transferFrom(admin.evmAddress, accounts[1].evmAddress, 3)).wait();
            // transfer back to try with approval revocation (has to go back to admin first)
            await (await erc721.connect(accounts[1].signer).transferFrom(accounts[1].evmAddress, admin.evmAddress, 3)).wait();

            // Revoke approval to reset the state
            await executeWasm(pointer, { revoke: { spender: accounts[1].seiAddress, token_id: "3" }});
            const result = await queryWasm(pointer, "approvals", { token_id: "3" });
            expect(result).to.deep.equal({data: { approvals:[]}});

            // no longer allowed to transfer
            await expect(erc721.connect(accounts[1].signer).transferFrom(admin.evmAddress, accounts[1].evmAddress, 3)).to.be.revertedWith("not authorized")
        });

        it("should set an operator for all tokens of an owner", async function () {
            await executeWasm(pointer, { approve_all: { operator: accounts[1].seiAddress }});
            expect(await erc721.isApprovedForAll(admin.evmAddress, accounts[1].evmAddress)).to.be.true
            await executeWasm(pointer, { revoke_all: { operator: accounts[1].seiAddress }});
            expect(await erc721.isApprovedForAll(admin.evmAddress, accounts[1].evmAddress)).to.be.false;
        });

    });

})