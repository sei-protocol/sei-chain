import { expect } from 'chai';
import { ethers } from 'hardhat';

describe('token operation fixtures', () => {
    it('supports repeated ERC20 and ERC721 state changes', async () => {
        const [owner, recipient] = await ethers.getSigners();
        const token = await ethers.deployContract('TestERC20', [owner.address]);
        await token.mint(owner.address, 100);
        await token.transfer(recipient.address, 25);
        await token.burn(10);
        expect(await token.balanceOf(owner.address)).to.equal(65);
        expect(await token.balanceOf(recipient.address)).to.equal(25);

        const nft = await ethers.deployContract('TestNFT', [owner.address]);
        await nft.safeMint(owner.address, 1);
        await nft.transferFrom(owner.address, recipient.address, 1);
        expect(await nft.ownerOf(1)).to.equal(recipient.address);

        await nft.safeMint(owner.address, 2);
        await expect(nft.roundTripTransfer(recipient.address, 2))
            .to.emit(nft, 'Transfer')
            .withArgs(owner.address, recipient.address, 2)
            .and.to.emit(nft, 'Transfer')
            .withArgs(recipient.address, owner.address, 2);
        expect(await nft.ownerOf(2)).to.equal(owner.address);
        expect(await nft.balanceOf(recipient.address)).to.equal(1);
    });

    it('supports ERC1155 single and batch operations', async () => {
        const [owner, recipient] = await ethers.getSigners();
        const token = await ethers.deployContract('TestERC1155');
        await token.mint(owner.address, 1, 10);
        await token.safeTransferFrom(owner.address, recipient.address, 1, 3, '0x');
        await token.mintBatch(owner.address, [2, 3], [5, 6]);
        await token.safeBatchTransferFrom(owner.address, recipient.address, [2, 3], [1, 2], '0x');
        expect(await token.balanceOf(1, recipient.address)).to.equal(3);
        expect(await token.balanceOf(2, recipient.address)).to.equal(1);
        expect(await token.balanceOf(3, recipient.address)).to.equal(2);
    });
});
