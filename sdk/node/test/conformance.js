'use strict';
// Node runner of conformance/spec.json.
const fs = require('fs');
const path = require('path');
const { HLL, CMS, Bloom, Semantic } = require('..');

const root = process.argv[2] || path.join(__dirname, '../../../conformance');
const spec = JSON.parse(fs.readFileSync(path.join(root, 'spec.json'), 'utf8'));
let failed = 0;
for (const c of spec.cases) {
  const s = new { hll: HLL, cms: CMS, bloom: Bloom, semantic: Semantic }[c.kind](c.params);
  if (c.kind === 'semantic') {
    const { dim, entities, items } = c.vectors;
    const v = new Float32Array(dim);
    for (let i = 0; i < items; i++) {
      const j = i % entities;
      for (let t = 0; t < dim; t++) {
        const noise = (0.01 * (((i * 7 + t * 13) % 11) - 5)) / 5;
        v[t] = (t === j % dim ? 1 : 0) + noise;
      }
      s.add(v, ((i * 37) % 101) / 100, BigInt(i + 1));
    }
  } else {
    const { prefix, count, modulo = 0 } = c.keys;
    for (let i = 0; i < count; i++) {
      const k = prefix + (modulo ? i % modulo : i);
      c.kind === 'cms' ? s.add(k, c.count_per_key || 1) : s.add(k);
    }
  }
  const ok = s.toBuffer().equals(fs.readFileSync(path.join(root, c.golden)));
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${c.name}`);
  if (!ok) failed++;
  s.close();
}
process.exit(failed ? 1 : 0);
