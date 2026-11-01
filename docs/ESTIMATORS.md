# Semantic Sketch Estimators

Status: **Approved at gate 0a (2026-11-02).** These formulas are implemented only in
[semantic/estimators.go](../semantic/estimators.go), as pure functions. Any change here needs an
explicit note in the phase report.

Every interval in this file is a **guaranteed error interval**: a deterministic bound that holds
whenever its preconditions hold. Because of the compression step, the precondition includes the
η event, so the intervals are **guaranteed conditional on the η event** (§1). None of them is a
confidence interval.

**Supersedes IMPLEMENTATION.md §4:** the sum interval (E1), the merge "fold into inherited mass"
rule (E2), and the separation condition (C2). See SPEC §11.

Notation, per counter (see SPEC §4):

| Symbol | Meaning |
|---|---|
| g, G | genuine count and genuine sum: observations confidently assigned since the counter was created, plus any fused in at merge, minus a re-anchored seed |
| I | inherited count bound, set to Ū when the counter is seeded; adds at fusion |
| f | floor contributions from merges (SPEC §7 step 4) |
| **I_eff** | **I + f**. Used everywhere an interval needs the unseen-mass bound. |
| a, b | declared value range, 0 ≤ a ≤ b |
| F, Ū | sketch count floor, and the bound on the count of any entity without a counter (SPEC §4) |
| Q, Q_s | ambiguous count and ambiguous sum in this counter's quarantine ledger |

## 1. Preconditions

All the guarantees below hold when **every** observation in the history of this sketch (including
every sketch merged into it) is valid, *and* the η event holds for every pair of codes compared,
*and*

```text
2ρ + 2η + band  ≤  τ  <  Δ − 2ρ − 2η − band        (so Δ > 4ρ + 4η + 2·band).
```

Call this condition **(V)**.

With 1-bit codes, the η event is probabilistic. Phase 0c measures how often it fails per
comparison, and charges any failure to V. Under violations the intervals are not guaranteed.
Bounding how far they can be wrong, as a function of V, is research question B (see the §8 block).

## 2. Identity lemma

**Lemma 1 (no observed ambiguity on valid streams).** Assume (V). Then:

- every observation is either confidently matched to the counter of its own entity, or creates a
  new counter because its entity has none;
- the quarantine ledger stays empty;
- no counter ever receives an observation from a different entity;
- every probation check promotes (SPEC §5.1).

*Argument.* Every stored code is the code of a valid observation r of some entity k, because codes
are frozen leaders. Let x be a valid observation of entity j.

- If k = j, then θ(x, r) ≤ 2ρ, so θ̂ ≤ 2ρ + 2η ≤ τ − band.
- If k ≠ j, then θ(x, r) ≥ Δ − 2ρ, so θ̂ ≥ Δ − 2ρ − 2η > τ + band.

So C₊ contains at most the counters of entity j. By induction, at most one counter exists per
entity: a new counter for j is created only when C₊ is empty, and that means j has no counter.
When j's counter exists, C₋ = C₊ = {it}.

Probation members of one counter are all observations of one entity, so each pair has
θ̂ ≤ 2ρ + 2η ≤ τ − band < τ. ∎

**Corollary (merge).** Under (V), on both inputs, the merge step-2 classification is confident
exactly for pairs of counters of the same entity, and "no match" otherwise. So merge never creates
alias links or fuses different entities. The sketch is then an exact-key Space-Saving summary over
latent entity indices. Writing this reduction up formally is listed as open in DESIGN.md.

### 2.3 Floor invariant

**Lemma 2.** Assume (V). For any entity j with no counter, its true count n_j ≤ Ū. For any entity
j with a counter, its true count satisfies g ≤ n_j ≤ g + I_eff.

*Argument (single stream).*

- **Lower bound.** g counts only observations of j (Lemma 1), so g ≤ n_j.
- **Upper bound.** Observations of j that arrived before the current counter was created were
  absorbed by earlier counters of j, and those counters were later evicted. When a counter is
  evicted, its count g' + I_eff' is the minimum over a full sketch, and the new counter takes
  I = Ū from before the eviction.
- **Induction.** At every moment, each entity without a counter has n_j ≤ Ū. Ū never decreases:
  the minimum count of a full Space-Saving sketch never decreases, and F only rises. When an entity
  is evicted, its mass so far is at most its counter's g + I_eff, which was the minimum, so it is
  ≤ Ū. Therefore the mass of j unseen by its current counter is ≤ the I that counter received.
- **Dissolution** (only possible under violation) raises F to at least g + I_eff of the dissolved
  counter, so the invariant still holds for the entities that lose their counter.

For merges, see §4. ∎

## 3. Count and sum intervals

```text
Count:  [ g ,  g + I_eff ]                Value = g + I_eff
Sum:    [ G ,  G + b·I_eff ]              Value = G + inhSum, clamped into the interval
```

**Why the brief's sum interval is not valid (E1).** IMPLEMENTATION.md §4 states the sum interval as
`[sum − inherited_sum, sum]`, where inherited_sum is the victim counter's sum at eviction. Its upper
end can be wrong. Victims are chosen by *count*, so the victim's sum says nothing about the evicted
*sum* of the entity that takes over the slot.

*Counterexample* (m = 2, [a, b] = [0, 1]). Each step shows (count, sum) for the counters, where
count = g + I and sum = G + inhSum.

| Step | Item (entity, value) | Action | Slot 1 | Slot 2 |
|---|---|---|---|---|
| 1 | X, 1 | new | X (1, 1) | — |
| 2–3 | Z, 0 ×2 | new, then match | X (1, 1) | Z (2, 0) |
| 4 | Y, 0 | evict X (min 1) | Y (2, 1) | Z (2, 0) |
| 5–6 | Y, 0 ×2 | match | Y (4, 1) | Z (2, 0) |
| 7 | W, 0 | evict Z (min 2) | Y (4, 1) | W (3, 0) |
| 8 | X, 1 | evict W (min 3) | Y (4, 1) | X (4, 1) |

At step 8, X's counter has G = 1 and inhSum = 0 (inherited from W). The brief's upper bound is
G + inhSum = 1, but X's true sum is 2. The X mass evicted at step 4 went into Y's slot, not W's.
The count bound is still correct (I = 3 ≥ 1). The corrected sum upper bound G + b·I = 4 contains
the truth. `semantic/estimators_test.go` replays this trace as a regression test.

The fix bounds the unseen sum by the unseen count times b. The count bound is guaranteed, and every
value is at most b.

The ambiguous mass Q, Q_s is reported next to these intervals and never folded into them. Under
(V) it is zero (Lemma 1).

## 4. Merge floor (E2)

**Why it is needed.** Suppose entity X has a counter in shard A but was evicted in shard B. The
merged counter must still bound X's B-side mass, which can be as large as Ū_B, even though B has
no counter for X. If nothing is added, the merged upper bound g + I undercounts.

**Rules** (SPEC §7):

- One-sided counters: f += Ū_other.
- Fused counters add g, G, I and f.
- After merge, F = Ū_A + Ū_B.
- If truncation drops counters, F = max(Ū_A + Ū_B, max over dropped counters of (g + I_eff)).
- Nothing is folded into any counter's inherited mass.

**Lemma 3 (sketch).** If Lemma 2 holds for A and for B, it holds for the merge.

- An entity on both sides: n = n_A + n_B ∈ [g_A + g_B, g_A + I_eff,A + g_B + I_eff,B].
- An entity on one side: its other-side mass is ≤ Ū_other, and that amount was added to f.
- An entity on neither side: n ≤ Ū_A + Ū_B ≤ F.
  The floor uses Ū and not F, because when an input is full, Ū_A + Ū_B can be larger than F_A + F_B.
- A dropped entity: its bound g + I_eff ≤ the new F. ∎

The error-size claim (I_eff ≤ εW after arbitrary merge trees) is **not** made here. That is the
merge theorem, and it goes in the §8 block.

## 5. Mean interval (E3)

Let n = g + y be the true count and S = G + x the true sum. The unseen part satisfies
y ∈ [0, I_eff] and x ∈ [a·y, b·y]. Then

```text
Lower = (G + a·I_eff) / (g + I_eff)
Upper = (G + b·I_eff) / (g + I_eff)
Value = G / g
```

*Derivation.* For fixed y, the mean ranges over [(G + a·y)/(g + y), (G + b·y)/(g + y)].
Take f(y) = (G + a·y)/(g + y). Its derivative has the sign of a·g − G = g·(a − G/g), which is ≤ 0
because every genuine value is ≥ a. So f is non-increasing in y, and its minimum is at y = I_eff.
The upper end works the same way, because G/g ≤ b. ∎

The interval is **tight**: an entity whose I_eff unseen observations all had value a (or all had
value b) reaches the endpoint.

Its width is (b − a)·I_eff / (g + I_eff). It is narrow when genuine mass dominates, and it becomes
[a, b] when g = 0. This is the "wide for small entities with large inherited mass" effect that
DESIGN.md says the results must show.

This is tighter than combining the count and sum intervals independently, because x and y are
coupled. It still uses only the count bound, the sum bound and [a, b].

Edge cases (each one is a unit test):

| Case | Interval | Value |
|---|---|---|
| g ≥ 1, I_eff = 0 | [G/g, G/g] | G/g |
| g ≥ 1, I_eff > 0 | as above | G/g |
| g = 0, I_eff > 0 (all inherited) | [a, b] | (a + b)/2, with `Empty = true` |
| g = 0, I_eff = 0 | [a, b] | (a + b)/2, with `Empty = true` |
| a = b | [a, a] | a |

Kahan rounding on G can push Value slightly outside [a, b]. Every result is clamped to [a, b].

## 6. Diff intervals

For entity e, with intervals [l_A, u_A] in A and [l_B, u_B] in B:

- Count and sum delta (A − B): [l_A − u_B, u_A − l_B].
- An entity in A only uses B's `Unmonitored()` interval for its B side, which is [0, Ū_B] for
  count and [0, b·Ū_B] for sum. Its B-side mean is [a, b].
- Mean delta: [Lower_A − Upper_B, Upper_A − Lower_B].
- `EntityDelta.Significant` is true when the count delta interval excludes 0.

## 7. Tests

These tests live in `semantic/`:

- The §3 counterexample, as a regression test for the sum upper bound.
- Exhaustive mean tests over a grid of g ∈ {0, 1, 2, 10}, I ∈ {0, 1, 5, 100}, with G at the
  extremes and in the middle, and (a, b) ∈ {(0, 1), (0, 0), (2, 5)}. Each test checks that the
  closed form equals a brute-force min/max over integer y and the x extremes.
- Property tests on valid synthetic streams (entities on the sphere, (V) satisfied by
  construction): every reported interval contains the exact per-entity truth, and no counter mixes
  entities. They run on a single stream and on three merge-tree shapes: sequential, balanced
  binary, and random.

## 8. Proof references (to be filled by the paper)

<!-- BEGIN PAPER PROOFS — do not edit by hand; replace with references to the paper's theorems. -->
- Theorem (identity under (V)): _TBD, extends Lemma 1._
- Theorem (merge reduction to mergeable Space-Saving, I_eff ≤ εW over arbitrary merge trees): _TBD._
- Theorem (violation stability, E ≤ εW + (C_geom + C_stab)·V): _TBD; research question B._
- Lower bound (Δ > 6ρ for leader seeding under worst-case outliers): _TBD._
<!-- END PAPER PROOFS -->
