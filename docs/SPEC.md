# Semantic Aggregation Sketch — Specification (model v0)

Status: **Approved at gate 0a (2026-11-02). Implemented in Phase 0b** (`quant/`, `semantic/`).

Interval formulas and their justification live in [ESTIMATORS.md](ESTIMATORS.md); this file defines
the model, the state, and the algorithm.

**This file supersedes IMPLEMENTATION.md §4** wherever they differ. Each superseded item is marked
**[SUPERSEDES]** and listed in §11, together with the decision that approved it.

## 1. Model v0

These assumptions are frozen for the prototype.

- **Latent centers.** There is an unknown set of unit vectors c_1, c_2, … (entities).
- **Radius.** Every valid observation x of entity j satisfies θ(x, c_j) ≤ ρ.
- **Separation.** Centers are at least Δ apart: θ(c_i, c_j) ≥ Δ for i ≠ j.
- **Compression.** Each compressed representative is within η of the observation it encodes (§3.3 makes this precise).
- **Values.** Each observation carries a value v ∈ [a, b] with 0 ≤ a ≤ b.
- **Violation.** Any observation outside the model is a violation. V is the total count (and, for sums, total value) of violating observations.

Distance is angular distance θ(x, y) = arccos(⟨x, y⟩ / (‖x‖‖y‖)) ∈ [0, π]. It is a metric on the
unit sphere, so the triangle inequality holds. All thresholds (ρ, Δ, η, τ, band) are in radians.

**Observed vs latent ambiguity.** Observed ambiguity means the stored state contains several
plausible representatives for x, and it can be detected. Latent ambiguity means several true
entities could explain x, and it generally cannot be detected, because the right entity may have no
counter. This spec never treats one as evidence of the other.

## 2. Parameters

All parameters are fixed at construction and stored in the serialized header.

| Param | Type | Meaning | Default | Constraint |
|---|---|---|---|---|
| `dim` | u32 | Input dimension d | required | ≥ 1 |
| `capacity` | u32 | Counters m (about 1/ε) | 4096 | ≥ 1 |
| `tau` | f64 | Assignment threshold τ (radians) | required | 0 < τ − band, τ + band < π |
| `band` | f64 | Half-width of the ambiguity band around τ | 0.05 | ≥ 0 |
| `bits` | u8 | Bits per (padded) dimension in representative codes | 1 | ∈ {1, 2, 4} |
| `value_min` (a) | f64 | Value lower bound | 0 | finite, ≥ 0 |
| `value_max` (b) | f64 | Value upper bound | 1 | finite, ≥ a |
| `exemplars` (R) | u16 | Exemplar sample size per entity | 8 | ≤ 1024 |
| `probation` (m) | u32 | Member codes collected before the promotion check (§5.1); 0 disables it | 3 | ≤ 64 |
| `refine` | bool | Representative refinement | false | **must be false in v0** |
| `seed` | u64 | Seed for the rotation, entity IDs, and exemplar priorities | required | — |

**Separation condition (V).** The identity lemma (ESTIMATORS.md §2) holds only when

```text
2ρ + 2η + band  ≤  τ  <  Δ − 2ρ − 2η − band,      which requires   Δ > 4ρ + 4η + 2·band.
```

This is the separation condition used everywhere. It replaces the brief's Δ > 4ρ + 4η. The library
cannot check it, because ρ, Δ and η are properties of the data. `tuner` will offer a helper that
computes τ from measured ρ, Δ and η (Phase 0c measures them).

## 3. Encoding pipeline

### 3.1 Input validation

`Add` rejects the observation, changes nothing, and increments `Stats.Rejected[reason]` when:

- the length is not `dim`;
- any component is NaN or ±Inf;
- the norm is 0;
- or the value is NaN or outside [a, b] (this includes negatives).

**[SUPERSEDES]** `Add` returns `error` (decision D1).

`AddBatch` is **all-or-nothing**. It validates and encodes every item first, and only then applies
them in index order. If any item fails, it returns a `*BatchError{Index, Err}` for the first failing
index. The sketch is then unchanged, except that `Stats.Rejected` counts that one item.

### 3.2 Seeded rotation

1. Normalise x to a unit vector, computing in float64.
2. Zero-pad to D = next power of two ≥ d.
3. Apply three rounds of a random-sign diagonal followed by a fast Walsh–Hadamard transform:
   y = (H/√D)·S₃·(H/√D)·S₂·(H/√D)·S₁·x. Each S_i is a ±1 diagonal drawn from `seed` via SplitMix64.

The transform is orthogonal, so it preserves angles exactly. It costs O(D log D) and stores no
matrix. It uses only additions, subtractions, sign flips and a scaling, so the result is
bit-identical across platforms (it has no FMA hazard). A dense random orthogonal matrix is rejected
because computing it needs a QR decomposition, and QR does not give bit-identical results across
platforms.

**Padding cost.** Codes are D·bits bits long, not d·bits:

| d | D | Overhead |
|---|---|---|
| 384 | 512 | +33% |
| 768 | 1024 | +33% |
| 1024 | 1024 | 0% |

### 3.3 Quantisation and the distance estimate

- `bits = 1`: code_i = [y_i ≥ 0], packed into ⌈D/64⌉ uint64 words. The distance estimate is
  θ̂(p, q) = π · popcount(p ⊕ q) / D.
- `bits = 2, 4`: uniform scalar quantisation of y_i over [−t, t] with t = 3/√D, clipped at the ends,
  into L = 2^bits levels. Level q is represented by the odd integer s = 2q + 1 − L. The estimate is
  θ̂ = arccos(⟨s_p, s_q⟩ / (‖s_p‖‖s_q‖)). The dot products are integers, so the estimate is deterministic.

**Definition of η.** v0 uses the symmetric estimate: both the incoming item and the stored
representatives are codes. Compression is valid for a pair when |θ̂(code_x, code_y) − θ(x, y)| ≤ 2η.

A pair whose estimate falls outside this is treated as a model violation, and its weight is charged
to V. With 1-bit codes this bound holds with high probability, not deterministically, so every
interval is **guaranteed conditional on the η event**.

Phase 0c reports the empirical η-failure rate per comparison, using this exact rotation, at
d ∈ {384, 768, 1024} and bits ∈ {1, 2, 4}. It also flags whether a union bound over N × capacity
comparisons looks loose at 1 bit.

## 4. State

The three categories live in separate structs and are never mixed.

```go
type aggState struct {   // Aggregation
    g        uint64      // genuine count: observations confidently assigned to this counter's entity
    G        kahan       // genuine sum
    inh      uint64      // I: inherited count bound at seeding (= Ū then)
    floorAdd uint64      // f: floor contributions from merges; I_eff = I + f
    inhSum   float64     // point-estimate contribution only; never used for bounds (ESTIMATORS §3)
}
type idState struct {    // Identity
    id          uint64
    code        quant.Code // frozen leader representative
    provisional bool
    confident   uint32     // confident matches since creation
    aliases     []uint64   // entity IDs this counter was ambiguous with at merge, or fused with
    members     []member   // probation member codes; nil once promoted
    seedItem    uint64     // seed item, so re-anchor can quarantine it
    seedValue   float64
}
type uncState struct {   // Uncertainty (quarantine ledger)
    count uint64
    sum   kahan
}
```

Sketch-level fields:

| Field | Meaning |
|---|---|
| `F` | Count floor: an upper bound on the count of any entity without a counter. 0 for a fresh sketch. Raised by merge (§7) and by dissolution (§5.1). |
| `W`, `WSum` | Total confidently aggregated count and value. |
| `Wq`, `WqSum` | Total quarantined mass, counted once per item. Reported next to W. |
| `Orphaned` | Quarantined count whose counter was evicted, dropped or dissolved. |
| `Rejected` | Counts of rejected inputs, by reason. |
| exemplars | Per counter: the R **distinct** item IDs with the smallest priority h(seed, itemID). Exemplars sample distinct IDs, not occurrences, so an item seen 100 times counts once. |

Define **Ū = max(F, min_i(g_i + I_eff_i))** if all m slots are in use, and **Ū = F** otherwise.
Ū is an upper bound on the count of any entity without a counter (ESTIMATORS §2.3).

## 5. Ingest (`Add`)

The steps are applied exactly in this order.

1. Validate (§3.1), then rotate and quantise (§3.2–3.3) into `c`.
2. Compute d_i = θ̂(c, code_i) for every live counter (linear scan). The search will move behind an
   `Index` interface when capacity exceeds 16384.
3. Let `C₋` = {i : d_i ≤ τ − band} and `C₊` = {i : d_i ≤ τ + band}.
4. **Confident match** when |C₋| = 1 and |C₊| = 1. The counter i is the single element:
   - set g_i += 1, G_i += v, confident_i += 1;
   - offer itemID to i's exemplars;
   - set W += 1, WSum += v;
   - if the counter is provisional, apply §5.1.
5. **Observed-ambiguous** when C₊ ≠ ∅ and step 4 did not apply:
   - take the two counters with the smallest d_i, breaking ties by lower id;
   - add (1, v) to *each* of their uncertainty ledgers;
   - add (1, v) once to W_q;
   - leave identity state and aggregation state untouched, and seed nothing.
   - **[SUPERSEDES]** Quarantined mass is excluded from W (decision D2).
6. **New entity** when C₊ = ∅. This is a Space-Saving insert:
   - Let Ū be computed before any eviction.
   - If a free slot exists, use it. Otherwise evict the victim: the counter with the smallest
     g + I_eff, breaking ties by smallest id. The victim's ledger count goes to `Orphaned`.
   - Set the new counter's aggregation state to g = 1, G = v, I = Ū, f = 0, and inhSum = the
     victim's G + inhSum (0 if no counter was evicted).
   - Reset identity state completely: code = c, id = h(seed, itemID, W), provisional =
     (probation > 0), confident = 1, no aliases, no members. Record the seed item and its value.
   - Reset the uncertainty ledger to zero, and reset the exemplars to {itemID}.
   - Set W += 1, WSum += v.

`AddBatch` is equivalent to calling `Add` in index order, except that it is all-or-nothing (§3.1).
It also takes the lock once.

**Refinement** (`refine = true`) is not implemented in v0, and the constructor rejects it.

### 5.1 Probation (m = `probation`)

- A newly seeded counter is **provisional**. Provisional counters match normally, and their entities
  are reported with `Provisional = true`.
- On each confident match, the item's code is stored as a **member** if its item ID differs from
  the seed's and from every stored member, and fewer than m members are stored.
- When m members are stored, run the **promotion check**. Here "consistent" means θ̂ ≤ τ.
  - All member pairs consistent, and every member consistent with the representative →
    **promote**. The representative stays as it is.
  - Members consistent with each other, but some member inconsistent with the representative →
    **re-anchor**:
    - the representative becomes the first member's code;
    - the seed item's (1, value) moves from aggregation state (g, G, W) to this counter's
      quarantine ledger and W_q;
    - the seed item is removed from the exemplars;
    - the counter is promoted.
  - Members not consistent with each other → **dissolve**:
    - raise F to max(F, Ū, g + I_eff) (see the note below);
    - move g and G out of W into W_q, and add g and the ledger count to `Orphaned`;
    - free the slot.
- Member codes are freed on promotion or dissolution.

**Re-anchor cannot happen during ingest.** Every member matched the representative at θ̂ ≤ τ − band
using the same deterministic estimate, so the member-vs-representative check always passes. The
re-anchor branch is reachable only after a merge fuses provisional counters (§7), because then the
members can come from the other shard's seed.

**Dissolve raises the floor.** The dissolved counter's entities lose their counter, so their counts
must stay bounded by Ū. Raising F to g + I_eff keeps Lemma 2 true.

**Valid streams.** Under (V), the promotion check always promotes: every pair of members are
observations of the same entity, so θ̂ ≤ 2ρ + 2η ≤ τ − band. Re-anchor and dissolve happen only
under violation.

## 6. Queries

| Call | Returns |
|---|---|
| `TopK(k, by)` | Entities sorted by `by` ∈ {ByCount: Count.Upper, BySum: Sum.Upper, ByMean: Mean.Value}, descending. Ties go to the lower id. k ≤ 0 returns all. |
| `Lookup(vec)` | The counter that `Add` would confidently match. Returns `false` if the match would be ambiguous or new. Never mutates state. |
| `Unmonitored()` | Count interval [0, Ū] and sum interval [0, b·Ū], which hold for any entity that has no counter. |
| `Stats()` | W, WSum, Wq, WqSum, Orphaned, F, Ū, capacity, live counters, rejected counts, and the probation and merge counters. |
| `Diff(other)` | Matches counters with the merge rule (§7 step 2). See ESTIMATORS §6 for the interval arithmetic. Entities found in only one sketch are differenced against the other sketch's `Unmonitored()`. |

Entity fields:

- `Count`, `Sum` and `Mean` come from ESTIMATORS.md.
- `Ambiguous` and `AmbiguousSum` are the counter's quarantine ledger.
- `Exemplars` are sorted by priority.
- `Provisional` and `Aliases` are added so callers can see identity uncertainty.

## 7. Merge (`A.Merge(B)`, mutates A)

1. **Reject** with `ErrIncompatible` if any parameter differs. **[SUPERSEDES]** The brief lists
   only dim, bits, seed, tau and band. The rest must match too, or the result's semantics are
   undefined. Also reject `A.Merge(A)`.
2. **Match**. Iterate B's counters in ascending id order. Compare each one against A's counters as
   they were before the merge, using the ingest classification from §5 steps 3–5 with B's code as
   the query.
   - Confident: fuse into that A counter.
   - Ambiguous: keep it as a separate counter, and add alias links both ways with the top two hits.
     Count it in `Stats.Aliased`.
   - No match: union, kept as a separate counter.
3. **Fuse** each matched pair: g, G, I, f, inhSum and the uncertainty ledgers all add. The
   representative stays A's code.
   - The fused counter keeps the smaller id, and the other id becomes an alias.
   - Exemplars merge to the R smallest priorities of the union of distinct IDs. This is exact and
     deterministic. **[SUPERSEDES]** The brief says weighted reservoir sampling (decision D3).
   - If both counters are provisional, the members are A's followed by B's new item IDs, capped at
     m. If either counter is promoted, the fused counter is promoted and its members are freed.
   - Two B counters that confidently match the same A counter are both fused into it. On a valid
     stream this cannot happen; counting it is part of the merge-fusion attack.
4. **One-sided counters** absorb the other side's unseen mass as a **floor contribution**: an
   A-only counter gets f += Ū_B, and a B-only counter gets f += Ū_A. The intervals use
   I_eff = I + f (ESTIMATORS §4).
5. **Truncate** if more than m counters remain. Sort by (g + I_eff) descending, breaking ties by id
   ascending. Keep the first m, and set F = max(Ū_A + Ū_B, max over dropped counters of
   (g + I_eff)). Without truncation, F = Ū_A + Ū_B. Ū is taken from each input before the merge.
   Dropped counters' ledger counts go to `Orphaned`.
   **[SUPERSEDES]** The brief says to fold the dropped remainder into inherited mass. Instead,
   dropped counters raise the sketch-wide count floor F, and nothing is folded into inherited mass
   (decision E2).
6. The totals (W, WSum, Wq, WqSum, Orphaned, Rejected) add.
7. Run the deferred promotion checks (§5.1) on fused provisional counters that hold m members, in
   id order.

Merge is deterministic given the two inputs. Whether merge is commutative and associative in its
*answers* is not claimed. Whether its *bounds* survive arbitrary merge trees is the merge theorem in
DESIGN.md; Phase 0b tests it on three tree shapes.

## 8. Concurrency

Every public method takes the sketch's own `sync.Mutex`. Reads are not split out into an RWMutex,
because `Lookup` is cheap and a second lock mode adds risk. `AddBatch` takes the lock once.
`Merge` and `Diff` lock both sketches in ascending order of a per-instance sequence number, so
they cannot deadlock. `Lookup` and `TopK` return copies.

## 9. Determinism rules

- No output or tie-break depends on Go map iteration order or on pointer addresses.
- Every random choice comes from `seed`, through SplitMix64 or h(seed, ·). There is no `math/rand`
  global and no clock.
- Float products that feed into stored state are wrapped as `float64(x*y)` to stop the compiler
  fusing them into FMA (arm64).
- Sums use Kahan compensation in a fixed order.
- Phase 1 adds a golden-byte test across linux/amd64, linux/arm64 and windows/amd64.

## 10. Complexity

Per `Add`: O(D log D) to rotate, plus O(m · D · bits / 64) for the popcount scan.

Memory: m · (D·bits/8 + 64 bytes + 16R bytes) + O(1). At m = 4096, D = 1024, bits = 1, R = 8 this is
about 4096 · (128 + 64 + 128) ≈ 1.3 MB.

## 11. Decisions at gate 0a, and what they supersede

| # | Decision | Supersedes in IMPLEMENTATION.md |
|---|---|---|
| E1 | Sum interval [G, G + b·I_eff] | §4 "Count and sum: [value − inherited, value]" (for sum) |
| E2 | Sketch-wide count floor F; per-counter floor contributions f; I_eff = I + f; dropped counters raise F | §4 Merge step 3 "fold the remainder into inherited mass" |
| E3 | Mean = [(G + a·I_eff)/(g + I_eff), (G + b·I_eff)/(g + I_eff)] | §4 mean bullet (refines it) |
| C1 | Intervals are "guaranteed conditional on the η event"; 0c reports the η-failure rate | §4 (clarifies it) |
| C2 | Separation condition Δ > 4ρ + 4η + 2·band | DESIGN.md's Δ > 4ρ + 4η row, for the banded algorithm |
| D1 | `Add` returns error; `AddBatch` is all-or-nothing with `*BatchError` | §2 `Add`/`AddBatch` signatures |
| D2 | Quarantined mass is outside W, reported as W_q | — (unspecified) |
| D3 | Exemplars are the bottom-R by seeded hash over distinct item IDs | §4 Merge step 4 "weighted reservoir sampling" |
| D4 | Probation rule (§5.1), default m = 3 | §4 params table: probation default 0 |
| D5 | Seeded randomized Hadamard (3 rounds), padded to a power of two | — (§4 said only "rotate (seeded)") |
| D6 | `semantic/` and `quant/` at the current root in 0b; `core/` move and FORMAT.md in Phase 1 | §1 layout (deferred, not dropped) |
| — | Merge also requires value range, capacity, exemplars, probation and refine to match | §4 Merge step 1 (list extended) |
| — | `Entity` gains `Provisional`, `Aliases` and `AmbiguousSum`; `Estimate` gains `Empty` | §2 struct fields (additive) |
