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
        console.log("erc721 = ", erc721)
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
            await (await erc721.connect(accounts[1].signer).transferFrom(accounts[1].evmAddress, accounts[0].evmAddress, 3)).wait();
            const ownerResult2 = await queryWasm(pointer, "owner_of", { token_id: "3" });
            expect(ownerResult2).to.deep.equal({ data: { owner: accounts[0].seiAddress, approvals: [] } });
        });

        it("should approve a spender for a specific token", async function () {
            // Approve accounts[1] to manage token ID 3
            await executeWasm(pointer, { approve: { spender: accounts[1].seiAddress, token_id: "3" }});
            const approvalResult = await queryWasm(pointer, "approval", { token_id: "3", spender: accounts[1].seiAddress });
            expect(approvalResult).to.deep.equal({ data: { approval: { spender: accounts[1].seiAddress, expires: { never: {} } } } });

            // Revoke approval to reset the state
            await executeWasm(pointer, { revoke: { spender: accounts[1].seiAddress, token_id: "3" }});
            const result = await queryWasm(pointer, "approvals", { token_id: "3" });
            expect(result).to.deep.equal({data: { approvals:[]}});
        });

        it("should set an operator for all tokens of an owner", async function () {
            await executeWasm(pointer, { approve_all: { operator: accounts[1].seiAddress }});
            expect(await erc721.isApprovedForAll(admin.evmAddress, accounts[1].evmAddress)).to.be.true;
            await executeWasm(pointer, { revoke_all: { operator: accounts[1].seiAddress }});
            expect(await erc721.isApprovedForAll(admin.evmAddress, accounts[1].evmAddress)).to.be.false;
        });

        it("should burn a token", async function() {
            // try it just from erc721 itself
            // accounts[1] owns token id 2
            burnSolidityRes = await (await erc721.burn(2)).wait()
            console.log("burnSolidityRes", burnSolidityRes)

            // console.log("In burn token")
            // burnResult = await executeWasm(pointer, { burn: { token_id: "2" }});
            // console.log("burnResult", burnResult)
            // const ownerResult = await queryWasm(pointer, "owner_of", { token_id: "2" });
            // console.log(ownerResult);
            // what should happen is the owner should be the zero address
            // with 2 it should not work
            // expect(ownerResult).to.deep.equal({ data: { owner: accounts[2].seiAddress, approvals: [] } });
        })
    });

})