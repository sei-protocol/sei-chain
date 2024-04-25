const {setupSigners, deployEvmContract, getAdmin, deployWasm, instantiateWasm, queryWasm} = require("./lib");
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
                    {spender: accounts[1].seiAddress,expires:{never:{}}}
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

})