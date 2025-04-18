const { expect } = require("chai");

const { setupSigners, getAdmin, getSeiBalance, printClaimMsg, addKey, getKeySeiAddress, fundSeiAddress,hex2uint8} = require("./lib");


describe("Sei Solo Tester", function () {

    let accounts;
    let admin;

    before(async function () {
        accounts = await setupSigners(await hre.ethers.getSigners());
        admin = await getAdmin();
    })

    describe("Claim Tester", function () {
        const SoloPrecompileContract = '0x000000000000000000000000000000000000100C';
        let solo;
        let soloAddr;

        before(async function () {
            const signer = accounts[0].signer
            const contractABIPath = '../../precompiles/solo/abi.json';
            const contractABI = require(contractABIPath);
            // Get a contract instance
            solo = new ethers.Contract(SoloPrecompileContract, contractABI, signer);
            // setup account to be claimed
            await addKey("solo");
            soloAddr = await getKeySeiAddress("solo");
            await fundSeiAddress(soloAddr);
        });

        it("Claims all tokens from the old account", async function () {
            let signerAddr = await accounts[0].signer.getAddress();
            let claimMsg = await printClaimMsg("solo", signerAddr);
            let payload = hex2uint8(claimMsg);
            const claim = await solo.claim(payload, {gasLimit: 100000});
            const receipt = await claim.wait();
            expect(receipt.status).to.equal(1);
            let postClaimBalance = await getSeiBalance(soloAddr);
            expect(postClaimBalance).to.equal(0);
        });
    });
});