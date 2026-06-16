# Security Policy

## Reporting Security Vulnerabilities

Security researchers are essential in identifying vulnerabilities that may impact the Sei ecosystem. If you believe you have discovered a security vulnerability in Sei Chain or another asset covered by Sei's bug bounty program, submit your report through the Sei Immunefi program:

- [Sei Bug Bounty on Immunefi](https://immunefi.com/bug-bounty/sei/information/)

Please do not report security vulnerabilities through public channels, including GitHub issues, pull requests, public chats, or social media. Reports submitted outside the Immunefi program will not be eligible for a bounty.

The Immunefi program page is the authoritative source for:

- Assets and impacts in scope
- Reward amounts and eligibility requirements
- Severity classification
- Proof-of-concept requirements
- Disclosure rules and prohibited activities
- KYC and payout requirements

## Responsible Vulnerability Testing

All vulnerability research must follow the rules and scope published in the Sei Immunefi program. In particular:

- Do not test vulnerabilities on Sei mainnet `pacific-1`, public testnets, public frontends, or other publicly accessible Sei environments. The Sei Immunefi program requires vulnerability testing to be performed in permitted local environments.
- Use local forks, local testnets, or other permitted local environments for proof-of-concept development.
- Do not attempt phishing, social engineering, denial-of-service attacks, or automated testing that generates significant traffic.
- Do not access, modify, delete, exfiltrate, or degrade data that does not belong to you.
- Do not publicly disclose any vulnerability or vulnerability report details unless and until disclosure has been explicitly approved under the Immunefi program rules.

## What to Include in a Report

Submit reports through Immunefi with enough detail for the issue to be reproduced and assessed. Include:

- Type of vulnerability
- Affected component, asset, version, commit, or configuration
- Description of the vulnerability
- Steps to reproduce the issue
- Proof of concept, when required by the Immunefi program
- Impact of the issue
- Explanation of how an attacker could exploit it

## Vulnerability Disclosure Process

1. **Submission**: Submit the vulnerability through the Sei Immunefi program.
2. **Triage**: Immunefi and the Sei security team will triage the report according to the program rules.
3. **Assessment**: The issue will be evaluated for validity, scope, severity, impact, and reward eligibility.
4. **Resolution**: Once fixed, you may be contacted to verify the solution.
5. **Disclosure**: Public disclosure must follow the Immunefi program's responsible publication rules.

During the vulnerability disclosure process, keep vulnerabilities and communications around vulnerability submissions private and confidential unless and until disclosure is explicitly approved. If a security issue requires a network upgrade, additional time may be needed to raise a governance proposal and complete the upgrade.

During this time:

- Avoid exploiting any vulnerabilities you discover.
- Demonstrate good faith by not disrupting or degrading Sei's services.

## Feature Requests

For a feature request, e.g. module inclusion, please make a GitHub issue. Clearly state your use case and what value it will bring to other users or developers on Sei.

## Feedback on this Policy

For non-sensitive recommendations on how to improve this policy, submit a pull request. Do not include vulnerability details in public pull requests.
