# Cardinex: Semantic GROUP BY Sketches for AI Data

Nov 2, 2026 · Dip Chakraborty

## What is Cardinex

Cardinex is a Go library for **semantic GROUP BY over embedding streams**: per-entity count, sum, and mean when the same entity arrives as different but nearby vectors. It runs in bounded memory, in one pass, mergeably across workers, with guaranteed error intervals on every answer.

An entity is defined by a latent center: every valid observation lies within angular distance ρ of its entity's center, and centers are at least Δ apart. This makes identity an equivalence relation on valid streams. Observations outside the model are violations, and the research question is how much damage they can do.

The sketch ingests `(embedding, value, item_id)` triples. Values must be nonnegative and bounded in a declared range [a, b]: 1 for counting, training loss, eval correctness, reward, quality score, token count. Per entity it reports:

- **count** and **sum**, each with a guaranteed error interval (not a statistical confidence interval)
- **mean**, with a guaranteed interval derived from the count and sum bounds and [a, b]
- **top-k** by count, sum, or mean, plus ambiguous mass and exemplar item IDs

Cardinex supports two estimands. **Plan A** aggregates over latent semantic entities and is the research target; it is expected to apply at near-duplicate granularity. **Plan B** aggregates over cells of a seeded hierarchical quantizer, is deterministic and trivially mergeable, and serves topic-level composition analytics as a product feature.

Scope of v1:

| Component | Role |
| --- | --- |
| Go core library | Sketch, merge, serialize, query; zero dependencies |
| Python wheels | Go core built with `-buildmode=c-shared`; the interface ML engineers actually touch |
| HTTP / MCP service | Hosted sketches for pipelines and agents |
| Classical sketches (HLL, CMS, Bloom) | Retained from the original Cardinex as exact-key building blocks, not the research contribution |

Embeddings are computed outside Cardinex. It consumes vectors; it does not run models.

## Why Cardinex

AI data work is increasingly done in embedding space, but the tools for aggregating over that space are offline, unbounded, or biased. Training, analytics, and evaluation all keep asking per-cluster questions:

- **Training:** which semantic regions of the corpus still have high excess loss? How redundant is each region? Which modes dominate a synthetic-data generator?
- **Analytics:** what is in this 2T-token dump by latent topic and mass? What changed between dataset v1 and v2?
- **Evaluation:** where does the model fail, by latent slice? How many genuinely distinct items does a benchmark contain? Which eval items sit near heavy training-data clusters?

The three current workflows each break somewhere:

| Workflow | How it works | Where it breaks |
| --- | --- | --- |
| Offline k-means (CLIMB, SemDeDup style) | Embed everything, store all vectors, cluster, join back, aggregate | Global pass over all vectors; must rerun when data changes; not mergeable across workers; no error bounds |
| LSH + classical sketch | Hash embeddings with SimHash/MinHash, feed codes into HLL or Count-Min | Biased: LSH does not collide all near-duplicates, so multiplicity is entangled with identity (shown formally by MaxSketch) |
| Sample + k-means | Uniform sample, cluster, compute per-cluster stats | For additive-εW counts and sums, needs O(log(1/δ)/ε²) samples vs O(1/ε) Space-Saving counters; probabilistic only. For means of heavy, low-variance entities, sampling can win |

Cardinex replaces the clustering job with an inline sketch: each worker builds one as embeddings stream through, sketches merge at reduce time, new data adds incrementally, and every answer carries an error bound you can act on.

The regime matters. If all vectors fit in memory, the data is static, and a global pass is acceptable, run FAISS k-means and skip Cardinex. Its value starts where data is distributed, continuously updated, or too large to hold, which depends on dimension, precision, hardware, and update frequency rather than item count alone.

## Background study

The closest work counts distinct semantic objects or clusters offline; none aggregates weighted values per semantic entity in a mergeable stream with error bounds.

| Work | Venue / year | What it does | What it leaves open |
| --- | --- | --- | --- |
| HyperLogLog, Count-Min, Space-Saving | Classical | Distinct count, frequency, heavy hitters over exact keys | Exact key equality required; near-duplicates count as different items |
| Mergeable summaries (Agarwal et al.) | PODS 2012 | Proves Space-Saving / Misra-Gries summaries merge with preserved error | Exact keys only |
| Unbiased Space-Saving (Ting) | SIGMOD 2018 | Unbiased subset-sum estimates with variance from a Space-Saving sketch | Exact keys; mean intervals derivable only via ratio estimators |
| [Robust distinct elements](https://static.aminer.org/pdf/20170130/pdfs/sigmod/o3yuvkmdunrqka71vjh9izxhqc086zwe.pdf) (Chen, Zhang) | SIGMOD 2016 | Distinct count where items within distance α are one entity, low-dim Euclidean | No frequency or weighted aggregates |
| [Distinct sampling with near-duplicates](https://homes.luddy.indiana.edu/qzhangcs/papers/pods18-robustL0.pdf) (Chen, Zhang) | PODS 2018 | Uniform sampling over entities, sliding windows | Sampling, not aggregation |
| Robust statistics in general metrics (Zhang) | PODS 2025 | Õ(√n/ε) space, Ω(√n) lower bound in general metric spaces | Worst-case regime; scope beyond F0 must be checked (open item) |
| [MaxSketch](https://arxiv.org/abs/2605.15571) (Tsikouras, Caramanis, Tzamos) | NeurIPS 2026 oral | Max-linear Gaussian-projection sketch; distinct latent objects in Õ(log n/ε²) memory under a separation model | Max aggregation discards multiplicity by design: no frequency, sums, or means |
| [Distance-sensitive Bloom filters](https://arxiv.org/pdf/1607.05451) (Goswami et al.; Kirsch, Mitzenmacher) | SODA 2017; 2006 | "Is x near some element of S?" with space bounds | Membership only; static sets; hard to configure |
| [vCache](https://arxiv.org/html/2502.03771v5) | ICLR 2026 | Semantic prompt cache with user-defined error-rate guarantees via per-entry learned thresholds | Caching, not aggregation |
| [LSHBloom](https://arxiv.org/pdf/2411.04257v4) | PVLDB 2025 | MinHash-LSH + Bloom for internet-scale text dedup | Binary dedup; no per-entity statistics |
| [CLIMB](https://arxiv.org/html/2504.13161v1) (NVIDIA) | arXiv 2025 | Embeds and k-means-clusters pretraining data (~1000 clusters, merged to ~20), searches mixture weights with proxy models; beats DoReMi and RegMix | Offline, global clustering; static mixture; no error bounds |
| Learned sketches (Hsu et al.; [LSS](https://arxiv.org/pdf/2406.16270)) | ICLR 2019; 2024 | ML predictors improve frequency sketches | Exact keys; orthogonal to near-duplicate identity |

Two results shape the design. MaxSketch shows LSH-then-classical-sketch is biased, so Cardinex cannot use hashing for identity. CLIMB shows semantic clusters are useful for data mixing, but its clustering is an offline batch job; Cardinex makes that step streaming, mergeable, and bounded.

Plan B has its own close relative: hierarchical heavy hitters (Cormode, Korn, Muthukrishnan, Srivastava; randomized HHH, Ben Basat et al., SIGCOMM 2017). Prefix codes from a hierarchical quantizer plus per-prefix mergeable counters is essentially HHH over quantizer codes, which is why Plan B is positioned as a product feature rather than a research contribution.

## Research novelty and contributions

**Research question:** when can approximate semantic identity be composed safely with bounded-memory, mergeable streaming aggregation, and how much damage does identity failure cause?

**Target result:** a compressed streaming aggregation sketch whose weighted error stays controlled under arbitrary merge trees, representative uncertainty, and a bounded amount of identity failure:

```latex
E \le \varepsilon W + (C_{\mathrm{geom}} + C_{\mathrm{stab}})\,V
```

where W is total weight, V is the weight of violating observations, and the constants are independent of stream length and merge topology. If this is impossible under the model, the contribution becomes the impossibility result plus the weakest assumptions that make it achievable. The identity lemma alone is elementary; the contribution is the end-to-end guarantee and its stability under violation.

No "first" claims until the literature checks below are done.

### Model v0 (frozen for the prototype)

- Latent centers c_j. Valid observations lie within angular distance ρ of their center; centers are at least Δ apart.
- Values are nonnegative and bounded in [a, b].
- Each compressed representative is within η of the observation it encodes.
- A violation is any observation outside the model; V is their total weight.
- Observed ambiguity (several stored representatives plausible) is detectable. Latent ambiguity (several true entities could explain x) is generally not identifiable from bounded-memory state, because an entity may have no counter. The theorem must not substitute one for the other.

### Separation conditions

| Condition | What it guarantees | Notes |
| --- | --- | --- |
| Δ > 4ρ + 4η | One global threshold separates all same-entity from cross-entity representative pairs | Binding for merge with frozen leader representatives |
| Δ > 4ρ + 2η (approx.) | Item-to-representative assignment with an asymmetric estimator (RaBitQ-style) | Ingestion with frozen leaders |
| Δ > 6ρ, with τ ≥ 2ρ | No single seed can capture observations from two entities | Worst-case outlier seeding; candidate lower bound for leader-style seeding |
| Δ > 2ρ + 2e(n) + 2η | Ingestion with refined representatives, e(n) = representative estimation error after n genuine observations | Stochastic model only; e(1) ≈ ρ, so young counters still need about 4ρ |

Outlier seeds near a legitimate center do not cause false merges; they cause false splits, which no value of Δ prevents. Coordinated outliers near a false center form an entity under the model; they only cause harm through violations, which the bound charges to V.

### Algorithm v0

- **Three state categories, kept separate.** Aggregation state: count, sum, inherited count and sum. Identity state: representatives, confidence, matching metadata. Uncertainty state: a quarantine ledger of ambiguous mass attached to the top-2 candidate counters. Uncertainty never modifies identity without a justified promotion rule.
- **Frozen leader representatives.** Refinement is added only after identity handling is shown to be stable.
- **Observed-ambiguous items** go to quarantine, never seed a counter, and never move a representative.
- **Merge** fuses counters only above the threshold; matches inside the ambiguous band become alias links, not fusions.
- **When refinement is added:** the mean vector uses only genuine assigned observations (count minus inherited count), with mass-weighted steps and a per-counter drift budget.

### Research questions

**B. Violation stability (first priority).** Can propagated error satisfy E_prop ≤ C_stab · V, independent of stream length and merge tree? Known amplification mechanisms: boundary seeding, mean drift, fusion at merge, outlier seeding, and undetected latent ambiguity. Stability must come from bounding each observation's influence on identity state, not from detecting ambiguity, since latent ambiguity is not always detectable.

**A. Representative refinement (second).** Does mass-dependent refinement reduce the required separation from 4ρ toward 2ρ for heavy entities? In the non-violating regime the acceptance region contains the whole entity ball, so assignment is not selective and the accepted observations stay i.i.d. Adaptive-assignment bias arises only under violation, which makes A conditional on B.

**Merge theorem.** Under the identity condition, each sketch is isomorphic to an exact-key Space-Saving summary over latent indices, so the mergeable-summaries result of Agarwal et al. carries over to arbitrary merge trees. Answers can depend on order; error bounds cannot. Needs a formal write-up of the reduction.

**Mean intervals.** Guaranteed intervals from count and sum bounds plus [a, b], not statistical confidence intervals. They will be wide for small entities with large inherited mass; the results must show that.

### Delta against the closest work

| Capability | MaxSketch | Space-Saving / Ting | CLIMB-style k-means | Sample + k-means | Cardinex (target) |
| --- | --- | --- | --- | --- | --- |
| Near-duplicate identity | Yes | No | Yes | Yes | Yes |
| Weighted sums per entity | No | Exact keys | Offline | Probabilistic | Yes, nonnegative bounded values |
| Mean with interval | No | Variance via ratio | No | Statistical CI | Guaranteed interval |
| Streaming, one pass | Yes | Yes | No | Yes | Yes |
| Mergeable over arbitrary trees | Yes | Yes | No | Yes | Yes, under the identity condition |
| Bounded damage from identity violation | Not addressed | n/a | Not addressed | Not addressed | Target of question B |
| Memory for εW error (counts, sums) | n/a | O(1/ε) counters | All vectors | O(log(1/δ)/ε²) vectors | O(1/ε) counters with O(d·b) bits each |

### Threats and open checks

- [ ] Read Zhang, PODS 2025, and Ting, SIGMOD 2018; position the contribution precisely against both.
- [ ] Separation measurement on constructed-identity datasets decides how much of Plan A applies in practice.
- [ ] Six-attack prototype with control run: if amplification grows with stream length under quarantine, redesign identity handling before anything else.
- [ ] Equal-byte comparison against sampling and offline k-means, counting representatives, counters, and metadata.
- [ ] Expect "leader clustering plus Space-Saving." The answer is the violation-stability result, not the identity lemma.

## Evaluation and publication plan

Target the final PVLDB Volume 20 cycle (abstract Feb 25, 2027, paper Mar 1, 2027), with an arXiv preprint first and a data-centric ML workshop in parallel.

### Phase 0: experiments that decide the architecture

Run both in parallel before any proof work or SDK work.

| Experiment | Setup | Measures |
| --- | --- | --- |
| Separation feasibility | Constructed-identity groups: DISC21 image augmentation groups, LLM-generated paraphrase groups from seed sentences. Quora duplicates and MTEB clustering labels only as external references, not proof the model applies | Within- and between-group distance distributions including tails; recall vs false-match rate of one global threshold; the same after compression; measured violation fraction |
| Adversarial prototype | Minimal Go engine (ingest, top-k, merge) running model v0; six attacks, each against a counterfactual control run with the same order, capacity, and merge tree | Amplification, false merges, false splits, heavy-hitter recall, memory, sensitivity to merge-tree shape |

Amplification is measured against the control, so ordinary Space-Saving error is not counted as contamination:

```latex
A_{\mathrm{cont}} = \frac{V + E_{\mathrm{prop}}}{V}, \quad V > 0
```

The six attacks:

1. **Boundary seed:** one point midway between two heavy entities, inserted right after an eviction.
2. **Drift injection:** a burst of ambiguous items aimed at a young counter.
3. **Merge fusion:** two sketches whose representatives of different entities land in the ambiguous band.
4. **Outlier seed:** a far point placed so two later entities both fall within τ of it; compare the 6ρ condition against probation.
5. **Hidden second entity:** an ambiguous observation arrives while only one of the two relevant entities has a counter.
6. **Coordinated probation poisoning:** several outliers reinforce a false provisional representative.

Decision rule: amplification near 1 under all six with quarantine means the violation-stability theorem is a realistic proof target. Amplification that grows with stream length means identity handling is redesigned first.

### Phase 1: paper experiments

| Experiment | Data | Baselines | What it must show |
| --- | --- | --- | --- |
| Equal-byte showdown | Synthetic + real embeddings | Sample + k-means; offline k-means | Lower count and sum error per byte, counting all state; honest per-entity comparison for means |
| Bias of hashing | Same | SimHash + Count-Min; SimHash + Space-Saving | The LSH bias MaxSketch predicts |
| Separation sweep | Synthetic, varying ρ, Δ, η | Full-precision leader clustering | Where bounds hold, where they break, how amplification behaves |
| Corpus analytics | [ClimbLab](https://research.nvidia.com/labs/lpr/climb) slice | CLIMB's offline clusters | Redundancy at near-duplicate grain (Plan A); composition at topic grain (Plan B) |
| Eval slicing | Benchmark items + released predictions | Exact per-group accuracy | Per-entity accuracy with guaranteed intervals; distinct-weighted vs raw accuracy |
| Throughput | Multi-core ingest and merge | Exact grouping | Items/s, merge latency, memory |
| Training reweighting (stretch) | ~125M model, a few B tokens | Uniform; static mixture | Future work if compute is short |

Lead the paper with one shareable finding, such as the near-duplicate share of a popular benchmark and how distinct-weighted accuracy changes its leaderboard.

### Venues

| Venue | Fit | Notes |
| --- | --- | --- |
| [PVLDB Vol. 20 / VLDB 2027](https://www.vldb.org/pvldb/volumes/20/submission) | Best | Rolling monthly deadlines; reproducibility package required at initial submission; must connect non-superficially to DB literature; Regular (algorithms) if proofs are strong, Scalable Data Science if experiments carry it; missed cycles roll to VLDB 2028 |
| ICML 2027 data-centric workshop | Good, low risk | Usually non-archival; check dual-submission rules |
| KDD applied track | Reasonable | Needs a strong real-world use case |
| NeurIPS / ICML main | Stretch | Needs a real theory result or a training improvement |

### Sequence

1. Read Zhang (PODS 2025) and Ting (SIGMOD 2018).
2. Freeze model v0; build the Go prototype; run the six attacks and the separation study in parallel.
3. Decide from the results: upper bound or characterization (including the 6ρ lower bound); Plan A scope vs Plan B scope.
4. Proofs in order: violation stability (B), merge reduction, then refinement (A) if B holds.
5. Phase 1 experiments; Python binding; arXiv preprint.
6. PVLDB abstract by Feb 25, 2027 and paper with reproducibility package by Mar 1, 2027. With theory now on the critical path, this date is tight; if Phase 0 or the proofs slip, target the VLDB 2028 monthly cycles instead of rushing.

## USP for developers

**USP:** ask per-cluster questions (how much, how bad, how redundant) over embedding data that does not fit in memory, from any number of workers, without running a clustering job, with error bars on every answer.

### Who uses it

| User | Their question | What Cardinex replaces |
| --- | --- | --- |
| Pretraining / fine-tuning data engineers | Composition, redundancy, and quality by semantic region | Offline embed → store → k-means → join pipeline |
| Data-mixing researchers | Per-cluster loss during training, for reweighting | Static mixtures; proxy-model search loops |
| Synthetic data teams | Mode collapse, per-mode redundancy caps | Post-hoc clustering of generated data |
| Eval infrastructure teams | Per-slice accuracy, regressions across checkpoints, benchmark redundancy | Hand-built taxonomies; full re-clustering per run |
| MLOps / monitoring | Semantic drift of inputs and outputs | Periodic batch jobs over logged embeddings |

Not the user: anyone whose vectors fit in memory, whose data is static, and who can afford a global pass. They should run FAISS k-means directly, and the README should say so.

### Why they adopt it

1. **No clustering stage.** Aggregation happens inline as embeddings flow; no intermediate store of every vector.
2. **Native to distributed pipelines.** Each Ray, Spark, or datatrove worker builds a sketch; merge at reduce. That is how these pipelines already run.
3. **Incremental.** New crawl, new checkpoint, new day of traffic: add to the existing sketch, no recompute.
4. **Diffable.** Two sketches answer "what changed between v1 and v2" by cluster.
5. **Decision-grade answers.** "Entity 17 mean loss is guaranteed to lie in [2.27, 2.35]" can drive reweighting; an unbounded point estimate cannot.

### Competition

The real competitors are workflows, not libraries: sample + k-means, BERTopic-style topic modelling, and embedding-analytics tools such as Nomic Atlas. The answer to all of them is the same: streaming, mergeable, incremental, bounded error, and O(1/ε) versus O(1/ε²) memory against sampling.

### Adoption requirements

- [ ] `pip install cardinex`: Go core as a C shared library with Python wheels. ML engineers will not adopt a Go-only API.
- [ ] Integrations: Hugging Face `datasets`, Ray Data, datatrove / NeMo Curator, Spark UDAF; one notebook each.
- [ ] Stable, versioned serialization so sketches can be stored, merged later, and diffed.
- [ ] A public demo result (a corpus or benchmark finding) that people share.

Go stays the right engine behind that surface: a single static binary for CLI and service, real multi-core ingest without a GIL, and easy embedding as a pipeline sidecar.

## USP for automated agents

**USP:** a Cardinex sketch is a context-window-sized, queryable summary of a dataset too large for any agent to read, and every answer tells the agent how much to trust it.

Agents doing data curation, eval analysis, or monitoring cannot read 100M rows, and summarising by sampling into context gives them no error bounds. A sketch gives them the shape of the data (top entities, per-entity stats, what changed) in a few kilobytes of structured output, plus a signal for when to drill down instead of acting.

### Why agents benefit

1. **Compression that fits context.** Top-k entities with count, sum, mean, interval, and exemplar IDs fit in one tool result.
2. **Calibrated autonomy.** Intervals and the ambiguous-mass flag let an agent decide on its own: act if the interval is tight, fetch exact data for that cluster if not.
3. **Cheap repeated queries.** Querying a sketch costs microseconds; re-embedding or re-reading data costs model calls and money.
4. **Mergeable across agents.** Sub-agents or parallel workers each build a sketch over their shard; the orchestrator merges them without shipping raw data.
5. **Diff as a primitive.** "What changed since the last run" becomes one call, which is what scheduled monitoring agents need.

### Agent use cases

| Use case | Stream | Aggregate | Agent action |
| --- | --- | --- | --- |
| Autonomous data curation | Corpus embeddings + quality scores | Mean quality, redundancy per cluster | Prune or downweight clusters with tight low-quality intervals |
| Eval triage | Benchmark items + correctness | Per-slice accuracy, checkpoint diff | File regressions on slices whose accuracy dropped beyond the interval |
| Synthetic data control | Generated samples | Count per mode | Stop or redirect generation when one mode exceeds a cap |
| Agent loop detection | The agent's own tool calls and states, windowed | Count per semantic action | Break out after semantically identical calls repeat |
| Agent trajectory analytics | Many agent runs, with outcome scores | Failure rate per semantic failure mode | Prioritise the dominant failure modes for fixes |
| Production monitoring | Model inputs and outputs | Drift by cluster | Alert only on drift that exceeds error bounds |

### Agent interface requirements

- [ ] MCP server exposing `create`, `add`, `merge`, `topk`, `query`, `diff`, `exemplars`, with compact deterministic JSON.
- [ ] Every numeric answer returns its interval and an ambiguous-mass figure, never a bare point estimate.
- [ ] Exemplar item IDs per entity, so an agent can fetch real examples of a cluster before acting on it.
- [ ] Idempotent, retry-safe writes keyed by batch ID, since agents retry.
- [ ] Explicit "insufficient confidence" responses instead of low-quality answers.

One caveat to state up front: stored representatives are compressed embeddings, and embeddings can be partially inverted. Sketches reduce raw-data exposure but are not a privacy guarantee.
