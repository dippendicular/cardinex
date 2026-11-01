// Batch ingest from a float buffer.
#include <cardinex/cardinex.hpp>

#include <iostream>
#include <random>
#include <vector>

int main() {
  const size_t n = 10000, d = 384;
  std::vector<float> buf(n * d);
  std::mt19937 rng(1);
  std::normal_distribution<float> g;
  for (auto& x : buf) x = g(rng);
  std::vector<double> values(n, 1.0);
  std::vector<uint64_t> ids(n);
  for (size_t i = 0; i < n; ++i) ids[i] = i + 1;

  cardinex::Sketch s("semantic", R"({"dim":384,"tau":0.6,"seed":1,"capacity":1024})");
  s.add_batch(buf.data(), n, d, values.data(), ids.data());
  std::cout << s.topk_json(5) << "\n";
}
