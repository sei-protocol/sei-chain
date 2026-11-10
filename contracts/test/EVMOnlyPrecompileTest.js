const { expect } = require("chai");
const hre = require("hardhat");
const { ethers } = hre;
const { isEvmOnlyChain, mineTransferBlock, waitForCondition } = require("./lib");

// The precompiles the EVM-only chain runs, called with ordinary EVM transactions and calls. The
// cluster must start with EVM governance on (EVM_GOVERNANCE_VOTING_PERIOD), which registers both.
const GOV_ADDRESS = "0x0000000000000000000000000000000000001006";
const PARAMS_ADDRESS = "0x0000000000000000000000000000000000001013";
const GOV_ABI = require("../../giga/evmonly/precompiles/gov/abi.json");
const PARAMS_ABI = require("../../giga/evmonly/precompiles/params/abi.json");

// The docker scripts give node N's governance vote to account N of this mnemonic, with one unit
// of voting power each.
const VOTER_MNEMONIC = "test test test test test test test test test test test junk";
const VOTER_ADDRESSES = [
  "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
  "0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
  "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC",
  "0x90F79bf6EB2c4f870365E785982E1f101E93b906",
];

const DEPOSIT_PERIOD = 1n;
const VOTING_PERIOD = 2n;
const PASSED = 3n;
const YES = 1;
const QUORUM_KEY = "tallyparams.quorum";

describe("EVM-only precompiles", function () {
  this.timeout(5 * 60 * 1000);
  const gasPrice = ethers.parseUnits("100", "gwei");
  const abi = ethers.AbiCoder.defaultAbiCoder();

  let owner;
  let voters;
  let gov;
  let params;
  let govParams;

  before(async function () {
    if (!(await isEvmOnlyChain())) {
      this.skip();
    }
    [owner] = await ethers.getSigners();
    voters = VOTER_ADDRESSES.map((address, i) => {
      const wallet = ethers.HDNodeWallet.fromPhrase(VOTER_MNEMONIC, undefined, `m/44'/60'/0'/0/${i}`);
      expect(wallet.address).to.equal(address);
      return wallet.connect(ethers.provider);
    });
    gov = new ethers.Contract(GOV_ADDRESS, GOV_ABI, owner);
    params = new ethers.Contract(PARAMS_ADDRESS, PARAMS_ABI, owner);
    govParams = await gov.params();
  });

  // submit sends a submit call with overrides and returns the proposal id its SubmitProposal event
  // names.
  async function submit(send, value) {
    const receipt = await (await send({ value, gasPrice })).wait();
    expect(receipt.status).to.equal(1);
    const event = receipt.logs
      .filter((log) => log.address.toLowerCase() === GOV_ADDRESS)
      .map((log) => gov.interface.parseLog(log))
      .find((parsed) => parsed?.name === "SubmitProposal");
    expect(event, "SubmitProposal event").to.not.equal(undefined);
    return event.args.proposalId;
  }

  // passWithVoters votes yes on id from three of the four voters and returns the proposal once
  // its tally has run.
  async function passWithVoters(id) {
    for (const voter of voters.slice(0, 3)) {
      const receipt = await (await gov.connect(voter).vote(id, YES, { gasPrice })).wait();
      expect(receipt.status).to.equal(1);
    }
    // The tally runs at the end of the first block past the voting period, and the chain only
    // makes blocks for transactions.
    await waitForCondition(async () => {
      await mineTransferBlock(owner);
      return (await gov.proposal(id)).status !== VOTING_PERIOD;
    }, `proposal ${id} to be tallied`, 3 * 60 * 1000, 1000);
    return gov.proposal(id);
  }

  async function quorum() {
    const [value] = abi.decode(["uint256"], await params.param("gov", QUORUM_KEY));
    return value;
  }

  describe("params", function () {
    it("serves gov's params as the gov precompile reads them", async function () {
      expect(await quorum()).to.equal(govParams.quorum);
      const [votingPeriod] = abi.decode(["uint64"], await params.param("gov", "votingparams.voting_period"));
      expect(votingPeriod).to.equal(govParams.votingPeriod);
    });

    it("refuses an unknown subspace or key", async function () {
      await expect(params.param("notasubspace", QUORUM_KEY)).to.be.rejectedWith("unknown param subspace");
      await expect(params.param("gov", "notakey")).to.be.rejectedWith("unknown param key");
    });

    it("refuses V2's string-valued params call, naming param", async function () {
      await expect(params.params("gov", QUORUM_KEY)).to.be.rejectedWith("use param(string,string)");
    });
  });

  describe("gov", function () {
    it("passes a text proposal the voters vote for", async function () {
      const id = await submit(
        (overrides) => gov.submitTextProposal("EVM-only text proposal", "integration test", false, overrides),
        govParams.minDeposit,
      );
      expect((await gov.proposal(id)).status).to.equal(VOTING_PERIOD);

      const proposal = await passWithVoters(id);
      expect(proposal.status).to.equal(PASSED);
      expect(proposal.finalTallyResult.yes).to.be.greaterThan(0n);
      expect(proposal.finalTallyResult.no).to.equal(0n);
    });

    it("opens voting once deposits reach the minimum deposit", async function () {
      const id = await submit(
        (overrides) => gov.submitTextProposal("EVM-only deposit", "integration test", false, overrides),
        0n,
      );
      expect((await gov.proposal(id)).status).to.equal(DEPOSIT_PERIOD);

      await expect(gov.deposit(id, { value: 0n, gasPrice })).to.be.rejectedWith("non-zero");
      const receipt = await (await gov.deposit(id, { value: govParams.minDeposit, gasPrice })).wait();
      expect(receipt.status).to.equal(1);

      const proposal = await gov.proposal(id);
      expect(proposal.status).to.equal(VOTING_PERIOD);
      expect(proposal.totalDeposit).to.equal(govParams.minDeposit);
      expect((await gov.getDeposit(id, owner.address)).amount).to.equal(govParams.minDeposit);
    });

    it("applies a passed param change, read back through params", async function () {
      const before = await quorum();
      const next = before === ethers.parseEther("0.4") ? ethers.parseEther("0.45") : ethers.parseEther("0.4");
      const change = { subspace: "gov", key: QUORUM_KEY, value: abi.encode(["uint256"], [next]) };
      const id = await submit(
        (overrides) => gov.submitParamChangeProposal("EVM-only quorum", "integration test", false, [change], overrides),
        govParams.minDeposit,
      );

      expect((await passWithVoters(id)).status).to.equal(PASSED);
      expect(await quorum()).to.equal(next);
      expect((await gov.params()).quorum).to.equal(next);
    });

    it("refuses a proposal without a title", async function () {
      await expect(gov.submitTextProposal("", "integration test", false, { gasPrice })).to.be.rejectedWith(
        "title and description are required",
      );
    });

    it("refuses V2's JSON submitProposal, naming the methods to use", async function () {
      await expect(gov.submitProposal("{}", { gasPrice })).to.be.rejectedWith("use submitTextProposal");
    });

    it("refuses a vote on an unknown proposal", async function () {
      await expect(gov.connect(voters[0]).vote(1_000_000n, YES, { gasPrice })).to.be.rejected;
    });
  });
});
