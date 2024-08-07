const { fundAddress, fundSeiAddress, getSeiBalance, associateKey, importKey, waitForReceipt, bankSend, evmSend, getNativeAccount} = require("./lib");
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
        },
        "test3": {
            seiAddress: 'sei1qkawqt7dw09rkvn53lm2deamtfcpuq9v0h6zur',
            evmAddress: '0xCb2FB25A6a34Ca874171Ac0406d05A49BC45a1cF',
            castAddress: 'sei1evhmykn2xn9gwst34szqd5z6fx7ytgw0l7g0vs',
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
        const beforeSei = BigInt(await getSeiBalance(seiAddr))
        const beforeEvm = await ethers.provider.getBalance(evmAddr)
        const gas = await associateFunc(seiAddr)
        const afterSei = BigInt(await getSeiBalance(seiAddr))
        const afterEvm = await ethers.provider.getBalance(evmAddr)

        // console.log(`SEI Balance (before): ${beforeSei}`)
        // console.log(`EVM Balance (before): ${beforeEvm}`)
        // console.log(`SEI Balance (after): ${afterSei}`)
        // console.log(`EVM Balance (after): ${afterEvm}`)

        const multiplier = BigInt(1000000000000)
        expect(afterEvm).to.equal((beforeSei * multiplier) + beforeEvm - (gas * multiplier))
        expect(afterSei).to.equal(truncate(beforeSei - gas))
    }

    before(async function(){
        await importKey("test1", "../contracts/test/test1.key")
        await importKey("test2", "../contracts/test/test2.key")
        await importKey("test3", "../contracts/test/test3.key")
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

    it("should associate with associate transaction", async function(){
        const addr = keys.test3
        await fundSeiAddress(addr.seiAddress, "10000000000")
        await fundAddress(addr.evmAddress, "200");

        await verifyAssociation(addr.seiAddress, addr.evmAddress, async function(){
            await associateKey("test3")
            return BigInt(0)
        });

        // it should not be able to send funds to the cast address after association
        expect(await getSeiBalance(addr.castAddress)).to.equal(0);
        await fundSeiAddress(addr.castAddress, "100");
        expect(await getSeiBalance(addr.castAddress)).to.equal(0);
    });

})