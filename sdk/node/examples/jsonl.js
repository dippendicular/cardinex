'use strict';
// Stream pipeline over JSONL embeddings: {"id": 1, "embedding": [...], "value": 0.3}
// Usage: node examples/jsonl.js data.jsonl 384
const fs = require('fs');
const readline = require('readline');
const { Semantic } = require('..');

async function main() {
  const [file, dimArg] = process.argv.slice(2);
  const dim = Number(dimArg);
  const s = new Semantic({ dim, tau: 0.6, seed: 1, capacity: 4096 });
  const batch = 512;
  let vecs = new Float32Array(batch * dim), vals = [], ids = [];
  const flush = () => {
    if (!ids.length) return;
    s.addBatch(vecs.subarray(0, ids.length * dim), dim, vals, ids);
    vals = []; ids = [];
  };
  for await (const line of readline.createInterface({ input: fs.createReadStream(file) })) {
    if (!line.trim()) continue;
    const r = JSON.parse(line);
    vecs.set(r.embedding, ids.length * dim);
    vals.push(r.value ?? 1);
    ids.push(r.id);
    if (ids.length === batch) flush();
  }
  flush();
  console.log(JSON.stringify(s.topk(10, 'count'), null, 2));
  s.close();
}

main();
