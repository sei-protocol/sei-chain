const { ethers, upgrades } = require('hardhat');

describe("ERC721 test", function () {

    describe("ERC721 Throughput", function () {
        let erc721;
        let owner;
        let receiver;

        // This function deploys a new instance of the contract before each test
        beforeEach(async function () {
            let signers = await ethers.getSigners();
            owner = signers[0];
            receiver = signers[1];
            const ERC721 = await ethers.getContractFactory("MyNFT")
            erc721 = await ERC721.deploy();

            await Promise.all([erc721.waitForDeployment()])
        });

        it("should send 10000", async function(){
            this.timeout(100000); // Increase timeout for this test

            let nonce = await ethers.provider.getTransactionCount(owner.address);
            const sends = []

            const count = 10000
            // start of all the rpc calls
            for(let i=0; i<count-1; i++){
                sends.push(erc721.mint(receiver, i, {nonce: nonce}))
                nonce++
            }
            await Promise.all(sends)
            const receipt = await erc721.mint(receiver, count-1, {nonce: nonce})
            await receipt.wait()
        })
    })
});
