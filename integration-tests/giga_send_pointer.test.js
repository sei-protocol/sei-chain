const { execSync } = require('child_process');

function shell(cmd) {
  console.log(`Executing: ${cmd}`);
  execSync(cmd, { stdio: 'inherit' });
}

const sender = process.env.CW20_SENDER_CONTRACT;
const receiver = process.env.CW20_RECEIVER_CONTRACT;
const from = process.env.CW20_SIGNER;

if (!sender || !receiver || !from) {
  describe.skip('CW20 Pointer Send Test', () => {
    it('requires CW20_SENDER_CONTRACT, CW20_RECEIVER_CONTRACT, and CW20_SIGNER env vars', () => {});
  });
} else {
  describe('CW20 Pointer Send Test', () => {
    it('associates and sends CW20 token', () => {
      const amount = process.env.CW20_SEND_AMOUNT || '10';
      const payload = Buffer.from(JSON.stringify({ stake: {} })).toString('base64');

      shell(`seid tx evm associate-contract-address ${receiver} --from ${from} --fees 20000usei --chain-id pacific-1 -b block`);
      shell(`seid tx wasm execute ${sender} '{"send":{"contract":"${receiver}","amount":"${amount}","msg":"${payload}"}}' --from ${from} --fees 500000usei --gas 200000 --chain-id pacific-1 -b block`);
    });
  });
}

