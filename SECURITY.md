# Security Policy

## Supported versions

Only the latest minor release line receives security fixes.

| Version | Supported |
|---------|-----------|
| Latest  | ✅        |
| Older   | ❌        |

## Reporting a vulnerability

**Please do not file a public issue.**

Email **security@tesserix.dev** with:

- A description of the issue and its impact.
- Steps to reproduce (commands, CLI output, config snippets — redact secrets).
- Affected version (`cloudnav version`).
- Any suggested mitigation.

You'll get an acknowledgement within 3 business days. We aim to release a fix
and publish a GitHub Security Advisory within 30 days of confirming the report.

## Scope

cloudnav wraps official cloud CLIs (`az`, `gcloud`, `aws`). Vulnerabilities in
those tools should be reported to their respective vendors. In-scope for
cloudnav:

- Credential leakage via logs, cache, or error output.
- Command injection in the CLI wrapper layer (`internal/cli`).
- IAM provisioning paths that grant excess privilege.
- Supply-chain concerns in our release pipeline.
