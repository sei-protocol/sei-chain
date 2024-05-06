const { exec } = require("child_process"); // Importing exec from child_process

const adminKeyName = "admin"

function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

async function delay() {
    await sleep(1000)
}

async function fundAddress(addr) {
    return await execute(`seid tx evm send ${addr} 10000000000000000000 --from ${adminKeyName}`);
}

async function fundAddressCosmos(addr) {
    const adminSeiAddr = (await getAdmin()).seiAddress
    return await execute(`seid tx bank send ${adminSeiAddr} ${addr} 10000000000usei --fees 20000usei -b block -y`)
}

async function getAdmin() {
    await associateAdmin(adminKeyName)
    const seiAddress = await getAdminSeiAddress()
    const evmAddress = await getEvmAddress(seiAddress)
    return {
        seiAddress,
        evmAddress
    }
}

async function getKey(name) {
    await associateKey(name)
    const seiAddress = await getKeyAddress(name)
    const evmAddress = await getEvmAddress(seiAddress)
    return {
        seiAddress,
        evmAddress
    }
}

async function getAdminSeiAddress() {
    return (await execute(`seid keys show ${adminKeyName} -a`)).trim()
}

async function addKey(name) {
    console.log("executing seid keys add ", name)
    const res = await execute(`printf "y" | seid keys add ${name} 2> /dev/null`)
    console.log("add key res = ", res )
    const addrs = await getKey(name)
    await fundAddressCosmos(addrs.seiAddress)
    await associateAdmin(name)
    return addrs
}

async function getKeyAddress(name) {
    return (await execute(`seid keys show ${name} -a`)).trim()
}

async function associateAdmin(name) {
    try {
        return await execute(`seid tx evm associate-address --from ${name}`)
    }catch(e){
        console.log("skipping associate")
    }
}

async function associateKey(name) {
    try {
        return await execute(`seid tx evm associate-address --from ${name}`)
    } catch(e) {
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

async function storeWasm(path) {
    const command = `seid tx wasm store ${path} --from ${adminKeyName} --gas=10000000 --fees=1000000usei -y --broadcast-mode block -o json`
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

async function deployErc20PointerForCw20(provider, cw20Address) {
    const command = `seid tx evm call-precompile pointer addCW20Pointer ${cw20Address} --from=admin -b block`
    const output = await execute(command);
    const txHash = output.replace(/.*0x/, "0x").trim()
    let attempt = 0;
    while(attempt < 10) {
        const receipt = await provider.getTransactionReceipt(txHash);
        if(receipt) {
            return (await getPointerForCw20(cw20Address)).pointer
        }
        await sleep(500)
        attempt++
    }
    throw new Error("contract deployment failed")
}

async function deployErc721PointerForCw721(provider, cw721Address) {
    const command = `seid tx evm call-precompile pointer addCW721Pointer ${cw721Address} --from=admin -b block`
    const output = await execute(command);
    const txHash = output.replace(/.*0x/, "0x").trim()
    let attempt = 0;
    while(attempt < 10) {
        const receipt = await provider.getTransactionReceipt(txHash);
        if(receipt) {
            return (await getPointerForCw721(cw721Address)).pointer
        }
        await sleep(500)
        attempt++
    }
    throw new Error("contract deployment failed")
}

async function deployWasm(path, adminAddr, label, args = {}) {
    const codeId = await storeWasm(path)
    return await instantiateWasm(codeId, adminAddr, label, args)
}

async function instantiateWasm(codeId, adminAddr, label, args = {}) {
    const jsonString = JSON.stringify(args).replace(/"/g, '\\"');
    const command = `seid tx wasm instantiate ${codeId} "${jsonString}" --label ${label} --admin ${adminAddr} --from ${adminKeyName} --gas=5000000 --fees=1000000usei -y --broadcast-mode block -o json`;
    const output = await execute(command);
    const response = JSON.parse(output);
    return getEventAttribute(response, "instantiate", "_contract_address");
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


async function deployEvmContract(name, args=[]) {
    const Contract = await ethers.getContractFactory(name);
    const contract = await Contract.deploy(...args);
    console.log("contract = ", contract)
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

async function executeWasm(contractAddress, msg, coins="0usei") {
    return executeWasmWithSigner(contractAddress, msg, adminKeyName, coins);
}

async function executeWasmWithSigner(contractAddress, msg, keyName, coins="0usei"){
    const jsonString = JSON.stringify(msg).replace(/"/g, '\\"'); // Properly escape JSON string
    const command = `seid tx wasm execute ${contractAddress} "${jsonString}" --amount ${coins} --from ${keyName} --gas=5000000 --fees=1000000usei -y --broadcast-mode block -o json`;
    console.log("command = ", command)
    const output = await execute(command);
    return JSON.parse(output);
}

async function execute(command) {
    console.log("executing command = ", command)
    return new Promise((resolve, reject) => {
        // Check if the Docker container 'sei-node-0' is running
        exec("docker ps --filter 'name=sei-node-0' --format '{{.Names}}'", (error, stdout, stderr) => {
            if (stdout.includes('sei-node-0')) {
                // The container is running, modify the command to execute inside Docker
                command = command.replace(/\.\.\//g, "/sei-protocol/sei-chain/");
                const dockerCommand = `docker exec sei-node-0 /bin/bash -c 'export PATH=$PATH:/root/go/bin:/root/.foundry/bin && printf "12345678\\n" | ${command}'`;
                execCommand(dockerCommand, resolve, reject);
            } else {
                // The container is not running, execute command normally
                execCommand(command, resolve, reject);
            }
        });
    });
}

function execCommand(command, resolve, reject) {
    exec(command, (error, stdout, stderr) => {
        if (error) {
            console.log("in execCommand error = ", error)
            reject(error);
            return;
        }
        if (stderr) {
            console.log("in execCommand stderr = ", stderr)
            reject(new Error(stderr));
            return;
        }
        resolve(stdout);
    });
}

module.exports = {
    fundAddress,
    storeWasm,
    deployWasm,
    instantiateWasm,
    execute,
    getSeiAddress,
    getEvmAddress,
    queryWasm,
    executeWasm,
    executeWasmWithSigner,
    getAdmin,
    getKey,
    addKey,
    setupSigners,
    deployEvmContract,
    deployErc20PointerForCw20,
    deployErc721PointerForCw721,
};
