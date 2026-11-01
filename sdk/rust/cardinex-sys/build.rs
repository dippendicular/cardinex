// Generates bindings from cardinex.h and links libcardinex.
// CARDINEX_LIB_DIR points at scripts/build-lib.sh output (default: ../../../dist).
use std::{env, path::PathBuf};

fn main() {
    let dir = env::var("CARDINEX_LIB_DIR").map(PathBuf::from).unwrap_or_else(|_| {
        PathBuf::from(env::var("CARGO_MANIFEST_DIR").unwrap()).join("../../../dist")
    });
    println!("cargo:rerun-if-env-changed=CARDINEX_LIB_DIR");
    println!("cargo:rustc-link-search=native={}", dir.display());
    println!("cargo:rustc-link-lib=dylib=cardinex");

    let bindings = bindgen::Builder::default()
        .header(dir.join("cardinex.h").to_string_lossy())
        .clang_arg(format!("-I{}", dir.display()))
        .allowlist_function("cdnx_.*")
        .allowlist_type("cdnx_.*")
        .allowlist_var("CDNX_.*")
        .generate()
        .expect("bindgen cardinex.h");
    bindings
        .write_to_file(PathBuf::from(env::var("OUT_DIR").unwrap()).join("bindings.rs"))
        .unwrap();
}
