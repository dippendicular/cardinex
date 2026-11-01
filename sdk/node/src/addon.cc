// N-API binding over cardinex.h. Handles cross as BigInt; typed arrays are
// read in place (no copy).
#include <napi.h>

#include <string>

extern "C" {
#include "cardinex.h"
}

namespace {

void Throw(Napi::Env env, int32_t st, int64_t index = -1) {
  char buf[1024];
  cdnx_last_error(buf, sizeof buf);
  Napi::Error e = Napi::Error::New(env, buf);
  e.Set("code", Napi::Number::New(env, st));
  if (index >= 0) e.Set("index", Napi::Number::New(env, double(index)));
  throw e;
}

#define CHECK(env, st)                    \
  do {                                    \
    int32_t _s = (st);                    \
    if (_s != CDNX_OK) Throw(env, _s);    \
  } while (0)

uint64_t H(const Napi::Value& v) {
  bool lossless;
  return v.As<Napi::BigInt>().Uint64Value(&lossless);
}

Napi::Value Out(Napi::Env env, uint8_t* p, size_t n, bool asString) {
  Napi::Value r = asString ? Napi::Value(Napi::String::New(env, reinterpret_cast<char*>(p), n))
                           : Napi::Value(Napi::Buffer<uint8_t>::Copy(env, p, n));
  cdnx_bytes_free(p);
  return r;
}

Napi::Value Est(Napi::Env env, const cdnx_estimate_t& e) {
  auto o = Napi::Object::New(env);
  o.Set("value", e.value);
  o.Set("lower", e.lower);
  o.Set("upper", e.upper);
  o.Set("empty", e.empty != 0);
  return o;
}

Napi::Value New(const Napi::CallbackInfo& i) {
  uint64_t h = 0;
  std::string kind = i[0].As<Napi::String>(), params = i[1].As<Napi::String>();
  CHECK(i.Env(), cdnx_new(kind.c_str(), params.c_str(), &h));
  return Napi::BigInt::New(i.Env(), h);
}

Napi::Value Unmarshal(const Napi::CallbackInfo& i) {
  auto b = i[0].As<Napi::Buffer<uint8_t>>();
  uint64_t h = 0;
  CHECK(i.Env(), cdnx_unmarshal(b.Data(), b.Length(), &h));
  return Napi::BigInt::New(i.Env(), h);
}

Napi::Value Marshal(const Napi::CallbackInfo& i) {
  uint8_t* p;
  size_t n;
  CHECK(i.Env(), cdnx_marshal(H(i[0]), &p, &n));
  return Out(i.Env(), p, n, false);
}

Napi::Value Stats(const Napi::CallbackInfo& i) {
  uint8_t* p;
  size_t n;
  CHECK(i.Env(), cdnx_stats_json(H(i[0]), &p, &n));
  return Out(i.Env(), p, n, true);
}

Napi::Value Merge(const Napi::CallbackInfo& i) {
  CHECK(i.Env(), cdnx_merge(H(i[0]), H(i[1])));
  return i.Env().Undefined();
}

Napi::Value Kind(const Napi::CallbackInfo& i) {
  uint16_t k;
  CHECK(i.Env(), cdnx_kind(H(i[0]), &k));
  return Napi::Number::New(i.Env(), k);
}

Napi::Value Free(const Napi::CallbackInfo& i) {
  cdnx_free(H(i[0]));
  return i.Env().Undefined();
}

Napi::Value AddKey(const Napi::CallbackInfo& i) {
  auto k = i[1].As<Napi::Buffer<uint8_t>>();
  CHECK(i.Env(), cdnx_add_key(H(i[0]), k.Data(), k.Length(), uint64_t(i[2].As<Napi::Number>().Int64Value())));
  return i.Env().Undefined();
}

Napi::Value HllEstimate(const Napi::CallbackInfo& i) {
  cdnx_estimate_t e;
  CHECK(i.Env(), cdnx_hll_estimate(H(i[0]), &e));
  return Est(i.Env(), e);
}

Napi::Value CmsCount(const Napi::CallbackInfo& i) {
  auto k = i[1].As<Napi::Buffer<uint8_t>>();
  cdnx_estimate_t e;
  CHECK(i.Env(), cdnx_cms_count(H(i[0]), k.Data(), k.Length(), &e));
  return Est(i.Env(), e);
}

Napi::Value BloomContains(const Napi::CallbackInfo& i) {
  auto k = i[1].As<Napi::Buffer<uint8_t>>();
  int32_t r;
  CHECK(i.Env(), cdnx_bloom_contains(H(i[0]), k.Data(), k.Length(), &r));
  return Napi::Boolean::New(i.Env(), r != 0);
}

Napi::Value SemAdd(const Napi::CallbackInfo& i) {
  auto v = i[1].As<Napi::Float32Array>();
  bool lossless;
  CHECK(i.Env(), cdnx_semantic_add(H(i[0]), v.Data(), v.ElementLength(), i[2].As<Napi::Number>().DoubleValue(),
                                   i[3].As<Napi::BigInt>().Uint64Value(&lossless)));
  return i.Env().Undefined();
}

Napi::Value SemAddBatch(const Napi::CallbackInfo& i) {
  auto v = i[1].As<Napi::Float32Array>();
  size_t dim = i[2].As<Napi::Number>().Uint32Value();
  auto vals = i[3].As<Napi::Float64Array>();
  auto ids = i[4].As<Napi::BigUint64Array>();
  int64_t fail = -1;
  int32_t st = cdnx_semantic_add_batch(H(i[0]), v.Data(), vals.ElementLength(), dim, vals.Data(), ids.Data(), &fail);
  if (st != CDNX_OK) Throw(i.Env(), st, fail);
  return i.Env().Undefined();
}

Napi::Value TopK(const Napi::CallbackInfo& i) {
  std::string by = i[2].As<Napi::String>();
  uint8_t* p;
  size_t n;
  CHECK(i.Env(), cdnx_semantic_topk_json(H(i[0]), i[1].As<Napi::Number>().Int32Value(), by.c_str(), &p, &n));
  return Out(i.Env(), p, n, true);
}

Napi::Value Lookup(const Napi::CallbackInfo& i) {
  auto v = i[1].As<Napi::Float32Array>();
  uint8_t* p;
  size_t n;
  CHECK(i.Env(), cdnx_semantic_lookup_json(H(i[0]), v.Data(), v.ElementLength(), &p, &n));
  return Out(i.Env(), p, n, true);
}

Napi::Value Diff(const Napi::CallbackInfo& i) {
  uint8_t* p;
  size_t n;
  CHECK(i.Env(), cdnx_semantic_diff_json(H(i[0]), H(i[1]), &p, &n));
  return Out(i.Env(), p, n, true);
}

Napi::Object Init(Napi::Env env, Napi::Object exports) {
  exports.Set("new", Napi::Function::New(env, New));
  exports.Set("unmarshal", Napi::Function::New(env, Unmarshal));
  exports.Set("marshal", Napi::Function::New(env, Marshal));
  exports.Set("stats", Napi::Function::New(env, Stats));
  exports.Set("merge", Napi::Function::New(env, Merge));
  exports.Set("kind", Napi::Function::New(env, Kind));
  exports.Set("free", Napi::Function::New(env, Free));
  exports.Set("addKey", Napi::Function::New(env, AddKey));
  exports.Set("hllEstimate", Napi::Function::New(env, HllEstimate));
  exports.Set("cmsCount", Napi::Function::New(env, CmsCount));
  exports.Set("bloomContains", Napi::Function::New(env, BloomContains));
  exports.Set("semAdd", Napi::Function::New(env, SemAdd));
  exports.Set("semAddBatch", Napi::Function::New(env, SemAddBatch));
  exports.Set("topk", Napi::Function::New(env, TopK));
  exports.Set("lookup", Napi::Function::New(env, Lookup));
  exports.Set("diff", Napi::Function::New(env, Diff));
  return exports;
}

}  // namespace

NODE_API_MODULE(cardinex, Init)
