const { exec } = require("child_process");
const {ethers} = require("hardhat"); // Importing exec from child_process
const axios = require("axios");

const adminKeyName = "admin"

const ABI = {
    ERC20: [
        "function name() view returns (string)",
        "function symbol() view returns (string)",
        "function decimals() view returns (uint8)",
        "function totalSupply() view returns (uint256)",
        "function balanceOf(address owner) view returns (uint256 balance)",
        "function transfer(address to, uint amount) returns (bool)",
        "function allowance(address owner, address spender) view returns (uint256)",
        "function approve(address spender, uint256 value) returns (bool)",
        "function transferFrom(address from, address to, uint value) returns (bool)",
        "error ERC20InsufficientAllowance(address spender, uint256 allowance, uint256 needed)"
    ],
    ERC721: [
        "event Approval(address indexed owner, address indexed approved, uint256 indexed tokenId)",
        "event Transfer(address indexed from, address indexed to, uint256 indexed tokenId)",
        "event ApprovalForAll(address indexed owner, address indexed operator, bool approved)",
        "function name() view returns (string)",
        "function symbol() view returns (string)",
        "function owner() view returns (address)",
        "function totalSupply() view returns (uint256)",
        "function tokenURI(uint256 tokenId) view returns (string)",
        "function royaltyInfo(uint256 tokenId, uint256 salePrice) view returns (address, uint256)",
        "function balanceOf(address owner) view returns (uint256 balance)",
        "function ownerOf(uint256 tokenId) view returns (address owner)",
        "function getApproved(uint256 tokenId) view returns (address operator)",
        "function isApprovedForAll(address owner, address operator) view returns (bool)",
        "function approve(address to, uint256 tokenId) returns (bool)",
        "function setApprovalForAll(address operator, bool _approved) returns (bool)",
        "function transferFrom(address from, address to, uint256 tokenId) returns (bool)",
        "function safeTransferFrom(address from, address to, uint256 tokenId) returns (bool)",
        "function safeTransferFrom(address from, address to, uint256 tokenId, bytes memory data) returns (bool)"
    ],
    ERC1155: [
        "event TransferSingle(address indexed _operator, address indexed _from, address indexed _to, uint256 _id, uint256 _value)",
        "event TransferBatch(address indexed _operator, address indexed _from, address indexed _to, uint256[] _ids, uint256[] _values)",
        "event ApprovalForAll(address indexed _owner, address indexed _operator, bool _approved)",
        "event URI(string _value, uint256 indexed _id)",
        "function name() view returns (string)",
        "function symbol() view returns (string)",
        "function owner() view returns (address)",
        "function uri(uint256 _id) view returns (string)",
        "function royaltyInfo(uint256 tokenId, uint256 salePrice) view returns (address, uint256)",
        "function balanceOf(address _owner, uint256 _id) view returns (uint256)",
        "function balanceOfBatch(address[] _owners, uint256[] _ids) view returns (uint256[])",
        "function isApprovedForAll(address _owner, address _operator) view returns (bool)",
        "function setApprovalForAll(address _operator, bool _approved)",
        "function transferFrom(address from, address to, uint256 tokenId) returns (bool)",
        "function safeTransferFrom(address _from, address _to, uint256 _id, uint256 _value, bytes _data)",
        "function safeBatchTransferFrom(address _from, address _to, uint256[] _ids, uint256[] _values, bytes _data)",
        "function totalSupply() view returns (uint256)",
        "function totalSupply(uint256 id) view returns (uint256)",
        "function exists(uint256 id) view returns (uint256)",
    ],
}

const WASM = {
    CW1155: "../contracts/wasm/cw1155_base.wasm",
    CW721: "../contracts/wasm/cw721_base.wasm",
    CW20: "../contracts/wasm/cw20_base.wasm",
    POINTER_CW20: "../example/cosmwasm/cw20/artifacts/cwerc20.wasm",
    POINTER_CW721: "../example/cosmwasm/cw721/artifacts/cwerc721.wasm",
    POINTER_CW1155: "../example/cosmwasm/cw721/artifacts/cwerc1155.wasm",
}

function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

async function delay() {
    await sleep(1000)
}

async function getCosmosTx(provider, evmTxHash) {
    return await provider.send("sei_getCosmosTx", [evmTxHash])
}

async function getEvmTx(provider, cosmosTxHash) {
    return await provider.send("sei_getEvmTx", [cosmosTxHash])
}

async function fundAddress(addr, amount="1000000000000000000000") {
    const result = await evmSend(addr, adminKeyName, amount)
    await delay()
    return result
}

async function evmSend(addr, fromKey, amount="10000000000000000000000000") {
    const output = await execute(`seid tx evm send ${addr} ${amount} --from ${fromKey} -b block -y`);
    return output.replace(/.*0x/, "0x").trim()
}

async function bankSend(toAddr, fromKey, amount="100000000000", denom="usei") {
    const result = await execute(`seid tx bank send ${fromKey} ${toAddr} ${amount}${denom} -b block --fees 20000usei -y`);
    await delay()
    return result
}

async function fundSeiAddress(seiAddr, amount="100000000000", denom="usei", funder=adminKeyName) {
    return await execute(`seid tx bank send ${funder} ${seiAddr} ${amount}${denom} -b block --fees 20000usei -y`);
}

async function getSeiBalance(seiAddr, denom="usei") {
    const result = await execute(`seid query bank balances ${seiAddr} -o json`);
    const balances = JSON.parse(result)
    for(let b of balances.balances) {
        if(b.denom === denom) {
            return parseInt(b.amount, 10)
        }
    }
    return 0
}

async function addKey(name) {
    try {
        return await execute(`seid keys add ${name}`, `printf "12345678\\n12345678\\n"`)
    } catch(e) {}
}

async function importKey(name, keyfile) {
    try {
        return await execute(`seid keys import ${name} ${keyfile}`, `printf "12345678\\n12345678\\n"`)
    } catch(e) {
        console.log("not importing key (skipping)")
        console.log(e)
    }
}

async function getNativeAccount(keyName) {
    await associateKey(adminKeyName)
    const seiAddress = await getKeySeiAddress(keyName)
    await fundSeiAddress(seiAddress)
    await delay()
    const evmAddress = await getEvmAddress(seiAddress)
    return {
        seiAddress,
        evmAddress
    }
}

async function getAdmin() {
    await associateKey(adminKeyName)
    return await getNativeAccount(adminKeyName)
}

async function getKeySeiAddress(name) {
    return (await execute(`seid keys show ${name} -a`)).trim()
}

async function associateKey(keyName) {
    try {
        await execute(`seid tx evm associate-address --from ${keyName} -b block`)
        await delay()
    }catch(e){
        console.log("skipping associate")
    }
}

function getEventAttribute(response, type, attribute) {
    if(!response.logs || response.logs.length === 0) {
        throw new Error("logs not returned")
    }

    for(let evt of response.logs[0].events) {
        if(evt.type === type) {
            for(let att of evt.attributes) {
                if(att.key === attribute) {
                    return att.value;
                }
            }
        }
    }
    throw new Error("attribute not found")
}

async function testAPIEnabled(provider) {
    try {
        // noop operation to see if it throws
        await incrementPointerVersion(provider, "cw20", 0)
        return true;
    } catch(e){
        console.log(e)
        return false;
    }
}

async function incrementPointerVersion(provider, pointerType, offset) {
    if(await isDocker()) {
        // must update on all nodes
        for(let i=0; i<4; i++) {
            const resultStr = await execCommand(`docker exec sei-node-${i} curl -s -X POST http://localhost:8545 -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","method":"test_incrementPointerVersion","params":["${pointerType}", ${offset}],"id":1}'`)
            const result = JSON.parse(resultStr)
            if(result.error){
                throw new Error(`failed to increment pointer version: ${result.error}`)
            }
        }
    } else {
       await provider.send("test_incrementPointerVersion", [pointerType, offset]);
    }
}

async function rawHttpDebugTraceWithCallTracer(txHash) {
    const payload = {
        jsonrpc: "2.0",
        method: "debug_traceTransaction",
        params: [txHash, {"tracer": "callTracer"}], // The second parameter is an optional trace config object
        id: 1,
    };
    const response = await axios.post("http://localhost:8545", payload, {
        headers: { "Content-Type": "application/json" },
    });
    return response.data;
}

async function createTokenFactoryTokenAndMint(name, amount, recipient, from=adminKeyName) {
    const command = `seid tx tokenfactory create-denom ${name} --from ${from} --gas=5000000 --fees=1000000usei -y --broadcast-mode block -o json`
    const output = await execute(command);
    const response = JSON.parse(output)
    const token_denom = getEventAttribute(response, "create_denom", "new_token_denom")
    const mint_command = `seid tx tokenfactory mint ${amount}${token_denom} --from ${from} --gas=5000000 --fees=1000000usei -y --broadcast-mode block -o json`
    await execute(mint_command);

    const send_command = `seid tx bank send ${from} ${recipient} ${amount}${token_denom} --from ${from} --gas=5000000 --fees=1000000usei -y --broadcast-mode block -o json`
    await execute(send_command);
    return token_denom
}

async function getChainId() {
    const nodeUrl = 'http://localhost:8545';
    const response = await axios.post(nodeUrl, {
        method: 'eth_chainId',
        params: [],
        id: 1,
        jsonrpc: "2.0"
    })
    return response.data.result;
}

async function getGasPrice() {
    const nodeUrl = 'http://localhost:8545';
    const response = await axios.post(nodeUrl, {
        method: 'eth_gasPrice',
        params: [],
        id: 1,
        jsonrpc: "2.0"
    })
    return response.data.result;
}

async function getPointerForNative(name) {
    const command = `seid query evm pointer NATIVE ${name} -o json`
    const output = await execute(command);
    return JSON.parse(output);
}

async function storeWasm(path, from=adminKeyName) {
    const command = `seid tx wasm store ${path} --from ${from} --gas=5000000 --fees=1000000usei -y --broadcast-mode block -o json`
    const output = await execute(command);
    const response = JSON.parse(output)
    return getEventAttribute(response, "store_code", "code_id")
}

async function getPointerForCw20(cw20Address) {
    const command = `seid query evm pointer CW20 ${cw20Address} -o json`
    const output = await execute(command);
    return JSON.parse(output);
}

async function getPointerForCw721(cw721Address) {
    const command = `seid query evm pointer CW721 ${cw721Address} -o json`
    const output = await execute(command);
    return JSON.parse(output);
}

async function getPointerForCw1155(cw1155Address) {
    const command = `seid query evm pointer CW1155 ${cw1155Address} -o json`
    const output = await execute(command);
    return JSON.parse(output);
}

async function deployErc20PointerForCw20(provider, cw20Address, attempts=10, from=adminKeyName, evmRpc="") {
    let command = `seid tx evm register-evm-pointer CW20 ${cw20Address} --from=${from} -b block`
    if (evmRpc) {
        command = command + ` --evm-rpc=${evmRpc}`
    }
    const output = await execute(command);
    const txHash = output.replace(/.*0x/, "0x").trim()
    let attempt = 0;
    while(attempt < attempts) {
        const receipt = await provider.getTransactionReceipt(txHash);
        if(receipt && receipt.status === 1) {
            return (await getPointerForCw20(cw20Address)).pointer
        } else if(receipt){
            throw new Error("contract deployment failed")
        }
        await sleep(500)
        attempt++
    }
    throw new Error("contract deployment failed")
}

async function deployErc20PointerNative(provider, name, from=adminKeyName, evmRpc="") {
    let command = `seid tx evm call-precompile pointer addNativePointer ${name} --from=${from} -b block`
    if (evmRpc) {
        command = command + ` --evm-rpc=${evmRpc}`
    }
    const output = await execute(command);
    const txHash = output.replace(/.*0x/, "0x").trim()
    let attempt = 0;
    while(attempt < 10) {
        const receipt = await provider.getTransactionReceipt(txHash);
        if(receipt) {
            return (await getPointerForNative(name)).pointer
        }
        await sleep(500)
        attempt++
    }
    throw new Error("contract deployment failed")
}

async function deployErc721PointerForCw721(provider, cw721Address, from=adminKeyName, evmRpc="") {
    let command = `seid tx evm register-evm-pointer CW721 ${cw721Address} --from=${from} -b block`
    if (evmRpc) {
        command = command + ` --evm-rpc=${evmRpc}`
    }
    const output = await execute(command);
    const txHash = output.replace(/.*0x/, "0x").trim()
    let attempt = 0;
    while(attempt < 10) {
        const receipt = await provider.getTransactionReceipt(txHash);
        if(receipt && receipt.status === 1) {
            return (await getPointerForCw721(cw721Address)).pointer
        } else if(receipt){
            throw new Error("contract deployment failed")
        }
        await sleep(500)
        attempt++
    }
    throw new Error("contract deployment failed")
}

async function deployErc1155PointerForCw1155(provider, cw1155Address, from=adminKeyName, evmRpc="") {
    let command = `seid tx evm register-evm-pointer CW1155 ${cw1155Address} --from=${from} -b block`
    if (evmRpc) {
        command = command + ` --evm-rpc=${evmRpc}`
    }
    const output = await execute(command);
    const txHash = output.replace(/.*0x/, "0x").trim()
    let attempt = 0;
    while(attempt < 10) {
        const receipt = await provider.getTransactionReceipt(txHash);
        if(receipt && receipt.status === 1) {
            return (await getPointerForCw1155(cw1155Address)).pointer
        } else if(receipt){
            throw new Error("contract deployment failed")
        }
        await sleep(500)
        attempt++
    }
    throw new Error("contract deployment failed")
}

async function deployWasm(path, adminAddr, label, args = {}, from=adminKeyName) {
    const codeId = await storeWasm(path, from)
    return await instantiateWasm(codeId, adminAddr, label, args, from)
}

async function instantiateWasm(codeId, adminAddr, label, args = {}, from=adminKeyName) {
    const jsonString = JSON.stringify(args).replace(/"/g, '\\"');
    const command = `seid tx wasm instantiate ${codeId} "${jsonString}" --label ${label} --admin ${adminAddr} --from ${from} --gas=5000000 --fees=1000000usei -y --broadcast-mode block -o json`;
    const output = await execute(command);
    const response = JSON.parse(output);
    return getEventAttribute(response, "instantiate", "_contract_address");
}

async function proposeCW20toERC20Upgrade(erc20Address, cw20Address, title="erc20-pointer", version=99, description="erc20 pointer",fees="200000usei", from=adminKeyName) {
    const command = `seid tx evm add-cw-erc20-pointer "${title}" "${description}" ${erc20Address} ${version} 200000000usei ${cw20Address} --from ${from} --fees ${fees} -y -o json --broadcast-mode=block`
    const output = await execute(command);
    const proposalId = getEventAttribute(JSON.parse(output), "submit_proposal", "proposal_id")
    return await passProposal(proposalId)
}

async function passProposal(proposalId,  desposit="200000000usei", fees="200000usei", from=adminKeyName) {
    if(await isDocker()) {
        await executeOnAllNodes(`seid tx gov vote ${proposalId} yes --from node_admin -b block -y --fees ${fees}`)
    } else {
        await execute(`seid tx gov vote ${proposalId} yes --from ${from} -b block -y --fees ${fees}`)
    }
    for(let i=0; i<100; i++) {
        const proposal = await execute(`seid q gov proposal ${proposalId} -o json`)
        const status = JSON.parse(proposal).status
        if(status === "PROPOSAL_STATUS_PASSED") {
            return proposalId
        }
        await delay()
    }
    throw new Error("could not pass proposal "+proposalId)
}

async function registerPointerForERC20(erc20Address, fees="20000usei", from=adminKeyName) {
    const command = `seid tx evm register-cw-pointer ERC20 ${erc20Address} --from ${from} --fees ${fees} --broadcast-mode block -y -o json`
    const output = await execute(command);
    const response = JSON.parse(output)
    if(response.code !== 0) {
        throw new Error("contract deployment failed")
    }
    return getEventAttribute(response, "pointer_registered", "pointer_address")
}

async function registerPointerForERC721(erc721Address, fees="20000usei", from=adminKeyName) {
    const command = `seid tx evm register-cw-pointer ERC721 ${erc721Address} --from ${from} --fees ${fees} --broadcast-mode block -y -o json`
    const output = await execute(command);
    const response = JSON.parse(output)
    if(response.code !== 0) {
        throw new Error("contract deployment failed")
    }
    return getEventAttribute(response, "pointer_registered", "pointer_address")
}

async function registerPointerForERC1155(erc1155Address, fees="200000usei", from=adminKeyName) {
    const command = `seid tx evm register-cw-pointer ERC1155 ${erc1155Address} --from ${from} --fees ${fees} --broadcast-mode block -y -o json`
    const output = await execute(command);
    const response = JSON.parse(output)
    if(response.code !== 0) {
        throw new Error("contract deployment failed")
    }
    return getEventAttribute(response, "pointer_registered", "pointer_address")
}

async function getSeiAddress(evmAddress) {
    const command = `seid q evm sei-addr ${evmAddress} -o json`
    const output = await execute(command);
    const response = JSON.parse(output)
    return response.sei_address
}

async function getEvmAddress(seiAddress) {
    const command = `seid q evm evm-addr ${seiAddress} -o json`
    const output = await execute(command);
    const response = JSON.parse(output)
    return response.evm_address
}

function generateWallet() {
    const wallet = ethers.Wallet.createRandom();
    return wallet.connect(ethers.provider);
}

async function deployEvmContract(name, args=[]) {
    const Contract = await ethers.getContractFactory(name);
    const contract = await Contract.deploy(...args);
    await contract.waitForDeployment()
    return contract;
}

async function setupSigners(signers) {
    const result = []
    for(let signer of signers) {
        const evmAddress = await signer.getAddress();
        await fundAddress(evmAddress);
        await delay()
        const resp = await signer.sendTransaction({
            to: evmAddress,
            value: 0
        });
        await resp.wait()
        const seiAddress = await getSeiAddress(evmAddress);
        result.push({
            seiAddress,
            evmAddress,
            signer,
        })
    }
    return result;
}

async function queryWasm(contractAddress, operation, args={}){
    const jsonString = JSON.stringify({ [operation]: args }).replace(/"/g, '\\"');
    const command = `seid query wasm contract-state smart ${contractAddress} "${jsonString}" --output json`;
    const output = await execute(command);
    return JSON.parse(output)
}

async function executeWasm(contractAddress, msg, coins = "0usei") {
    const jsonString = JSON.stringify(msg).replace(/"/g, '\\"'); // Properly escape JSON string
    const command = `seid tx wasm execute ${contractAddress} "${jsonString}" --amount ${coins} --from ${adminKeyName} --gas=5000000 --fees=1000000usei -y --broadcast-mode block -o json`;
    const output = await execute(command);
    return JSON.parse(output);
}

async function associateWasm(contractAddress) {
    const command = `seid tx evm associate-contract-address ${contractAddress} --from ${adminKeyName} --gas=5000000 --fees=1000000usei -y --broadcast-mode block -o json`;
    const output = await execute(command);
    return JSON.parse(output);
}

async function printClaimMsg(sender, claimer) {
    const command = `seid tx evm print-claim ${claimer} --from ${sender} -y`;
    try { return await execute(command); }
    catch(e) { console.log(e); }
}

async function printClaimMsgBySender(sender, claimer, senderAddr) {
    const command = `seid tx evm print-claim-by-sender ${claimer} ${senderAddr} --from ${sender} -y`;
    try { return await execute(command); }
    catch(e) { console.log(e); }
}

async function printClaimSpecificMsg(sender, claimer, ...assets) {
    const command = `seid tx evm print-claim-specific ${claimer} ${assets.join(' ')} --from ${sender} -y`;
    try { return await execute(command); }
    catch(e) { console.log(e); }
}

async function isDocker() {
    return new Promise((resolve, reject) => {
        exec("docker ps --filter 'name=sei-node-0' --format '{{.Names}}'", (error, stdout, stderr) => {
            if (stdout.includes('sei-node-0')) {
                resolve(true)
            } else {
                resolve(false)
            }
        });
    });
}

async function executeOnAllNodes(command, interaction=`printf "12345678\\n"`){
    if (await isDocker()) {
        command = command.replace(/\.\.\//g, "/sei-protocol/sei-chain/");
        command = command.replace("/sei-protocol/sei-chain//sei-protocol/sei-chain/", "/sei-protocol/sei-chain/")
        let response;
        for(let i=0; i<4; i++) {
            const nodeCommand = `docker exec sei-node-${i} /bin/bash -c 'export PATH=$PATH:/root/go/bin:/root/.foundry/bin && ${interaction} | ${command}'`;
            response = await execCommand(nodeCommand);
        }
        return response
    }
    return await execCommand(command);
}

async function execute(command, interaction=`printf "12345678\\n"`){
    if (await isDocker()) {
        command = command.replace(/\.\.\//g, "/sei-protocol/sei-chain/");
        command = command.replace("/sei-protocol/sei-chain//sei-protocol/sei-chain/", "/sei-protocol/sei-chain/")
        command = `docker exec sei-node-0 /bin/bash -c 'export PATH=$PATH:/root/go/bin:/root/.foundry/bin && ${interaction} | ${command}'`;
    }
    return await execCommand(command);
}

function execCommand(command) {
    return new Promise((resolve, reject) => {
        exec(command, (error, stdout, stderr) => {
            if (error) {
                reject(error);
                return;
            }
            if (stderr) {
                reject(new Error(stderr));
                return;
            }
            resolve(stdout);
        });
    })
}

async function waitForReceipt(txHash) {
    let receipt = await ethers.provider.getTransactionReceipt(txHash)
    while(!receipt) {
        await delay()
        receipt = await ethers.provider.getTransactionReceipt(txHash)
    }
    return receipt
}

async function waitForBaseFeeToEq(baseFee, timeoutMs=10000) {
    const startTime = Date.now();
    while (true) {
        const block = await ethers.provider.getBlock("latest");
        const blockBaseFee = Number(block.baseFeePerGas);
        if (blockBaseFee === Number(baseFee)) {
            break
        }
        if((Date.now() - startTime) > timeoutMs) {
            throw new Error(`base fee hasn't dropped to ${baseFee} in ${timeoutMs}ms`)
        }
        await sleep(200);
    }
}

async function waitForBaseFeeToBeGt(baseFee, timeoutMs=10000) {
    const startTime = Date.now();
    while (true) {
        const block = await ethers.provider.getBlock("latest");
        const blockBaseFee = Number(block.baseFeePerGas);
        if (blockBaseFee > Number(baseFee)) {
            break
        }
        if((Date.now() - startTime) > timeoutMs) {
            throw new Error(`base fee hasn't risen above ${baseFee} in ${timeoutMs}ms`)
        }
        await sleep(200);
    }
}

function hex2uint8(hex) {
    const hex_chars = ['0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'A', 'B', 'C', 'D', 'E', 'F'];
    hex = hex.toUpperCase();
    let uint8 = new Uint8Array(Math.floor(hex.length/2));
    for (let i=0; i < Math.floor(hex.length/2); i++) {
      uint8[i] = hex_chars.indexOf(hex[i*2])*16;
      uint8[i] += hex_chars.indexOf(hex[i*2+1]);
    }
    return uint8;
}

module.exports = {
    fundAddress,
    fundSeiAddress,
    getSeiBalance,
    storeWasm,
    deployWasm,
    instantiateWasm,
    createTokenFactoryTokenAndMint,
    getChainId,
    getGasPrice,
    execute,
    getSeiAddress,
    getEvmAddress,
    queryWasm,
    rawHttpDebugTraceWithCallTracer,
    executeWasm,
    getAdmin,
    setupSigners,
    deployEvmContract,
    deployErc20PointerForCw20,
    deployErc20PointerNative,
    deployErc721PointerForCw721,
    deployErc1155PointerForCw1155,
    registerPointerForERC20,
    registerPointerForERC721,
    registerPointerForERC1155,
    getPointerForNative,
    proposeCW20toERC20Upgrade,
    importKey,
    getNativeAccount,
    associateKey,
    delay,
    bankSend,
    evmSend,
    waitForReceipt,
    getCosmosTx,
    getEvmTx,
    isDocker,
    testAPIEnabled,
    incrementPointerVersion,
    associateWasm,
    generateWallet,
    printClaimMsg,
    printClaimMsgBySender,
    printClaimSpecificMsg,
    addKey,
    getKeySeiAddress,
    hex2uint8,
    WASM,
    ABI,
    waitForBaseFeeToEq,
    waitForBaseFeeToBeGt,
};
