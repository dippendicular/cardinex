'use strict';
// Cardinex for Node.js. Call close() when done; a FinalizationRegistry frees
// sketches that are garbage-collected without it.
const native = require('node-gyp-build')(__dirname);

const registry = new FinalizationRegistry((h) => native.free(h));
const key = (k) => (Buffer.isBuffer(k) ? k : Buffer.from(String(k)));

class Sketch {
  constructor(kind, params = {}, handle) {
    this._h = handle ?? native.new(kind, JSON.stringify(params));
    registry.register(this, this._h, this);
  }
  static load(buf) {
    const h = native.unmarshal(buf);
    const Cls = { 1: HLL, 2: CMS, 3: Bloom, 4: Semantic }[native.kind(h)];
    return new Cls(undefined, h);
  }
  close() {
    if (this._h !== undefined) {
      registry.unregister(this);
      native.free(this._h);
      this._h = undefined;
    }
  }
  merge(other) { native.merge(this._h, other._h); }
  toBuffer() { return native.marshal(this._h); }
  stats() { return JSON.parse(native.stats(this._h)); }
}

class HLL extends Sketch {
  constructor(params = {}, h) { super('hll', params, h); }
  add(k) { native.addKey(this._h, key(k), 0); }
  estimate() { return native.hllEstimate(this._h); }
}

class CMS extends Sketch {
  constructor(params = {}, h) { super('cms', params, h); }
  add(k, n = 1) { native.addKey(this._h, key(k), n); }
  count(k) { return native.cmsCount(this._h, key(k)); }
}

class Bloom extends Sketch {
  constructor(params = {}, h) { super('bloom', params, h); }
  add(k) { native.addKey(this._h, key(k), 0); }
  has(k) { return native.bloomContains(this._h, key(k)); }
}

class Semantic extends Sketch {
  constructor(params = {}, h) { super('semantic', params, h); }
  add(vec, value = 1, id = 0n) { native.semAdd(this._h, Float32Array.from(vec), value, BigInt(id)); }
  /** All-or-nothing. vecs: Float32Array of n*dim (read in place). */
  addBatch(vecs, dim, values, ids) {
    native.semAddBatch(this._h, vecs, dim, Float64Array.from(values), BigUint64Array.from(ids, BigInt));
  }
  topk(k = 10, by = 'count') { return JSON.parse(native.topk(this._h, k, by)); }
  lookup(vec) { return JSON.parse(native.lookup(this._h, Float32Array.from(vec))); }
  diff(other) { return JSON.parse(native.diff(this._h, other._h)); }
}

module.exports = { Sketch, HLL, CMS, Bloom, Semantic };
