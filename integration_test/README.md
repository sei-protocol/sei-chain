# Integration Test Framework
This page provides an overview of how to use the testing framework
to quickly add new integration test cases.

## Getting Started
These instructions will help you set up the integration test framework
on your local machine for development and testing purposes.

### Prerequisites
- An up-to-date Python 3.x
- Pyyaml install (pip3 install pyyaml)
- Docker and docker compose installed and running

### Usage
1. Ensure docker containers are up and running: `make docker-cluster-start`
2. Execute the tests with this command: `python3 integration_test/scripts/runner.py test.yaml`

## Writing Tests
Each integration test is defined in a YAML file under its specific module folder under the integration_test directory

There's a template yaml file which you can copy from to start with: [template](https://github.com/sei-protocol/sei-chain/tree/main/integration_test/template/template_test.yaml)

A typical yaml test case would look like this:
```yaml
- name: <Replace with test description>
  inputs:
    # Add comments for what this command is doing
    - cmd: <Replace with bash command>
      env: <Add if you want to store the output as an env variable>
      node: <Optional, default is sei-node-0>
    # Add comments for what this command is doing
    - cmd: <Replace with bash command>
      env: RESULT
  verifiers:
    # Add comments for what should the expected result
    - type: eval
      expr: <Replace with a valid python eval>
    - type: regex
      result: RESULT
      expr: <Replace with regular expression>
```

One simple example for verify chain is started and running fine:
```yaml
- name: Test number of validators should be equal to 4
  inputs:
    # Query num of validators
    - cmd: seid q tendermint-validator-set |grep address |wc -l
      env: RESULT
  verifiers:
  - type: eval
    expr: RESULT == 4
```

### Explanation

| field_name | required | description                                                                                                                                   |
|------------|----------|-----------------------------------------------------------------------------------------------------------------------------------------------|
| name       | Yes      | Defines the purpose of the test case .                                                                                                        |
| inputs     | Yes      | Contains a list of command inputs to run one by one.                                                                                          |
| cmd        | Yes      | Exact seid or bash command to run.                                                                                                            |
| env        | No       | If given, the command output will be persisted to this env variable, which can be referenced by all below commands                            |
| node       | No       | If given, the command will be executed on a specific container, default to sei-node-0                                                         |
| verifiers  | Yes      | Contains a list of verify functions to check correctness                                                                                      |
| type       | Yes      | Currently support either `eval` or `regex`.                                                                                                   |
| result     | Yes      | Pick any env variables you want to pass in for regex match                                                                                    |
| expr       | Yes      | If type is eval, then the format is `[env] > \| == \| != \| >= \| > \| <= \| < [number]` <br/> If type is regex, then provide a valid regular expression. |                                                         |

### Notes & Tips
There are some tricks and tips you should know when adding a new test case:
1. Try to avoid using sing quote `'` in your command as much as possible, use `"` to replace whenever possible
2. Sometimes you need to escape `"` and make it `\"`
3. Use jq expressions to simplify the output and make your verification logic easier
4. Commands will be executed one by one and will be wrapped within `docker exec -ti`
5. Chain is keep running and is stateful, so some tests might not be idempotent which is fine
6. You can define more than one verifier and each one check a different env