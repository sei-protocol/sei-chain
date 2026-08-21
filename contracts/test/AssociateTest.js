const { fundAddress, fundSeiAddress, getSeiBalance, importKey, waitForReceipt, bankSend, evmSend, getNativeAccount, waitForCondition} = require("./lib");
const { expect } = require("chai");

describe("Associate Balances", function () {

    const keys = {
        "test1": {
            seiAddress: 'sei1nzdg7e6rvkrmvp5zzmp5tupuj0088nqsa4mze4',
            evmAddress: '0x90684e7F229f2d8E2336661f79caB693E4228Ff7'
        },
        "test2": {
            seiAddress: 'sei1jqgph9jpdtvv64e3rzegxtssvgmh7lxnn8vmdq',
            evmAddress: '0x28b2B0621f76A2D08A9e04acb7F445E61ba5b7E7'
        }
    }

    const addresses = {
        seiAddress: 'sei1nzdg7e6rvkrmvp5zzmp5tupuj0088nqsa4mze4',
        evmAddress: '0x90684e7F229f2d8E2336661f79caB693E4228Ff7'
    }

    function truncate(num, byThisManyDecimals) {
        return parseFloat(`${num}`.slice(0, 12))
    }

    async function verifyAssociation(seiAddr, evmAddr, associateFunc) {
        const multiplier = BigInt(1000000000000)
        const beforeSei = BigInt(await getSeiBalance(seiAddr))
        const beforeEvm = await ethers.provider.getBalance(evmAddr)
        const gas = await associateFunc(seiAddr)
        const expectedEvm = (beforeSei * multiplier) + beforeEvm - (gas * multiplier)
        await waitForCondition(
            async () => (await ethers.provider.getBalance(evmAddr)) === expectedEvm,
            `EVM balance of ${evmAddr} to equal ${expectedEvm}`,
        )
        const afterSei = BigInt(await getSeiBalance(seiAddr))
        const afterEvm = await ethers.provider.getBalance(evmAddr)

        console.log(`SEI Balance (before): ${beforeSei}`)
        console.log(`EVM Balance (before): ${beforeEvm}`)
        console.log(`SEI Balance (after): ${afterSei}`)
        console.log(`EVM Balance (after): ${afterEvm}`)

        expect(afterEvm).to.equal(expectedEvm)
        expect(afterSei).to.equal(truncate(beforeSei - gas))
    }

    before(async function(){
        await importKey("test1", "../contracts/test/test1.key")
        await importKey("test2", "../contracts/test/test2.key")
    })

    it("should associate with sei transaction", async function(){
        const addr = keys.test1
        await fundSeiAddress(addr.seiAddress, "10000000000")
        await fundAddress(addr.evmAddress, "200");

        await verifyAssociation(addr.seiAddress, addr.evmAddress, async function(){
            await bankSend(addr.seiAddress, "test1")
            return BigInt(20000)
        })
    });

    it("should associate with evm transaction", async function(){
        const addr = keys.test2
        await fundSeiAddress(addr.seiAddress, "10000000000")
        await fundAddress(addr.evmAddress, "200");

        await verifyAssociation(addr.seiAddress, addr.evmAddress, async function(){
            const txHash = await evmSend(addr.evmAddress, "test2", "0")
            const receipt = await waitForReceipt(txHash)
            return BigInt(receipt.gasUsed * (receipt.gasPrice / BigInt(1000000000000)))
        })
    });
})
