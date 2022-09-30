# RFC 002: Wasm Contract Parallelization

## Changelog

- 2022-09-30: Initial Draft

## Abstract

This document is meant as an extension to RFC-001: Parallel Tx Message Processing. This RFC discusses how we can extend the tx parallelization framework to wasm contract execution.

## Background

As discussed in the previous RFC-001, we can prallelize transactions by identifying resource dependencies with variable granularity and building a dependency DAG that allows us to safely process independent transactions concurrently while also maintaining proper ordering for dependent transactions in a block. Wasm contracts are a bit more difficult because they are inherently more opaque in functionality compared to predefined message handlers for chain level messages. As a result, it is more difficult to identify the dependencies of Wasm contracts and parallelize them in the same way as sdk messages.

## Discussion

### Parallelization Approach
One approach we can take is a gradual parallelization of a wasm contract. When the wasm contract is first uplaoded, we can have it behave as if it uses all resources, essentially making its execution sequential. After the contract has been uploaded (or even as part of upload proposal), we can then allow for a dependency mapping for the contract parallelization which would map between the different execute and query messages for the contract to the resource dependencies that the contract would require during the execution of that execute / query. These dependency mappings can be associated with a contract code ID so that the parallelization can be consistently applied to multiple instances of that contract.

> OPEN QUESTION: If we require additional granularity, should we allow mapping specific to the contract instance (this would be relevant for other contracts calling a specific contract (eg. code ID 1), if there are multiple instances of thcontracts with code ID 1, we could allow them to run in parallel since the other calling contract can specify exactly what resources would be affected that are specific to the contract instance that it would be calling).

This would follow a similar pattern for the message dependency mapping for sdk messages, but instead would be blocked prior to contract execution and would release the resources at the end of the contract execution. The reason for this is because it would be much more effort to construct a system to granularly release resources within contract execution.

One key difference would be that we would also store an enabled flag for the parallelization mapping, and if the wasm contract fails validation, we would disable the parallelization mappings and only process that contract sequentially until the parallellization mappings are updated.


### Failure Remediation
However, we do need some remediation steps for wasm contract execution as well in the case of improper resource access that hasn't been defined in the dependency mappings. One way we can accomplish this is by modifying the wasm module to perform ongoing resource validation during a contract's execution. Because wasm contract need to go through the wasm module for any queries or messages during contract execution, we can either fork the wasm module or override the message handler and querier plugins so that we can have greater control over all of the queries / messages that are executed by the wasm contract. As we process these messages / queries, we can validate them against the wasm contract mappings, and if we detect a resource access that has not been defined in the contract parallelization, we can then fail the contract's transaction, and disable the parallel processing via the enabled flag for that contract.

We need to fail the current transaction so that it doesn't improperly modify any resources that haven't been properly defined in the contract mapping. Additionally, we would also need to fail any other transactions with that contract in the current block, since otherwise we would need to rebuild the dag with the sequential processing for the contract and restart the block's execution. Then, in the following blocks, those contract transactions would be able to process sequentially as expected.

> OPEN QUESTION: What are the potential side effects of failing contract transactions this way?
>
> OPEN QUESTION: What about the specific transactions that failed? Is it ok to just leave them as failed and assume that the user can resend them later? Or do we need to some other handling to ensure that we process those TXs sequentially as well (This would negatively impact block time)?

### Contract Migration
Because Wasm contract can be migrated in the future, we need to make sure that a contract migration wouldn't cause problems to the parallelization. This should be inherently handled by the dependency mappnig model discussed earlier, where each mapping is associated with a code ID. As a result, wheen migration to a new contract code (aka new code ID), it would initially start off being processed sequentially again (or parallel if we allow defining mappings within the code upload proposal). Then at a later time, we can once again parallelize the new contract code by introducing the dependency mappings for the new code ID. This does induce more operational overhead to the migration process, but that process is no intended to be frequent, so this additional overhead seems acceptable.