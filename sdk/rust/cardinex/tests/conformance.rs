// Rust runner of conformance/spec.json.
use cardinex::Sketch;
use serde_json::Value;
use std::path::PathBuf;

#[test]
fn golden_bytes() {
    let root = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../conformance");
    let spec: Value = serde_json::from_str(&std::fs::read_to_string(root.join("spec.json")).unwrap()).unwrap();
    for c in spec["cases"].as_array().unwrap() {
        let kind = c["kind"].as_str().unwrap();
        let s = Sketch::new(kind, &c["params"].to_string()).unwrap();
        if kind == "semantic" {
            let op = &c["vectors"];
            let (dim, ents, items) = (op["dim"].as_u64().unwrap() as usize, op["entities"].as_u64().unwrap() as usize, op["items"].as_u64().unwrap() as usize);
            let mut v = vec![0f32; dim];
            for i in 0..items {
                let j = i % ents;
                for t in 0..dim {
                    let noise = 0.01 * (((i * 7 + t * 13) % 11) as f64 - 5.0) / 5.0;
                    v[t] = ((if t == j % dim { 1.0 } else { 0.0 }) + noise) as f32;
                }
                s.add(&v, ((i * 37) % 101) as f64 / 100.0, (i + 1) as u64).unwrap();
            }
        } else {
            let op = &c["keys"];
            let (count, m) = (op["count"].as_u64().unwrap(), op["modulo"].as_u64().unwrap_or(0));
            let n = c["count_per_key"].as_u64().unwrap_or(1);
            for i in 0..count {
                let k = format!("{}{}", op["prefix"].as_str().unwrap(), if m > 0 { i % m } else { i });
                s.add_key(k.as_bytes(), if kind == "cms" { n } else { 1 }).unwrap();
            }
        }
        let golden = std::fs::read(root.join(c["golden"].as_str().unwrap())).unwrap();
        assert_eq!(s.to_bytes().unwrap(), golden, "{}", c["name"]);
    }
}
