const {getAdmin, deployEvmContract, setupSigners,
    registerPointerForERC20, registerPointerForERC721, registerPointerForERC1155,
} = require("./lib");
const { expect } = require("chai");

describe("CW Pointer Functionality", function () {
    let accounts;
    let admin;
    let testToken;

    async function setBalance(addr, balance) {
        const resp = await testToken.setBalance(addr, balance);
        await resp.wait();
    }

    it("should not allow registering CW20->ERC20 pointers", async function () {
        accounts = await setupSigners(await hre.ethers.getSigners());

        // Deploy TestToken
        testToken = await deployEvmContract("TestToken", ["TEST", "TEST"]);
        const tokenAddr = await testToken.getAddress();

        // Give admin balance
        admin = await getAdmin();
        await setBalance(admin.evmAddress, 1000000000000);

        await expect(registerPointerForERC20(tokenAddr)).to.be.rejectedWith("contract deployment failed");
    });

    it("should not allow registering CW721->ERC721 pointers", async function () {
        accounts = await setupSigners(await hre.ethers.getSigners())
        erc721 = await deployEvmContract("MyNFT")
        admin = await getAdmin()

        await expect(registerPointerForERC721(await erc721.getAddress())).to.be.rejectedWith("contract deployment failed");
    });

    it("should not allow registering CW1155->ERC1155 pointers", async function () {
        accounts = await setupSigners(await hre.ethers.getSigners())
        erc1155 = await deployEvmContract("ERC1155Example")
        admin = await getAdmin()

        await expect(registerPointerForERC1155(await erc1155.getAddress())).to.be.rejectedWith("contract deployment failed");
    });
});
