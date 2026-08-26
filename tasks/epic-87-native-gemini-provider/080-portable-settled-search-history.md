# Portable Settled Search History

**Status:** In Progress

## Description

A Responses client can resume a canonical session whose earlier turn completed
a provider-owned web-search effect without Gemini `InteractionsReplay`. The
default Gemini target currently rejects that settled portable history before
dispatch, so a personal workspace with only that target returns
`NO_COMPATIBLE_TARGET` even though the current turn is ordinary text.

## Governing Docs

- `swobucli/opencore/internal/adapters/outbound/providers/gemini/doc.go`
- `docs/04-design/public-interface-contracts/http-routing-success-streaming-and-errors.md`
- `docs/03-architecture/system-shape-and-request-flow/monotonic-routing-boundary-and-attempt-semantics.md`
- `docs/05-engineering/testing-and-quality-gates/domain-contract-integration-and-end-to-end-tests.md`

## Applicable Skills

- `swobu-execute-loop`
- `swobu-request-path`
- `swobu-provider-adapter`

## Owned Behavior

- Owning bounded context: native Gemini provider request translation.
- Primary fault plane: target-local portable history projection.
- Target package: `internal/adapters/outbound/providers/gemini`.
- A completed web-search call/result pair lacking exact Interactions replay is
  omitted atomically from only the outbound Gemini request.
- An unresolved web-search call remains a typed incompatible-target failure.
- Exact Interactions replay remains exact and unchanged.

## Proof

- Codec counterfactual: settled portable Search history plus a current user turn
  encodes and records one occurrence-addressed omission; removing the result
  recreates incompatibility.
- Exchange/request-path regression: a Responses resume through a sole default
  Gemini target reaches provider preparation instead of
  `NO_COMPATIBLE_TARGET`.
- Narrow provider and exchange packages pass, followed by the request-path lane.

## Non-Goals

- No routing, session, checkpoint, target-support, or model-family changes.
- No fabrication of Google Search replay, summaries, citations, or sources.
- No support for unsettled or malformed foreign tool lifecycles.

## Continuation State

Reproduce the settled-history rejection at the Gemini codec, implement the
smallest occurrence-atomic request projection, then prove default-route
provider preparation and reconcile the epic status.
