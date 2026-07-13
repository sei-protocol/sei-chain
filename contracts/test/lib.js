const { exec } = require("child_process");
const {ethers} = require("hardhat"); // Importing exec from child_process
const axios = require("axios");
const crypto = require("crypto");

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

async function mineTransferBlock(sender) {
    // Progress-only self-transfer: under allow_empty_blocks=false, tests must
    // actively create a block when they need "the next block" to exist.
    const tx = await sender.sendTransaction({
        to: sender.address,
        value: 1n,
        gasPrice: ethers.parseUnits('100', 'gwei'),
    })
    return await waitForReceipt(tx.hash)
}

// Default 2 because the very next block after submit can be empty
// Like provider.getTransactionReceipt, but treats the Autobahn-specific
// "requested height N is not yet available; safe latest is N-1"
// transient as "no receipt yet" (null). That error fires in the
// narrow race between a tx being indexed in block N and block N
// becoming safe-latest; it should not propagate out of polling loops.
async function tryGetReceipt(provider, txHash) {
    try {
        return await provider.getTransactionReceipt(txHash)
    } catch (e) {
        if (String(e?.message || e).includes("not yet available")) return null
        throw e
    }
}

// Poll an arbitrary side-effect check until it returns truthy. Used by
// helpers that need to wait for a -b sync tx to take effect: instead of
// polling `seid q tx <hash>` (which doesn't work under Autobahn — the
// cosmos tx indexer isn't wired), each caller passes a closure that
// queries the actual state it cares about (e.g. account balance, denom
// existence). Works under both Autobahn and legacy because the check
// goes through whatever query path the caller already relies on.
async function waitForCondition(check, description, timeoutMs=30000, intervalMs=200) {
    const deadline = Date.now() + timeoutMs
    while (Date.now() < deadline) {
        try {
            if (await check()) return
        } catch (e) {
            // tolerate transient query failures; retry until deadline
        }
        await sleep(intervalMs)
    }
    throw new Error(`timed out waiting for ${description} within ${timeoutMs}ms`)
}

async function getCosmosTx(provider, evmTxHash) {
    return await provider.send("sei_getCosmosTx", [evmTxHash])
}

async function getEvmTx(provider, cosmosTxHash) {
    return await provider.send("sei_getEvmTx", [cosmosTxHash])
}

async function fundAddress(addr, amount="1000000000000000000000") {
    return await evmSend(addr, adminKeyName, amount)
}

async function evmSend(addr, fromKey, amount="10000000000000000000000000") {
    // seid tx evm send prints "Transaction hash: 0x..." on its own format
    // (not the standard cosmos JSON response), so we extract from text and
    // wait via the JSON-RPC receipt — semantically equivalent to -b block.
    const output = await execute(`seid tx evm send ${addr} ${amount} --from ${fromKey} -b sync -y`);
    const evmTxHash = output.replace(/.*0x/, "0x").trim()
    await waitForReceipt(evmTxHash)
    return evmTxHash
}

async function bankSend(toAddr, fromKey, amount="100000000000", denom="usei") {
    const senderAddr = await getKeySeiAddress(fromKey)
    // Non-self-send: recipient's `denom` balance goes up by `amount`.
    // Self-send: `denom` amount cancels; the recipient (also the sender)
    // pays fees in usei, so usei drops. Track whichever balance is
    // expected to change post-commit so the wait is a side-effect check.
    const trackedDenom = senderAddr === toAddr ? "usei" : denom
    const before = await getSeiBalanceBigInt(toAddr, trackedDenom)
    const result = await execute(`seid tx bank send ${fromKey} ${toAddr} ${amount}${denom} -b sync -o json --fees 20000usei -y`);
    const parsed = JSON.parse(result)
    if (parsed.code !== 0) throw new Error(`bank send rejected: ${parsed.raw_log}`)
    await waitForCondition(
        async () => (await getSeiBalanceBigInt(toAddr, trackedDenom)) !== before,
        `${toAddr} ${trackedDenom} balance to change from ${before}`,
    )
    return result
}

async function fundSeiAddress(seiAddr, amount="100000000000", denom="usei", funder=adminKeyName) {
    const before = await getSeiBalanceBigInt(seiAddr, denom)
    const result = await execute(`seid tx bank send ${funder} ${seiAddr} ${amount}${denom} -b sync -o json --fees 20000usei -y`);
    const parsed = JSON.parse(result)
    if (parsed.code !== 0) throw new Error(`fundSeiAddress rejected: ${parsed.raw_log}`)
    const target = before + BigInt(amount)
    await waitForCondition(
        async () => (await getSeiBalanceBigInt(seiAddr, denom)) >= target,
        `${seiAddr} ${denom} balance >= ${target}`,
    )
    return result
}

// BigInt variant of getSeiBalance. Genesis-funded accounts hold balances
// well above 2^53, where JS Number arithmetic loses precision (a
// `before + amount` comparison can silently round wrong). Polling
// helpers should use this; existing Number-returning getSeiBalance is
// kept for callers that don't run into the precision range.
async function getSeiBalanceBigInt(seiAddr, denom="usei") {
    const result = await execute(`seid query bank balances ${seiAddr} -o json`);
    const balances = JSON.parse(result)
    for(let b of balances.balances) {
        if(b.denom === denom) {
            return BigInt(b.amount)
        }
    }
    return 0n
}

// Causal commit signal: returns the on-chain account sequence for
// seiAddr, or null if the account doesn't exist yet. A sender's
// sequence advances atomically when its tx is included in a block,
// regardless of whether the tx's intended side effect (e.g. a balance
// credit) happened. Use this when the natural side-effect check can't
// distinguish "tx hasn't committed" from "tx committed but no-op'd"
// (e.g. bank send to a post-association cast address).
async function getAccountSequence(seiAddr) {
    try {
        const out = await execute(`seid query account ${seiAddr} -o json`)
        return parseInt(JSON.parse(out).sequence ?? "0", 10)
    } catch (e) {
        return null
    }
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
    // Skip funding the admin from admin — it's a no-op self-send that burns
    // fees and (under -b sync) leaves the helper polling for a balance change
    // that, for self-sends, is just -fees rather than +amount.
    if (keyName !== adminKeyName) {
        await fundSeiAddress(seiAddress)
    }
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

async function waitForProposalStatus(
    proposalId,
    targetStatus,
    { kickKeyName = adminKeyName, pollIntervalMs = 1000 } = {},
) {
    const readProposal = async () => JSON.parse(await execute(`seid q gov proposal ${proposalId} -o json`))
    const ensureNotFailed = (status) => {
        if (
            (status === "PROPOSAL_STATUS_REJECTED" || status === "PROPOSAL_STATUS_FAILED") &&
            status !== targetStatus
        ) {
            throw new Error(`Proposal ${proposalId} was rejected/failed with status: ${status}`)
        }
    }

    const kickAddr = await getKeySeiAddress(kickKeyName)

    while (true) {
        const proposal = await readProposal()
        if (proposal.status === targetStatus) {
            return proposal
        }
        ensureNotFailed(proposal.status)

        const votingEndMs = Date.parse(proposal.voting_end_time ?? "")
        if (!Number.isFinite(votingEndMs)) {
            throw new Error(`Proposal ${proposalId} is missing a valid voting_end_time`)
        }

        if (Date.now() >= votingEndMs + 1000) {
            // Progress-only bank send: once the currently observed voting end
            // time has passed, force one committed block so tallying can
            // advance. Expedited proposals can convert to regular and extend
            // voting_end_time, so re-read proposal state after every kick.
            await bankSend(kickAddr, kickKeyName, "1", "usei")
        }

        await sleep(pollIntervalMs)
    }
}

// Best-effort helper for idempotent bootstrap paths that may run after an
// address is already associated.
async function associateKey(keyName) {
    try {
        const seiAddress = await getKeySeiAddress(keyName)
        // seid tx evm associate-address has a custom (non-cosmos-JSON) output
        // format. The try/catch already tolerates failure here, and subsequent
        // associate calls will succeed once the chain catches up.
        await execute(`seid tx evm associate-address --from ${keyName} -b sync`)
        await waitForCondition(
            async () => (await getEvmAddressAssociation(seiAddress)).associated === true,
            `${seiAddress} to have an associated EVM address`,
        )
    } catch (e) {
        console.log(`skipping associate for ${keyName}`)
        console.log(e)
    }
}

// Strict helper for tests that are explicitly asserting association behavior.
async function associateKeyStrict(keyName) {
    const seiAddress = await getKeySeiAddress(keyName)
    await execute(`seid tx evm associate-address --from ${keyName} -b sync`)
    await waitForCondition(
        async () => (await getEvmAddressAssociation(seiAddress)).associated === true,
        `${seiAddress} to have an associated EVM address`,
    )
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
    const fromSeiAddr = await getKeySeiAddress(from)
    // Tokenfactory denom is deterministic: factory/<creator-bech32>/<subdenom>.
    const token_denom = `factory/${fromSeiAddr}/${name}`
    const target = BigInt(amount)

    // Step 1: create-denom. Wait for the denom to exist before submitting
    // the next tx — each `seid tx` reads the node's committed account
    // sequence, so consecutive -b sync submissions from the same key
    // without a between-commit wait construct duplicate-sequence txs and
    // get rejected with "account sequence mismatch".
    const create_cmd = `seid tx tokenfactory create-denom ${name} --from ${from} --gas=5000000 --fees=1000000usei -y --broadcast-mode sync -o json`
    const response = JSON.parse(await execute(create_cmd))
    if (response.code !== 0) throw new Error(`create-denom rejected: ${response.raw_log}`)
    await waitForCondition(
        async () => {
            try {
                const out = await execute(`seid query tokenfactory denom-authority-metadata ${token_denom} --output json`)
                // For a non-existent denom this query returns {"authority_metadata":{"admin":""}}
                // (no error). Only a non-empty admin indicates the create-denom is committed.
                return (JSON.parse(out).authority_metadata?.admin || '') !== ''
            } catch (e) { return false }
        },
        `denom ${token_denom} to be created`,
    )

    // Step 2: mint to ${from}. Wait until the creator holds the minted
    // amount before the bank send, for the same sequence reason.
    const mint_cmd = `seid tx tokenfactory mint ${amount}${token_denom} --from ${from} --gas=5000000 --fees=1000000usei -y --broadcast-mode sync -o json`
    const mintResp = JSON.parse(await execute(mint_cmd))
    if (mintResp.code !== 0) throw new Error(`mint rejected: ${mintResp.raw_log}`)
    await waitForCondition(
        async () => (await getSeiBalanceBigInt(fromSeiAddr, token_denom)) >= target,
        `${from} ${token_denom} balance >= ${target}`,
    )

    // Step 3: bank send to recipient. Wait for the recipient to hold the amount.
    const send_cmd = `seid tx bank send ${from} ${recipient} ${amount}${token_denom} --from ${from} --gas=5000000 --fees=1000000usei -y --broadcast-mode sync -o json`
    const sendResp = JSON.parse(await execute(send_cmd))
    if (sendResp.code !== 0) throw new Error(`bank send rejected: ${sendResp.raw_log}`)
    await waitForCondition(
        async () => (await getSeiBalanceBigInt(recipient, token_denom)) >= target,
        `${recipient} ${token_denom} balance >= ${target}`,
    )
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

// Highest existing wasm code_id, or 0 if the chain has no codes yet.
// Used as the side-effect signal for storeWasm: after a successful
// store, the max code_id grows by one.
async function getMaxWasmCodeId() {
    try {
        const out = await execute(`seid query wasm list-code --reverse --limit 1 -o json`)
        const codes = JSON.parse(out).code_infos || []
        return codes.length === 0 ? 0 : Number(codes[0].code_id)
    } catch (e) {
        return 0
    }
}

// Contracts instantiated under codeId. Used as the side-effect signal
// for instantiateWasm: after a successful instantiation, the list
// grows by exactly one entry.
async function listContractsByCode(codeId) {
    try {
        const out = await execute(`seid query wasm list-contract-by-code ${codeId} -o json`)
        return JSON.parse(out).contracts || []
    } catch (e) {
        return []
    }
}

async function storeWasm(path, from=adminKeyName) {
    const codeIdBefore = await getMaxWasmCodeId()
    const command = `seid tx wasm store ${path} --from ${from} --gas=5000000 --fees=1000000usei -y -b sync -o json`
    const response = JSON.parse(await execute(command))
    if (response.code !== 0) throw new Error(`storeWasm failed: ${response.raw_log}`)
    // Capture the new code_id inside the wait: a second re-query after the
    // wait would race with any other concurrent store landing in the same
    // window. Serial test execution makes that race dormant today, but
    // capture-during-wait closes it deterministically.
    let newCodeId = 0
    try {
        await waitForCondition(
            async () => {
                const cur = await getMaxWasmCodeId()
                if (cur > codeIdBefore) { newCodeId = cur; return true }
                return false
            },
            `new wasm code_id > ${codeIdBefore}`,
        )
    } catch (e) {
        // CheckTx accepted the tx but the new code never appeared on chain
        // (rejected at DeliverTx — e.g. wasm is disabled via gov). Wrap
        // with the "storeWasm failed" prefix that callers match on.
        throw new Error(`storeWasm failed: ${e.message}`)
    }
    return String(newCodeId)
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
    let command = `seid tx evm register-evm-pointer CW20 ${cw20Address} --from=${from} -b sync`
    if (evmRpc) {
        command = command + ` --evm-rpc=${evmRpc}`
    }
    const output = await execute(command);
    const txHash = output.replace(/.*0x/, "0x").trim()
    let attempt = 0;
    while(attempt < attempts) {
        const receipt = await tryGetReceipt(provider, txHash);
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
    let command = `seid tx evm call-precompile pointer addNativePointer ${name} --from=${from} -b sync`
    if (evmRpc) {
        command = command + ` --evm-rpc=${evmRpc}`
    }
    const output = await execute(command);
    const txHash = output.replace(/.*0x/, "0x").trim()
    let attempt = 0;
    while(attempt < 10) {
        const receipt = await tryGetReceipt(provider, txHash);
        if(receipt) {
            return (await getPointerForNative(name)).pointer
        }
        await sleep(500)
        attempt++
    }
    throw new Error("contract deployment failed")
}

async function deployErc721PointerForCw721(provider, cw721Address, from=adminKeyName, evmRpc="") {
    let command = `seid tx evm register-evm-pointer CW721 ${cw721Address} --from=${from} -b sync`
    if (evmRpc) {
        command = command + ` --evm-rpc=${evmRpc}`
    }
    const output = await execute(command);
    const txHash = output.replace(/.*0x/, "0x").trim()
    let attempt = 0;
    while(attempt < 10) {
        const receipt = await tryGetReceipt(provider, txHash);
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
    let command = `seid tx evm register-evm-pointer CW1155 ${cw1155Address} --from=${from} -b sync`
    if (evmRpc) {
        command = command + ` --evm-rpc=${evmRpc}`
    }
    const output = await execute(command);
    const txHash = output.replace(/.*0x/, "0x").trim()
    let attempt = 0;
    while(attempt < 10) {
        const receipt = await tryGetReceipt(provider, txHash);
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
    const contractsBefore = new Set(await listContractsByCode(codeId))
    const jsonString = JSON.stringify(args).replace(/"/g, '\\"');
    const command = `seid tx wasm instantiate ${codeId} "${jsonString}" --label ${label} --admin ${adminAddr} --from ${from} --gas=5000000 --fees=1000000usei -y -b sync -o json`;
    const response = JSON.parse(await execute(command));
    if (response.code !== 0) throw new Error(`instantiateWasm failed: ${response.raw_log}`)
    // Capture the new contract address inside the wait: a second re-query
    // after the wait would race with any other concurrent instantiation
    // under the same codeId. Serial tests make that race dormant today,
    // but capture-during-wait closes it deterministically.
    let newContract = null
    try {
        await waitForCondition(
            async () => {
                const cur = await listContractsByCode(codeId)
                newContract = cur.find(c => !contractsBefore.has(c))
                return newContract != null
            },
            `new contract under code ${codeId}`,
        )
    } catch (e) {
        // CheckTx accepted the tx but no new contract appeared on chain
        // (rejected at DeliverTx — e.g. wasm is disabled via gov). Wrap
        // with the "instantiateWasm failed" prefix that callers match on.
        throw new Error(`instantiateWasm failed: ${e.message}`)
    }
    return newContract
}

async function proposeCW20toERC20Upgrade(erc20Address, cw20Address, title="erc20-pointer", version=99, description="erc20 pointer",fees="200000usei", from=adminKeyName) {
    const maxIdBefore = await maxProposalId()
    const command = `seid tx evm add-cw-erc20-pointer "${title}" "${description}" ${erc20Address} ${version} 200000000usei ${cw20Address} --from ${from} --fees ${fees} -y -o json -b sync`
    const response = JSON.parse(await execute(command))
    if (response.code !== 0) throw new Error(`proposeCW20toERC20Upgrade failed: ${response.raw_log}`)
    const proposalId = await findProposalByTitle(title, maxIdBefore, response.txhash)
    return await passProposal(proposalId)
}

async function proposeParamChange(title, description, changes, deposit="200000000usei", fees="200000usei", from=adminKeyName, expedited=true) {
    const proposal = {
        title,
        description,
        changes,
        deposit,
        is_expedited: expedited  // Use expedited voting (15s vs 30s on localnet)
    };
    const proposalJson = JSON.stringify(proposal);
    const tempFile = `/tmp/param_change_${Date.now()}.json`;

    // Use base64 encoding to avoid quote escaping issues in Docker
    const base64Json = Buffer.from(proposalJson).toString('base64');
    await execute(`echo ${base64Json} | base64 -d > ${tempFile}`);

    const maxIdBefore = await maxProposalId();
    const command = `seid tx gov submit-proposal param-change ${tempFile} --from ${from} --fees ${fees} -y -o json -b sync`;
    const output = await execute(command);
    await execute(`rm ${tempFile}`);
    const response = JSON.parse(output);
    if (response.code !== 0) {
        throw new Error(`Failed to submit proposal: ${response.raw_log}`);
    }
    return await findProposalByTitle(title, maxIdBefore, response.txhash);
}

// After a -b sync gov proposal submission, scan gov state for the new
// proposal whose title matches. The diff against maxIdBefore is what
// pins it to *this* submission rather than a stale prior-run proposal
// with the same title.
async function findProposalByTitle(title, maxIdBefore, txHashForError) {
    const deadline = Date.now() + 30000;
    while (Date.now() < deadline) {
        const cur = await maxProposalId();
        let queryFailed = false;
        for (let id = maxIdBefore + 1; id <= cur; id++) {
            let detail;
            try {
                detail = JSON.parse(await execute(`seid q gov proposal ${id} -o json`));
            } catch (e) {
                // Transient query failure (RPC blip, indexer lag, parse
                // error mid-flight). Leave this id for re-scan next
                // iteration rather than aborting the whole helper on a
                // single bad read.
                queryFailed = true;
                continue;
            }
            const observedTitle = detail.content?.title ?? detail.title;
            if (observedTitle === title) return String(id);
        }
        // Only advance the window if every id in the range was
        // successfully scanned. A transient miss leaves maxIdBefore
        // unchanged so the failed id is re-checked next iteration —
        // without this guard, the proposal we just submitted could be
        // permanently skipped by a single failed query, or (in the
        // original code) any transient failure threw out of the whole
        // helper.
        if (!queryFailed) {
            maxIdBefore = Math.max(maxIdBefore, cur);
        }
        await sleep(250);
    }
    throw new Error(`proposal submitted (tx ${txHashForError}) but did not appear in gov state within 30s`);
}

// Returns the highest existing proposal id, or 0 if there are no proposals.
async function maxProposalId() {
    let out;
    try {
        // seid exits non-zero ("Error: no proposals found") on an empty
        // gov set; the try/catch treats that as id=0.
        out = await execute(`seid q gov proposals --reverse --limit 1 -o json 2>/dev/null`);
    } catch (e) {
        return 0;
    }
    if (!out || !out.trim()) return 0;
    const proposals = JSON.parse(out).proposals || [];
    if (proposals.length === 0) return 0;
    return Number(proposals[0].proposal_id || proposals[0].id);
}

async function proposeDisableWasm(title="Disable WASM", description="Disable cosmwasm store code and instantiate operations", deposit="200000000usei", fees="200000usei", from=adminKeyName) {
    const changes = [
        {
            subspace: "wasm",
            key: "uploadAccess",
            value: { permission: "Nobody" }
        },
        {
            subspace: "wasm",
            key: "instantiateAccess",
            value: "Nobody"
        }
    ];
    const proposalId = await proposeParamChange(title, description, changes, deposit, fees, from);
    return proposalId;
}

async function proposeEnableWasm(title="Enable WASM", description="Enable cosmwasm store code and instantiate operations", deposit="200000000usei", fees="200000usei", from=adminKeyName) {
    const changes = [
        {
            subspace: "wasm",
            key: "uploadAccess",
            value: { permission: "Everybody" }
        },
        {
            subspace: "wasm",
            key: "instantiateAccess",
            value: "Everybody"
        }
    ];
    const proposalId = await proposeParamChange(title, description, changes, deposit, fees, from);
    return proposalId;
}

async function disableWasm(from=adminKeyName) {
    const proposalId = await proposeDisableWasm("Disable WASM", "Disable cosmwasm store code and instantiate operations", "200000000usei", "200000usei", from);
    return await passProposal(proposalId);
}

async function enableWasm(from=adminKeyName) {
    const proposalId = await proposeEnableWasm("Enable WASM", "Enable cosmwasm store code and instantiate operations", "200000000usei", "200000usei", from);
    return await passProposal(proposalId);
}

async function getWasmParams() {
    const uploadAccess = await execute(`seid query params subspace wasm uploadAccess -o json`);
    const instantiateAccess = await execute(`seid query params subspace wasm instantiateAccess -o json`);
    return {
        uploadAccess: JSON.parse(uploadAccess),
        instantiateAccess: JSON.parse(instantiateAccess)
    };
}

async function isWasmEnabled() {
    const params = await getWasmParams();
    const uploadEnabled = params.uploadAccess.value.includes("Everybody");
    const instantiateEnabled = params.instantiateAccess.value.includes("Everybody");
    return uploadEnabled && instantiateEnabled;
}

async function isWasmDisabled() {
    const params = await getWasmParams();
    const uploadDisabled = params.uploadAccess.value.includes("Nobody");
    const instantiateDisabled = params.instantiateAccess.value.includes("Nobody");
    return uploadDisabled && instantiateDisabled;
}

async function ensureWasmEnabled(from=adminKeyName) {
    if (!(await isWasmEnabled())) {
        await enableWasm(from);
    }
}

async function ensureWasmDisabled(from=adminKeyName) {
    if (!(await isWasmDisabled())) {
        await disableWasm(from);
    }
}

async function passProposal(proposalId,  desposit="200000000usei", fees="200000usei", from=adminKeyName) {
    if(await isDocker()) {
        await executeOnAllNodes(`seid tx gov vote ${proposalId} yes --from node_admin -b sync -y --fees ${fees}`)
    } else {
        await execute(`seid tx gov vote ${proposalId} yes --from ${from} -b sync -y --fees ${fees}`)
    }
    await waitForProposalStatus(proposalId, "PROPOSAL_STATUS_PASSED")
    return proposalId
}

async function registerPointerForERC20(erc20Address, fees="20000usei", from=adminKeyName) {
    return await registerPointerForCw(erc20Address, "ERC20", fees, from)
}

async function registerPointerForERC721(erc721Address, fees="20000usei", from=adminKeyName) {
    return await registerPointerForCw(erc721Address, "ERC721", fees, from)
}

async function registerPointerForERC1155(erc1155Address, fees="200000usei", from=adminKeyName) {
    return await registerPointerForCw(erc1155Address, "ERC1155", fees, from)
}

async function registerPointerForCw(cwAddress, type, fees, from) {
    const command = `seid tx evm register-cw-pointer ${type} ${cwAddress} --from ${from} --fees ${fees} -b sync -y -o json`
    const response = JSON.parse(await execute(command))
    if (response.code !== 0) throw new Error(`contract deployment failed: ${response.raw_log}`)
    let pointer = ''
    try {
        await waitForCondition(
            async () => {
                const out = await execute(`seid query evm pointer ${type} ${cwAddress} -o json`)
                pointer = JSON.parse(out).pointer || ''
                return pointer !== ''
            },
            `pointer for ${type} ${cwAddress}`,
        )
    } catch (e) {
        // CheckTx accepted the tx but no pointer was registered (rejected
        // at DeliverTx — e.g. trying to register a pointer for an address
        // that's already a pointer). Wrap with the "contract deployment
        // failed" prefix that callers match on.
        throw new Error(`contract deployment failed: ${e.message}`)
    }
    return pointer
}

async function getSeiAddress(evmAddress) {
    const command = `seid q evm sei-addr ${evmAddress} -o json`
    const output = await execute(command);
    const response = JSON.parse(output)
    return response.sei_address
}

async function getEvmAddress(seiAddress) {
    return (await getEvmAddressAssociation(seiAddress)).evm_address
}

async function getEvmAddressAssociation(seiAddress) {
    const command = `seid q evm evm-addr ${seiAddress} -o json`
    const output = await execute(command);
    return JSON.parse(output)
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

// Wrap a signer's sendTransaction with retry on "incorrect account
// sequence". Under Autobahn the post-commit window in which
// eth_getTransactionCount may briefly return a stale nonce is wider
// than under CometBFT, so an ethers-managed send right after an
// awaited prior tx can hit a one-off nonce mismatch even though the
// chain has fully processed the previous tx. The retry refetches a
// fresh nonce on the next attempt; the happy path is unaffected.
function _wrapSignerWithNonceRetry(signer) {
    if (signer.__nonceRetryWrapped) return signer
    const TX_NONCE_RETRIES = 5
    const TX_NONCE_RETRY_DELAY_MS = 500
    const original = signer.sendTransaction.bind(signer)
    signer.sendTransaction = async function(...args) {
        let lastErr
        for (let i = 0; i <= TX_NONCE_RETRIES; i++) {
            try {
                return await original(...args)
            } catch (e) {
                lastErr = e
                if (!/incorrect account sequence/i.test(e?.message || '')) throw e
                await new Promise(r => setTimeout(r, TX_NONCE_RETRY_DELAY_MS))
            }
        }
        throw lastErr
    }
    signer.__nonceRetryWrapped = true
    return signer
}

async function setupSigners(signers) {
    const result = []
    for(let signer of signers) {
        _wrapSignerWithNonceRetry(signer)
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

async function executeWasm(contractAddress, msg, coins = "0usei") {
    const jsonString = JSON.stringify(msg).replace(/"/g, '\\"'); // Properly escape JSON string
    const command = `seid tx wasm execute ${contractAddress} "${jsonString}" --amount ${coins} --from ${adminKeyName} --gas=5000000 --fees=1000000usei -y -b sync -o json`;
    return await waitForAdminTxCommit(command)
}

async function associateWasm(contractAddress) {
    const command = `seid tx evm associate-contract-address ${contractAddress} --from ${adminKeyName} --gas=5000000 --fees=1000000usei -y -b sync -o json`;
    return await waitForAdminTxCommit(command)
}

// Current latest block height as observed by the local node.
async function getCurrentBlockHeight() {
    const status = await execute(`seid status`)
    return Number(JSON.parse(status).SyncInfo.latest_block_height)
}

// Scan blocks in [fromHeight, toHeight] for the cosmos tx with the
// given hex hash; returns the height it landed in, or null if none of
// the scanned blocks contain it. Cosmos tx hash is the uppercase hex
// SHA-256 of the protobuf tx bytes, which appear base64-encoded in
// `block.data.txs`.
async function findInclusionBlock(txhashHex, fromHeight, toHeight) {
    const target = txhashHex.toUpperCase()
    for (let h = fromHeight; h <= toHeight; h++) {
        try {
            const block = JSON.parse(await execute(`seid query block ${h}`))
            const txs = block.block?.data?.txs || []
            for (const t of txs) {
                const buf = Buffer.from(t, 'base64')
                const hash = crypto.createHash('sha256').update(buf).digest('hex').toUpperCase()
                if (hash === target) return h
            }
        } catch (e) {
            // skip transient block-query failures
        }
    }
    return null
}

// Submit a -b sync tx from adminKeyName and wait for it to be included
// in a block via sender sequence advance. Returns the submit (CheckTx)
// response, with `.height` rewritten to the actual inclusion block
// (recovered by scanning blocks between submit and observed commit for
// the tx hash). Use when the natural side effect of the tx isn't easily
// queryable from the helper (e.g. wasm execute, contract association)
// and callers verify the outcome via state queries themselves.
//
// Note: the returned `code` is the CheckTx code (0 for accepted into
// mempool), not the DeliverTx code; callers can't assert tx success/
// failure from the response alone — they must inspect post-state.
async function waitForAdminTxCommit(command) {
    const senderAddr = await getKeySeiAddress(adminKeyName)
    const heightBefore = await getCurrentBlockHeight()
    const seqBefore = await getAccountSequence(senderAddr)
    const response = JSON.parse(await execute(command))
    if (response.code !== 0) return response  // CheckTx rejection — surface immediately
    await waitForCondition(
        async () => (await getAccountSequence(senderAddr)) > seqBefore,
        `${adminKeyName} sequence > ${seqBefore}`,
    )
    const heightAfter = await getCurrentBlockHeight()
    const inclusionHeight = await findInclusionBlock(response.txhash, heightBefore + 1, heightAfter)
    if (inclusionHeight !== null) {
        response.height = String(inclusionHeight)
    } else {
        // Anomalous: sender sequence advanced so the tx did land in some
        // block, but block-walk in [heightBefore+1, heightAfter] couldn't
        // find it. Likely a transient block-query failure across the whole
        // range. Surface so it's visible in test output rather than
        // silently leaving response.height at the CheckTx default.
        console.log(`waitForAdminTxCommit: tx ${response.txhash} sequence advanced but not found in blocks [${heightBefore + 1}, ${heightAfter}]; response.height left as CheckTx default`)
    }
    return response
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
    // Cap shelled-out child processes (typically `docker exec ... seid ...`)
    // with a timeout so an indefinite stall surfaces as an error with the
    // command text, instead of consuming the entire job's wall-clock budget.
    // The Autobahn EVM matrix has hit multiple 30-minute job timeouts where
    // hardhat went silent between test files; suspect path is a stalled
    // child here. Override via EXEC_TIMEOUT_MS for tests that legitimately
    // need longer.
    const timeoutMs = Number(process.env.EXEC_TIMEOUT_MS || 60000)
    return new Promise((resolve, reject) => {
        exec(command, { timeout: timeoutMs, killSignal: "SIGKILL" }, (error, stdout, stderr) => {
            if (error) {
                // Node sets error.killed=true for the two kinds of kill it
                // initiates: the timeout we configured, and a maxBuffer
                // overflow. Only the former leaves error.code unset; the
                // latter sets it to 'ERR_CHILD_PROCESS_STDIO_MAXBUFFER'.
                // Gate on !error.code so a buffer overflow isn't mis-
                // attributed as a timeout, and so external signal deaths
                // (OOM, runner cleanup — error.killed=false) keep their
                // original error.
                if (error.killed && !error.code) {
                    reject(new Error(`execCommand timed out after ${timeoutMs}ms: ${command}`))
                    return
                }
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
    while (true) {
        const receipt = await tryGetReceipt(ethers.provider, txHash)
        if (receipt) return receipt
        await delay()
    }
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
    proposeParamChange,
    proposeDisableWasm,
    proposeEnableWasm,
    disableWasm,
    enableWasm,
    getWasmParams,
    isWasmEnabled,
    isWasmDisabled,
    ensureWasmEnabled,
    ensureWasmDisabled,
    passProposal,
    importKey,
    getNativeAccount,
    associateKey,
    associateKeyStrict,
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
    waitForCondition,
    waitForProposalStatus,
    getAccountSequence,
    mineTransferBlock,
};
