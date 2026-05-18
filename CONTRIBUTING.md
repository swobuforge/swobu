# Contributing to Swobu

This document defines how to contribute changes that are reviewable, testable, and safe to merge.

## What Good Contributions Look Like

Preferred contributions:

- fix incorrect behavior
- improve compatibility across supported clients/backends
- improve error clarity and diagnostics
- improve setup/configuration clarity
- improve docs with concrete examples
- improve tests for real user-facing behavior
- improve performance with before/after evidence

## What To Avoid

Avoid broad rewrites without prior alignment:

- drive-by architecture rewrites
- broad refactors with no user-facing outcome
- dependency churn without a concrete problem statement
- speculative features disconnected from interoperability focus
- unrelated bundle changes in one pull request

If the change is large, open an issue first.

## Workflow

1. Open an issue for large or non-obvious changes.
2. Fork the repository.
3. Create a focused branch.
4. Implement one bounded change.
5. Run relevant checks.
6. Open a pull request.

Before opening a pull request, run:

```sh
make test
make build
```

If a check is not applicable, explain why in the pull request.

## Pull Request Standard

Each pull request should include:

- what changed
- why it changed
- what behavior is now different
- what tests prove it
- screenshots or recordings for UI changes
- migration notes, if applicable
- breaking changes, if any

Pull requests may be closed when they are too broad, stale, unsafe, unclear, or outside current roadmap focus.

## Contribution Scope Rule

Swobu is in beta.

Prefer improvements that strengthen current supported surfaces over speculative platform expansion.

Priority direction:

- compatibility correctness
- deterministic behavior
- clear failures
- local-first safety defaults
- practical operability

## CLA (Required)

By submitting a contribution, you agree to the terms in [`CLA.md`](./CLA.md).

Swobu must be able to maintain, sublicense, dual-license, and relicense contributions as the project evolves.

Why this exists:

- Swobu is AGPL-licensed in public.
- some teams need a commercial license path.
- the CLA preserves that option while keeping the public project open.
- contributors keep ownership of their contributions.

Only submit code, docs, tests, designs, or assets you have the legal right to contribute.

If you contribute on behalf of an employer, client, school, or other organization, ensure you are authorized to do so.

## AI-Assisted Contributions

AI-assisted contributions are allowed.

Before submitting AI-assisted changes, verify that:

- you reviewed and understood the change
- tests and docs match the actual behavior
- generated content does not copy incompatible licensed material

## Security Reports

Do not report vulnerabilities in public issues.

Report privately: `security@swobu.com`

Include enough detail to reproduce the issue.
Swobu will review valid reports and coordinate disclosure.
