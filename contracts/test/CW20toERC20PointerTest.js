const {fundAddress, storeWasm, instantiateWasm, getSeiAddress, queryWasm, deployEvmContract} = require("./lib")
const { expect } = require("chai");

const CW20_POINTER_WASM = "../example/cosmwasm/cw20/artifacts/cwerc20.wasm";
describe("CW20 to ERC20 Pointer", function () {
    let deployer;
    let testToken;
    let wasmAddress;

    async function setBalance(addr, balance) {
        const resp = await testToken.setBalance(addr, balance)
        await resp.wait()
    }

    before(async function () {
        let signers = await hre.ethers.getSigners();
        deployer = signers[0];
        const deployerAddr = await deployer.getAddress();
        await fundAddress(deployerAddr);

        // force to associate
        const resp = await deployer.sendTransaction({
            to: deployerAddr,
            value: 0
        });
        await resp.wait()

        const deployerSeiAddress = await getSeiAddress(deployerAddr)

        // deploy TestToken
        testToken = await deployEvmContract("TestToken", ["TEST", "TEST"])
        const tokenAddr = await testToken.getAddress()
        await setBalance(deployerAddr, 1000000000000)

        const codeId = await storeWasm(CW20_POINTER_WASM)
        wasmAddress = await instantiateWasm(codeId, deployerSeiAddress, "cw20-erc20", {erc20_address: tokenAddr })
    })

    it("should return token_info", async function(){
        const result = await queryWasm(wasmAddress, "token_info", {})
        expect(result).to.deep.equal({"data":{"name":"TEST","symbol":"TEST","decimals":18,"total_supply":"1000000000000"}})
    })

})