// C++ runner of conformance/spec.json: rebuilds each case and checks its bytes
// against the golden file. Usage: conformance [path/to/conformance]
#include <cardinex/cardinex.hpp>
#include <nlohmann/json.hpp>

#include <fstream>
#include <iostream>
#include <sstream>
#include <vector>

using json = nlohmann::json;

static std::string slurp(const std::string& p) {
  std::ifstream f(p, std::ios::binary);
  std::stringstream ss;
  ss << f.rdbuf();
  return ss.str();
}

int main(int argc, char** argv) {
  std::string root = argc > 1 ? argv[1] : "conformance";
  json spec = json::parse(slurp(root + "/spec.json"));
  int failed = 0;
  for (auto& c : spec["cases"]) {
    std::string kind = c["kind"];
    cardinex::Sketch s(kind, c["params"].dump());
    if (kind == "semantic") {
      auto op = c["vectors"];
      int dim = op["dim"], ents = op["entities"], items = op["items"];
      std::vector<float> v(dim);
      for (int i = 0; i < items; ++i) {
        int j = i % ents;
        for (int t = 0; t < dim; ++t) {
          double noise = 0.01 * double((i * 7 + t * 13) % 11 - 5) / 5;
          v[t] = float((t == j % dim ? 1.0 : 0.0) + noise);
        }
        s.add(v.data(), dim, double((i * 37) % 101) / 100, uint64_t(i + 1));
      }
    } else {
      auto op = c["keys"];
      int count = op["count"], mod = op.value("modulo", 0);
      uint64_t n = c.value("count_per_key", 1);
      for (int i = 0; i < count; ++i) {
        std::string k = op["prefix"].get<std::string>() + std::to_string(mod ? i % mod : i);
        s.add_key(k, kind == "cms" ? n : 1);
      }
    }
    bool ok = s.bytes() == slurp(root + "/" + c["golden"].get<std::string>());
    std::cout << (ok ? "ok   " : "FAIL ") << c["name"].get<std::string>() << "\n";
    failed += !ok;
  }
  return failed ? 1 : 0;
}
