To regenerate these files, run:
```
solc --bin -o example/contracts/erc721 example/contracts/erc721/ERC721.sol --overwrite
solc --abi -o example/contracts/erc721 example/contracts/erc721/ERC721.sol --overwrite
abigen --abi=example/contracts/erc721/DummyERC721.abi --pkg=erc721 --out=example/contracts/erc721/ERC721.go
```