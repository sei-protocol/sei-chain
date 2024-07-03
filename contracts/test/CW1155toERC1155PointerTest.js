const {setupSigners, deployEvmContract, getAdmin, deployWasm, executeWasm, queryWasm,
    deployErc1155PointerForCw1155, WASM, registerPointerForERC1155
} = require("./lib");
const {expect} = require("chai");

describe("CW1155 to ERC1155 Pointer", function () {
    let accounts;
    let erc1155;
    let pointer;
    let admin;

    before(async function () {
        accounts = await setupSigners(await hre.ethers.getSigners())
        erc1155 = await deployEvmContract("ERC1155Example")
        admin = await getAdmin()

        pointer = await registerPointerForERC1155(await erc1155.getAddress())

        await (await erc1155.mintForTest(accounts[0].evmAddress, 1)).wait()
        await (await erc1155.mintForTest(accounts[1].evmAddress, 2)).wait()
        await (await erc1155.mintForTest(admin.evmAddress, 3)).wait()
        await (await erc1155.setDefaultRoyalty(admin.evmAddress)).wait()
        await (await erc1155.connect(accounts[1].signer).setApprovalForAll(admin.evmAddress, true)).wait();
    })

    describe("validation", function(){
        it("should not allow a pointer to the pointer", async function(){
            try {
                await deployErc1155PointerForCw1155(hre.ethers.provider, pointer)
                expect.fail(`Expected to be prevented from creating a pointer`);
            } catch(e){
                expect(e.message).to.include("contract deployment failed");
            }
        })
    })

    describe("query", function(){
        it("should query the balance of an owner's token", async function () {
            const result = await queryWasm(pointer, "balance_of", { "owner": admin.seiAddress, token_id: "4" });
            expect(result).to.deep.equal({data:{balance:11}});
        });

        it("should batch query the balance of several owners' tokens", async function () {
            const result = await queryWasm(pointer, "balance_of_batch", [{"owner": admin.seiAddress, token_id: "4"}, {"owner": accounts[0].seiAddress, token_id: "14"}]);
            expect(result).to.deep.equal({data:{
                balances: [
                    { token_id: "4", owner: admin.seiAddress, amount: "11" },
                    { token_id: "14", owner: accounts[0].seiAddress, amount: "0" },
                ]
            }});
        });

        it("should show a specific spender is approved", async function () {
            const result = await queryWasm(pointer, "is_approved_for_all", { "owner": accounts[1].seiAddress, operator: admin.seiAddress });
            expect(result).to.deep.equal({data:{approved:true}});
        });

        it("should show a specific spender is not approved", async function () {
            const result = await queryWasm(pointer, "is_approved_for_all", { "owner": admin.seiAddress, operator: accounts[0].seiAddress });
            expect(result).to.deep.equal({data:{approved:false}});
        });

        it("should retrieve number of circulating tokens", async function () {
            const result = await queryWasm(pointer, "num_tokens", {});
            expect(result).to.deep.equal({data:{count:75*5}});
        });

        it("should retrieve number of circulating tokens for one token id", async function () {
            const result = await queryWasm(pointer, "num_tokens", { "token_id": "1" });
            expect(result).to.deep.equal({data:{count:21}});
        });

        it("should retrieve contract information", async function () {
            const result = await queryWasm(pointer, "contract_info", {});
            expect(result).to.deep.equal({data:{name:"DummyERC1155",symbol:"DUMMY"}});
        });

        it("should fetch all information about an NFT", async function () {
            const result = await queryWasm(pointer, "token_info", { token_id: "1" });
            expect(result.data.token_uri).to.equal('https://example.com/{id}');
            expect(result.data.extension.royalty_percentage).to.equal(5);
            expect(result.data.extension.royalty_payment_address).to.equal(admin.seiAddress);
        });
    })

    describe("execute operations", function () {
        it("should transfer an NFT to another address", async function () {
            let ownerResult = await queryWasm(pointer, "balance_of_batch", [
                { owner: admin.seiAddress, token_id: "3" },
                { owner: accounts[1].seiAddress, token_id: "3" },
            ]);
            expect(ownerResult).to.deep.equal({ data: {
                balances: [
                    { token_id: "3", owner: admin.seiAddress, amount: "10" },
                    { token_id: "3", owner: accounts[1].seiAddress, amount: "11" },
                ]
            }});
            const res = await executeWasm(pointer, {
                send: {
                    from: admin.seiAddress,
                    to: accounts[1].seiAddress,
                    token_id: "3",
                    amount: "5",
                }
            });
            expect(res.code).to.equal(0);
            ownerResult = await queryWasm(pointer, "balance_of_batch", [
                { owner: admin.seiAddress, token_id: "3" },
                { owner: accounts[1].seiAddress, token_id: "3" },
            ]);
            expect(ownerResult).to.deep.equal({ data: {
                balances: [
                    { token_id: "3", owner: admin.seiAddress, amount: "5" },
                    { token_id: "3", owner: accounts[1].seiAddress, amount: "16" },
                ]
            }});
            await (await erc1155.connect(accounts[1].signer).safeTransferFrom(accounts[1].evmAddress, admin.evmAddress, 3, 5, '0x')).wait();
            ownerResult = await queryWasm(pointer, "balance_of_batch", [
                { owner: admin.seiAddress, token_id: "3" },
                { owner: accounts[1].seiAddress, token_id: "3" },
            ]);
            expect(ownerResult).to.deep.equal({ data: {
                balances: [
                    { token_id: "3", owner: admin.seiAddress, amount: "10" },
                    { token_id: "3", owner: accounts[1].seiAddress, amount: "11" },
                ]
            }});
        });

        it("should not transfer an NFT if not owned", async function () {
            let ownerResult = await queryWasm(pointer, "balance_of_batch", [
                { owner: admin.seiAddress, token_id: "0" },
                { owner: accounts[1].seiAddress, token_id: "0" },
            ]);
            expect(ownerResult).to.deep.equal({ data: {
                balances: [
                    { token_id: "0", owner: admin.seiAddress, amount: "0" },
                    { token_id: "0", owner: accounts[1].seiAddress, amount: "0" },
                ]
            }});
            const res = await executeWasm(pointer, {
                send: {
                    from: accounts[1].seiAddress,
                    to: admin.seiAddress,
                    token_id: "0",
                    amount: "5",
                }
            });
            expect(res.code).to.not.equal(0);
            ownerResult = await queryWasm(pointer, "balance_of_batch", [
                { owner: admin.seiAddress, token_id: "0" },
                { owner: accounts[1].seiAddress, token_id: "0" },
            ]);
            expect(ownerResult).to.deep.equal({ data: {
                balances: [
                    { token_id: "0", owner: admin.seiAddress, amount: "0" },
                    { token_id: "0", owner: accounts[1].seiAddress, amount: "0" },
                ]
            }});
        });

        it("should transfer multiple NFT token ids to another address", async function () {
            let ownerResult = await queryWasm(pointer, "balance_of_batch", [
                { owner: admin.seiAddress, token_id: "3" },
                { owner: accounts[1].seiAddress, token_id: "3" },
                { owner: admin.seiAddress, token_id: "4" },
                { owner: accounts[1].seiAddress, token_id: "4" },
            ]);
            expect(ownerResult).to.deep.equal({ data: {
                balances: [
                    { token_id: "3", owner: admin.seiAddress, amount: "10" },
                    { token_id: "3", owner: accounts[1].seiAddress, amount: "11" },
                    { token_id: "4", owner: admin.seiAddress, amount: "11" },
                    { token_id: "4", owner: accounts[1].seiAddress, amount: "12" },
                ]
            }});
            const res = await executeWasm(pointer, {
                send_batch: {
                    from: admin.seiAddress,
                    to: accounts[1].seiAddress,
                    batch: [
                        { token_id: "3", amount: "5" },
                        { token_id: "4", amount: "1" },
                    ],
                }
            });
            expect(res.code).to.equal(0);
            ownerResult = await queryWasm(pointer, "balance_of_batch", [
                { owner: admin.seiAddress, token_id: "3" },
                { owner: accounts[1].seiAddress, token_id: "3" },
                { owner: admin.seiAddress, token_id: "4" },
                { owner: accounts[1].seiAddress, token_id: "4" },
            ]);
            expect(ownerResult).to.deep.equal({ data: {
                balances: [
                    { token_id: "3", owner: admin.seiAddress, amount: "5" },
                    { token_id: "3", owner: accounts[1].seiAddress, amount: "16" },
                    { token_id: "4", owner: admin.seiAddress, amount: "10" },
                    { token_id: "4", owner: accounts[1].seiAddress, amount: "13" },
                ]
            }});
        });

        it("should set an operator for all tokens of an owner", async function () {
            expect(await erc1155.isApprovedForAll(admin.evmAddress, accounts[1].evmAddress)).to.be.false;
            await executeWasm(pointer, { approve_all: { operator: accounts[1].seiAddress }});
            expect(await erc1155.isApprovedForAll(admin.evmAddress, accounts[1].evmAddress)).to.be.true
            await executeWasm(pointer, { revoke_all: { operator: accounts[1].seiAddress }});
            expect(await erc1155.isApprovedForAll(admin.evmAddress, accounts[1].evmAddress)).to.be.false;
        });

    });

})