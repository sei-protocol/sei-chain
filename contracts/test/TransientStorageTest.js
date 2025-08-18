const { expect } = require("chai");
const { ethers } = require("hardhat");
const { setupSigners } = require("./lib");

describe("Transient Storage Tests", function () {
    let transientStorageTester;
    let snapshotRevertTester;
    let owner;
    let addr1;
    let addr2;
0
    beforeEach(async function () {
        let signers = await ethers.getSigners();
        [owner, addr1, addr2] = await setupSigners(signers);

        const TransientStorageTester = await ethers.getContractFactory("TransientStorageTester");
        transientStorageTester = await TransientStorageTester.deploy({ gasLimit: 10000000 });

        const SnapshotRevertTester = await ethers.getContractFactory("SnapshotRevertTester");
        snapshotRevertTester = await SnapshotRevertTester.deploy({ gasLimit: 10000000 });
    });

    describe("TransientStorageTester", function () {
        it("Should test basic transient storage operations", async function () {
            const key = ethers.keccak256(ethers.toUtf8Bytes("test_key"));
            const value = 12345;

            const res = await transientStorageTester.runBasicTransientStorage(key, value, { gasLimit: 1000000 });
            const receipt = await res.wait();
            expect(receipt).to.emit(transientStorageTester, "TransientStorageSet")
                .withArgs(key, value);

            const results = await transientStorageTester.getTestResults();
            expect(results.basic).to.be.true;
        });

        it("Should test transient storage with snapshot/revert", async function () {
            const key = ethers.keccak256(ethers.toUtf8Bytes("snapshot_key"));
            const value1 = 100;
            const value2 = 200;

            const res = await transientStorageTester.runTransientStorageWithSnapshot(key, value1, value2, { gasLimit: 1000000 });
            const receipt = await res.wait();
            await expect(receipt)
                .to.emit(transientStorageTester, "SnapshotCreated")
                .and.to.emit(transientStorageTester, "SnapshotReverted");

            const results = await transientStorageTester.getTestResults();
            expect(results.snapshot).to.be.true;
        });

        it("Should test multiple transient storage keys", async function () {
            const keys = [
                ethers.keccak256(ethers.toUtf8Bytes("key1")),
                ethers.keccak256(ethers.toUtf8Bytes("key2")),
                ethers.keccak256(ethers.toUtf8Bytes("key3"))
            ];
            const values = [111, 222, 333];

            const res = await transientStorageTester.runMultipleTransientKeys(keys, values, { gasLimit: 1000000 });
            await res.wait();

            const results = await transientStorageTester.getTestResults();
            expect(results.multiple).to.be.true;
        });

        it("Should test complex snapshot scenario", async function () {
            const res = await transientStorageTester.runComplexSnapshotScenario({ gasLimit: 1000000 });
            const receipt = await res.wait();
            await expect(receipt)
                .to.emit(transientStorageTester, "SnapshotCreated")
                .and.to.emit(transientStorageTester, "SnapshotReverted");

            const results = await transientStorageTester.getTestResults();
            expect(results.complex).to.be.true;
        });

        it("Should test zero values", async function () {
            const res = await transientStorageTester.runZeroValues({ gasLimit: 1000000 });
            await res.wait();

            const results = await transientStorageTester.getTestResults();
            expect(results.zero).to.be.true;
        });

        it("Should test large values", async function () {
            const res = await transientStorageTester.runLargeValues({ gasLimit: 1000000 });
            await res.wait();

            const results = await transientStorageTester.getTestResults();
            expect(results.large).to.be.true;
        });

        it("Should test uninitialized keys", async function () {
            const res = await transientStorageTester.runUninitializedKeys({ gasLimit: 1000000 });
            await res.wait();

            const results = await transientStorageTester.getTestResults();
            expect(results.uninitialized).to.be.true;
        });

        it("Should test comparison with regular storage", async function () {
            const key = ethers.keccak256(ethers.toUtf8Bytes("comparison"));
            const value = 999;

            const res = await transientStorageTester.runTransientVsRegularStorage(key, value, { gasLimit: 1000000 });
            const receipt = await res.wait();
            await expect(receipt)
                .to.emit(transientStorageTester, "TransientStorageSet")
                .and.to.emit(transientStorageTester, "RegularStorageSet");

            const results = await transientStorageTester.getTestResults();
            expect(results.comparison).to.be.true;
        });

        it("Should run comprehensive test", async function () {
            const res = await transientStorageTester.runComprehensiveTest({ gasLimit: 2000000 });
            const receipt = await res.wait();
            await expect(receipt)
                .to.emit(transientStorageTester, "TestCompleted")
                .withArgs("comprehensive", true);

            const results = await transientStorageTester.getTestResults();
            expect(results.basic).to.be.true;
            expect(results.snapshot).to.be.true;
            expect(results.multiple).to.be.true;
            expect(results.complex).to.be.true;
            expect(results.zero).to.be.true;
            expect(results.large).to.be.true;
            expect(results.uninitialized).to.be.true;
            expect(results.comparison).to.be.true;
        });
    });

    describe("SnapshotRevertTester", function () {
        it("Should test nested calls with transient storage", async function () {
            const res = await snapshotRevertTester.runNestedCalls({ gasLimit: 1000000 });
            const receipt = await res.wait();
            await expect(receipt)
                .to.emit(snapshotRevertTester, "CallStarted")
                .and.to.emit(snapshotRevertTester, "CallEnded");

            const results = await snapshotRevertTester.getAllTestResults();
            expect(results.nested).to.be.true;
        });

        it("Should test snapshot/revert with transient storage", async function () {
            const res = await snapshotRevertTester.runSnapshotRevert({ gasLimit: 1000000 });
            const receipt = await res.wait();
            await expect(receipt)
                .to.emit(snapshotRevertTester, "SnapshotCreated")
                .and.to.emit(snapshotRevertTester, "SnapshotReverted");

            const results = await snapshotRevertTester.getAllTestResults();
            expect(results.snapshotRevert).to.be.true;
        });

        it("Should test complex snapshot scenario", async function () {
            const res = await snapshotRevertTester.runComplexSnapshotScenario({ gasLimit: 1000000 });
            const receipt = await res.wait();
            await expect(receipt)
                .to.emit(snapshotRevertTester, "SnapshotCreated")
                .and.to.emit(snapshotRevertTester, "SnapshotReverted");

            const results = await snapshotRevertTester.getAllTestResults();
            expect(results.complexSnapshot).to.be.true;
        });

        it("Should test error handling with transient storage", async function () {
            const res = await snapshotRevertTester.runErrorHandling({ gasLimit: 1000000 });
            const receipt = await res.wait();
            await expect(receipt)
                .to.emit(snapshotRevertTester, "ErrorOccurred");

            const results = await snapshotRevertTester.getAllTestResults();
            expect(results.errorHandling).to.be.true;
        });

        it("Should test gas optimization", async function () {
            const res = await snapshotRevertTester.runGasOptimization({ gasLimit: 1000000 });
            await res.wait();

            const results = await snapshotRevertTester.getAllTestResults();
            expect(results.gasOptimization).to.be.true;
        });

        it("Should test delegate call with transient storage", async function () {
            const res = await snapshotRevertTester.runDelegateCall({ gasLimit: 1000000 });
            await res.wait();

            const results = await snapshotRevertTester.getAllTestResults();
            expect(results.delegateCall).to.be.true;
        });

        it("Should test multiple snapshots", async function () {
            const res = await snapshotRevertTester.runMultipleSnapshots({ gasLimit: 1000000 });
            const receipt = await res.wait();
            await expect(receipt)
                .to.emit(snapshotRevertTester, "SnapshotCreated")
                .and.to.emit(snapshotRevertTester, "SnapshotReverted");

            const results = await snapshotRevertTester.getAllTestResults();
            expect(results.multipleSnapshots).to.be.true;
        });

        it("Should run all tests", async function () {
            const res = await snapshotRevertTester.runAllTests({ gasLimit: 2000000 });
            await res.wait();

            const results = await snapshotRevertTester.getAllTestResults();
            expect(results.nested).to.be.true;
            expect(results.snapshotRevert).to.be.true;
            expect(results.complexSnapshot).to.be.true;
            expect(results.errorHandling).to.be.true;
            expect(results.gasOptimization).to.be.true;
            expect(results.delegateCall).to.be.true;
            expect(results.multipleSnapshots).to.be.true;
        });
    });

    describe("Integration Tests", function () {
        it("Should test both contracts together", async function () {
            // Test TransientStorageTester
            const res1 = await transientStorageTester.runComprehensiveTest({ gasLimit: 2000000 });
            await res1.wait();
            const results1 = await transientStorageTester.getTestResults();
            expect(results1.basic).to.be.true;

            // Test SnapshotRevertTester
            const res2 = await snapshotRevertTester.runAllTests({ gasLimit: 2000000 });
            await res2.wait();
            const results2 = await snapshotRevertTester.getAllTestResults();
            expect(results2.nested).to.be.true;
        });

        it("Should test reset functionality", async function () {
            // Run tests first
            const res1 = await transientStorageTester.runComprehensiveTest({ gasLimit: 2000000 });
            await res1.wait();
            const res2 = await snapshotRevertTester.runAllTests({ gasLimit: 2000000 });
            await res2.wait();

            // Reset results
            const res3 = await transientStorageTester.resetTestResults({ gasLimit: 100000 });
            await res3.wait();
            const res4 = await snapshotRevertTester.resetTestResults({ gasLimit: 100000 });
            await res4.wait();

            // Verify reset
            const results1 = await transientStorageTester.getTestResults();
            const results2 = await snapshotRevertTester.getAllTestResults();

            expect(results1.basic).to.be.false;
            expect(results2.nested).to.be.false;
        });
    });
}); 