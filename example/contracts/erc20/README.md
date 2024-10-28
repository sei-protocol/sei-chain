To regenerate these files, run:
```
solc --bin -o example/contracts/erc20 example/contracts/erc20/ERC20.sol --overwrite
solc --abi -o example/contracts/erc20 example/contracts/erc20/ERC20.sol --overwrite
abigen --abi=example/contracts/erc20/ERC20.abi --pkg=erc20 --out=example/contracts/erc20/ERC20.go
```