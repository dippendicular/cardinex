"""Python runner of conformance/spec.json."""
import json
import math
import pathlib

import numpy as np

import cardinex

ROOT = pathlib.Path(__file__).resolve().parents[3] / "conformance"
SPEC = json.loads((ROOT / "spec.json").read_text())


def keys(op):
    m = op.get("modulo", 0)
    return [(op["prefix"] + str(i % m if m else i)).encode() for i in range(op["count"])]


def vector(op, i):
    j = i % op["entities"]
    out = np.empty(op["dim"], dtype=np.float32)
    for t in range(op["dim"]):
        noise = 0.01 * float((i * 7 + t * 13) % 11 - 5) / 5
        out[t] = np.float32((1.0 if t == j % op["dim"] else 0.0) + noise)
    return out


def build(case):
    cls = {"hll": cardinex.HLL, "cms": cardinex.CMS, "bloom": cardinex.Bloom, "semantic": cardinex.Semantic}[case["kind"]]
    s = cls(**case["params"])
    if case["kind"] == "semantic":
        op = case["vectors"]
        for i in range(op["items"]):
            s.add(vector(op, i), ((i * 37) % 101) / 100, i + 1)
    elif case["kind"] == "cms":
        for k in keys(case["keys"]):
            s.add(k, case.get("count_per_key") or 1)
    else:
        for k in keys(case["keys"]):
            s.add(k)
    return s


def close(a, b):
    if isinstance(a, float) or isinstance(b, float):
        return math.isclose(a, b, abs_tol=1e-9)
    if isinstance(a, list):
        return len(a) == len(b) and all(close(x, y) for x, y in zip(a, b))
    if isinstance(a, dict):
        return all(close(a[k], b[k]) for k in a if k in b)
    return a == b


def answers(case, s):
    k = case["kind"]
    if k == "hll":
        e = s.estimate()
        return {"estimate": {"value": e.value, "lower": e.lower, "upper": e.upper}}
    if k == "cms":
        return {"counts": {key: int(s.count(key).value) for key in case["expect"]["counts"]}}
    if k == "bloom":
        return {"contains": {key: key in s for key in case["expect"]["contains"]}, "set_bits": s.stats()["set_bits"]}
    st = s.stats()
    return {"topk": s.topk(0)["entities"], "stats_w": st["w"], "stats_live": st["live"]}


def test_conformance():
    for case in SPEC["cases"]:
        golden = (ROOT / case["golden"]).read_bytes()
        with build(case) as s:
            assert s.to_bytes() == golden, case["name"]
        with cardinex.load(golden) as d:
            got = answers(case, d)
            for key, val in got.items():
                assert close(val, case["expect"][key]), (case["name"], key)


def test_zero_copy_batch():
    v = np.ascontiguousarray(np.eye(16, dtype=np.float32))
    with cardinex.Semantic(dim=16, tau=0.5, seed=1, capacity=32, probation=0) as s:
        s.add_batch(v, np.ones(16), np.arange(1, 17, dtype=np.uint64))
        assert s.stats()["w"] == 16
