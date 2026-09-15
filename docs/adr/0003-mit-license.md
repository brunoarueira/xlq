# 3. MIT license

## Status

Accepted

## Context

xlq needs a license before the first commit lands, so it's clear from
day one what people can do with the code. `jq` and `yq`, the two tools
xlq takes its shape from, are both permissively licensed (MIT and
similar), and there's no reason here - no patent concern, no dependency
whose license forces a particular choice - to reach for anything more
restrictive or more complex than a single permissive license.

## Decision

xlq is licensed under MIT alone (`LICENSE`, standard MIT text, copyright
Bruno Arueira). No dual-licensing, no CLA.

## Consequences

Maximally permissive and maximally simple: one file, one set of terms,
nothing for contributors or downstream users to reconcile. If a reason
to add a second license (e.g. Apache-2.0's explicit patent grant) shows
up later, that's a new ADR.
