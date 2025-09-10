const { execSync } = require('child_process');

function shell(cmd) {
  console.log(`Executing: ${cmd}`);
  execSync(cmd, { stdio: 'inherit' });
}

describe("CW20 Pointer Send Test", () => {
  it("Associates and sends CW20 token", () => {
    const sender = "sei1xxxx...";  // Replace with real CW20 contract
    const receiver = "sei1yyyy...";  // Contract with `receive()`
    const from = "sei1zzzz...";  // Signer wallet
    const amount = "10";
    const payload = Buffer.from(JSON.stringify({ stake: {} })).toString('base64');

    shell(`seid tx evm associate-contract-address ${receiver} --from ${from} --fees 20000usei --chain-id pacific-1 -b block`);
    shell(`seid tx wasm execute ${sender} '{"send":{"contract":"${receiver}","amount":"${amount}","msg":"${payload}"}}' --from ${from} --fees 500000usei --gas 200000 --chain-id pacific-1 -b block`);
  });
});
