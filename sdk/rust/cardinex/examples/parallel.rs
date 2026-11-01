// Rayon parallel ingest: one sketch per chunk, merged at the end.
use cardinex::Sketch;
use rayon::prelude::*;

const PARAMS: &str = r#"{"dim":384,"tau":0.6,"seed":1,"capacity":4096}"#;

fn main() {
    let (n, dim) = (100_000usize, 384usize);
    let vecs: Vec<f32> = (0..n * dim).map(|i| ((i * 2654435761) % 1000) as f32 / 1000.0 - 0.5).collect();
    let merged = vecs
        .par_chunks(1024 * dim)
        .enumerate()
        .map(|(c, chunk)| {
            let s = Sketch::new("semantic", PARAMS).unwrap();
            let rows = chunk.len() / dim;
            let ids: Vec<u64> = (0..rows).map(|r| (c * 1024 + r + 1) as u64).collect();
            s.add_batch(chunk, dim, &vec![1.0; rows], &ids).unwrap();
            s
        })
        .reduce_with(|a, b| {
            a.merge(&b).unwrap();
            a
        })
        .unwrap();
    println!("{}", merged.topk_json(5, "count").unwrap());
}
