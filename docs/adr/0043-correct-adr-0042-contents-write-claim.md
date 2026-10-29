# 43. Correct ADR-0042's `contents: write` removal claim

## Status

Accepted

## Context

ADR-0042 said removing `contents: write` from `binary-provenance`'s
permissions was correct, "since nothing in it touches the release
anymore." Two more real release attempts (the `VERSION` bump this ADR
ships alongside) proved that wrong, the same way ADR-0028 once
corrected a wrong claim in ADR-0016 without editing it:

- `contents: read` alone (what ADR-0042 actually shipped) made GitHub
  reject the workflow file outright: `Error calling workflow
  '...generator_generic_slsa3.yml@v2.1.0'. The nested job
  'upload-assets' is requesting 'contents: write', but is only allowed
  'contents: read'.`

The generator's own `upload-assets` job (fetched and read directly
from `generator_generic_slsa3.yml@v2.1.0`'s source, not assumed)
declares `contents: write` and is gated on `if: inputs.upload-assets &&
(...)` - a condition this call never satisfies, since `upload-assets`
is never passed as an input here. ADR-0042's reasoning assumed a job
that never runs has no bearing on the permissions a caller must grant.
That's wrong: GitHub's static permission check for a reusable-workflow
call evaluates the **union of every nested job's own declared
permissions**, independent of each job's own `if` condition - a
structural property of the called workflow as a whole, checked at
parse time, before any `if` is ever evaluated.

## Decision

`binary-provenance`'s permissions grant `contents: write`, not `read`.
Confirmed directly, twice, not assumed from either single attempt:
`read` alone still failed with the error above, and restoring `write`
is what let the workflow file parse. Granting it does not mean
`upload-assets` executes - its own `if` condition is unaffected and
still never satisfied here - it's purely a static precondition of
calling `generator_generic_slsa3.yml` at all, not a permission this
job's own steps use for anything.

## Consequences

Read ADR-0042's "`contents: write` is also removed from this job's own
permissions, since nothing in it touches the release anymore" as
superseded by this ADR - ADR-0042 itself is left unedited, per
`CONTRIBUTING.md`'s own convention. The rest of ADR-0042's decisions
(bypassing the generator's upload path, downloading `provenance-name`
as a workflow artifact, attaching it explicitly in `publish-release`)
stand unchanged.
