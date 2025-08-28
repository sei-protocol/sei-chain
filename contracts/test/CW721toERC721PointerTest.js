const {setupSigners, deployEvmContract, getAdmin, deployWasm, executeWasm, queryWasm, deployErc20PointerForCw20,
    deployErc721PointerForCw721, WASM, registerPointerForERC721
} = require("./lib");
const {expect} = require("chai");

describe("CW721 to ERC721 Pointer", function () {
    let accounts;
    let erc721;
    let pointer;
    let admin;

    before(async function () {
        accounts = await setupSigners(await hre.ethers.getSigners())
        erc721 = await deployEvmContract("MyNFT")
        admin = await getAdmin()

        pointer = await registerPointerForERC721(await erc721.getAddress())

        await (await erc721.mint(accounts[0].evmAddress, 1)).wait()
        await (await erc721.mint(accounts[1].evmAddress, 2)).wait()
        await (await erc721.mint(admin.evmAddress, 3)).wait()

        await (await erc721.approve(accounts[1].evmAddress, 1)).wait();
        await (await erc721.setApprovalForAll(admin.evmAddress, true)).wait();
    })

    describe("validation", function(){
        it("should not allow a pointer to the pointer", async function(){
            try {
                await deployErc721PointerForCw721(hre.ethers.provider, pointer)
                expect.fail(`Expected to be prevented from creating a pointer`);
            } catch(e){
                expect(e.message).to.include("contract deployment failed");
            }
        })
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

        it("should retrieve number of circulating tokens", async function () {
            const result = await queryWasm(pointer, "num_tokens", {});
            expect(result).to.deep.equal({data:{count:3}});
        });

        it("should retrieve contract information", async function () {
            const result = await queryWasm(pointer, "contract_info", {});
            expect(result).to.deep.equal({data:{name:"MyNFT",symbol:"MYNFT"}});
        });

        it("should fetch all information about an NFT", async function () {
            const result = await queryWasm(pointer, "all_nft_info", { token_id: "1" });
            expect(result.data.access).to.deep.equal({
                owner: accounts[0].seiAddress,
                approvals: [
                    {
                        spender: accounts[1].seiAddress,
                        expires: {
                            never: {}
                        }
                    }
                ]
            });
            expect(result.data.info.token_uri).to.equal('https://sei.io/token/1');
            expect(result.data.info.extension.royalty_percentage).to.equal(5);
            expect(result.data.info.extension.royalty_payment_address).to.include("sei1");
        });

        it("should retrieve all minted NFT token ids", async function () {
            const result = await queryWasm(pointer, "all_tokens", {});
            expect(result).to.deep.equal({data:{tokens:["1","2","3"]}});
        });

        it("should retrieve list of 1 minted NFT token id after token id 1", async function () {
            const result = await queryWasm(pointer, "all_tokens", { start_after: "1", limit: 1 });
            expect(result).to.deep.equal({data:{tokens:["2"]}});
        });

        it("should retrieve list of NFT token ids owned by admin", async function () {
            const result = await queryWasm(pointer, "tokens", { owner: admin.seiAddress });
            expect(result).to.deep.equal({data:{tokens:["3"]}});
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