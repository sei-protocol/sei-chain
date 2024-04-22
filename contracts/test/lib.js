const { exec } = require("child_process"); // Importing exec from child_process

async function fundAddress(addr) {
    return await execute(`seid tx evm send ${addr} 10000000000000000000 --from admin`);
}

async function getAdmin() {
    await associateAdmin()
    const seiAddress = await getAdminSeiAddress()
    const evmAddress = await getEvmAddress(seiAddress)
    return {
        seiAddress,
        evmAddress
    }
}

async function getAdminSeiAddress() {
    return (await execute(`seid keys show admin -a`)).trim()
}

async function associateAdmin() {
    try {
        const result = await execute(`seid tx evm associate-address --from admin`)
        console.log(result)
        return result
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

async function storeWasm(path) {
    const command = `seid tx wasm store ${path} --from admin --gas=5000000 --fees=1000000usei -y --broadcast-mode block -o json`
    const output = await execute(command);
    const response = JSON.parse(output)
    return getEventAttribute(response, "store_code", "code_id")
}

async function instantiateWasm(codeId, adminAddr, label, args = {}) {
    const jsonString = JSON.stringify(args).replace(/"/g, '\\"');
    const command = `seid tx wasm instantiate ${codeId} "${jsonString}" --label ${label} --admin ${adminAddr} --from admin --gas=5000000 --fees=1000000usei -y --broadcast-mode block -o json`;
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
    await contract.waitForDeployment()
    return contract;
}

async function setupSigners(signers) {
    const result = []
    for(let signer of signers) {
        const evmAddress = await signer.getAddress();
        await fundAddress(evmAddress);
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

async function executeWasm(contractAddress, msg, args = {}, coins = "0usei") {
    const jsonString = JSON.stringify(msg).replace(/"/g, '\\"'); // Properly escape JSON string
    const command = `seid tx wasm execute ${contractAddress} "${jsonString}" --amount ${coins} --from admin --gas=5000000 --fees=1000000usei -y --broadcast-mode block -o json`;
    console.log(command)
    const output = await execute(command);
    return JSON.parse(output);
}

async function execute(command) {
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
            reject(error);
            return;
        }
        if (stderr) {
            reject(new Error(stderr));
            return;
        }
        resolve(stdout);
    });
}

module.exports = {
    fundAddress,
    storeWasm,
    instantiateWasm,
    execute,
    getSeiAddress,
    getEvmAddress,
    queryWasm,
    executeWasm,
    getAdmin,
    setupSigners,
    deployEvmContract,
};