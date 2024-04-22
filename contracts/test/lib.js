const { exec } = require("child_process"); // Importing exec from child_process

async function fundAddress(addr) {
    return await execute(`seid tx evm send ${addr} 10000000000000000000 --from admin`);
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

async function instantiateWasm(codeId, adminAddr, label, args={}) {
    const command = `seid tx wasm instantiate ${codeId} '${JSON.stringify(args)}' --label ${label} --admin ${adminAddr} --from admin --gas=5000000 --fees=1000000usei -y --broadcast-mode block -o json`
    const output = await execute(command);
    const response = JSON.parse(output)
    return getEventAttribute(response, "instantiate", "_contract_address")
}

async function getSeiAddress(evmAddress) {
    const command = `seid q evm sei-addr ${evmAddress} -o json`
    const output = await execute(command);
    const response = JSON.parse(output)
    return response.sei_address
}


async function deployEvmContract(name, args=[]) {
    const Contract = await ethers.getContractFactory(name);
    const contract = await Contract.deploy(...args);
    await contract.waitForDeployment()
    return contract;
}

async function queryWasm(contractAddress, operation, args={}){
    const command = `seid query wasm contract-state smart ${contractAddress} '{"${operation}": ${JSON.stringify(args)}}' --output json`
    const output = await execute(command);
    return JSON.parse(output)
}

async function execute(command) {
    // Check if the seid binary is available on the path
    const checkSeidCommand = 'command -v seid';
    return new Promise((resolve, reject) => {
        exec(checkSeidCommand, (error, stdout, stderr) => {
            if (stdout) {
                // seid is available, execute command normally
                execCommand(command, resolve, reject);
            } else {
                // seid is not available, execute command inside Docker (integration test)
                const dockerCommand = `docker exec sei-node-0 /bin/bash -c 'export PATH=$PATH:/root/go/bin:/root/.foundry/bin && printf "12345678\\n" | ${command}'`;
                execCommand(dockerCommand, resolve, reject);
            }
        });
    });
}

function execCommand(command, resolve, reject) {
    exec(command, (error, stdout, stderr) => {
        if (error) {
            console.log(`error: ${error.message}`);
            reject(error);
            return;
        }
        if (stderr) {
            console.log(`stderr: ${stderr}`);
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
    queryWasm,
    deployEvmContract,
};