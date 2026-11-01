//! Safe Rust API over libcardinex. All math runs in the Go core.
use cardinex_sys as sys;
use std::ffi::{CStr, CString};
use std::fmt;

/// A library error: status code, message, and the failing batch index if any.
#[derive(Debug, Clone)]
pub struct Error {
    pub code: i32,
    pub message: String,
    pub index: Option<i64>,
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "cardinex error {}: {}", self.code, self.message)
    }
}
impl std::error::Error for Error {}

pub type Result<T> = std::result::Result<T, Error>;

fn check(st: i32, index: i64) -> Result<()> {
    if st == 0 {
        return Ok(());
    }
    let mut buf = [0 as std::os::raw::c_char; 1024];
    unsafe { sys::cdnx_last_error(buf.as_mut_ptr(), buf.len()) };
    let message = unsafe { CStr::from_ptr(buf.as_ptr()) }.to_string_lossy().into_owned();
    Err(Error { code: st, message, index: (index >= 0).then_some(index) })
}

unsafe fn take(p: *mut u8, n: usize) -> Vec<u8> {
    let v = std::slice::from_raw_parts(p, n).to_vec();
    sys::cdnx_bytes_free(p);
    v
}

#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Estimate {
    pub value: f64,
    pub lower: f64,
    pub upper: f64,
    pub empty: bool,
}

impl From<sys::cdnx_estimate_t> for Estimate {
    fn from(e: sys::cdnx_estimate_t) -> Self {
        Estimate { value: e.value, lower: e.lower, upper: e.upper, empty: e.empty != 0 }
    }
}

/// A sketch handle. Freed on drop. The library locks internally, so a Sketch
/// is Send + Sync.
pub struct Sketch {
    h: u64,
}

unsafe impl Send for Sketch {}
unsafe impl Sync for Sketch {}

impl Drop for Sketch {
    fn drop(&mut self) {
        unsafe { sys::cdnx_free(self.h) };
    }
}

impl Sketch {
    /// kind: "semantic", "hll", "cms" or "bloom"; params use the HTTP API JSON shape.
    pub fn new(kind: &str, params_json: &str) -> Result<Self> {
        let (k, p) = (CString::new(kind).unwrap(), CString::new(params_json).unwrap());
        let mut h = 0u64;
        check(unsafe { sys::cdnx_new(k.as_ptr(), p.as_ptr(), &mut h) }, -1)?;
        Ok(Sketch { h })
    }

    pub fn from_bytes(b: &[u8]) -> Result<Self> {
        let mut h = 0u64;
        check(unsafe { sys::cdnx_unmarshal(b.as_ptr(), b.len(), &mut h) }, -1)?;
        Ok(Sketch { h })
    }

    fn out(&self, f: unsafe extern "C" fn(u64, *mut *mut u8, *mut usize) -> i32) -> Result<Vec<u8>> {
        let (mut p, mut n) = (std::ptr::null_mut(), 0usize);
        check(unsafe { f(self.h, &mut p, &mut n) }, -1)?;
        Ok(unsafe { take(p, n) })
    }

    pub fn to_bytes(&self) -> Result<Vec<u8>> {
        self.out(sys::cdnx_marshal)
    }

    pub fn stats_json(&self) -> Result<String> {
        Ok(String::from_utf8_lossy(&self.out(sys::cdnx_stats_json)?).into_owned())
    }

    pub fn merge(&self, other: &Sketch) -> Result<()> {
        check(unsafe { sys::cdnx_merge(self.h, other.h) }, -1)
    }

    pub fn kind(&self) -> Result<u16> {
        let mut k = 0u16;
        check(unsafe { sys::cdnx_kind(self.h, &mut k) }, -1)?;
        Ok(k)
    }

    /// Classic sketches; n is the occurrence count for CMS.
    pub fn add_key(&self, key: &[u8], n: u64) -> Result<()> {
        check(unsafe { sys::cdnx_add_key(self.h, key.as_ptr(), key.len(), n) }, -1)
    }

    pub fn hll_estimate(&self) -> Result<Estimate> {
        let mut e = sys::cdnx_estimate_t { value: 0.0, lower: 0.0, upper: 0.0, empty: 0 };
        check(unsafe { sys::cdnx_hll_estimate(self.h, &mut e) }, -1)?;
        Ok(e.into())
    }

    pub fn cms_count(&self, key: &[u8]) -> Result<Estimate> {
        let mut e = sys::cdnx_estimate_t { value: 0.0, lower: 0.0, upper: 0.0, empty: 0 };
        check(unsafe { sys::cdnx_cms_count(self.h, key.as_ptr(), key.len(), &mut e) }, -1)?;
        Ok(e.into())
    }

    pub fn bloom_contains(&self, key: &[u8]) -> Result<bool> {
        let mut r = 0i32;
        check(unsafe { sys::cdnx_bloom_contains(self.h, key.as_ptr(), key.len(), &mut r) }, -1)?;
        Ok(r != 0)
    }

    pub fn add(&self, vec: &[f32], value: f64, id: u64) -> Result<()> {
        check(unsafe { sys::cdnx_semantic_add(self.h, vec.as_ptr(), vec.len(), value, id) }, -1)
    }

    /// All-or-nothing; vecs is row-major n×dim and is read in place.
    pub fn add_batch(&self, vecs: &[f32], dim: usize, values: &[f64], ids: &[u64]) -> Result<()> {
        let n = values.len();
        assert!(vecs.len() == n * dim && ids.len() == n, "shape mismatch");
        let mut fail = -1i64;
        let st = unsafe {
            sys::cdnx_semantic_add_batch(self.h, vecs.as_ptr(), n, dim, values.as_ptr(), ids.as_ptr(), &mut fail)
        };
        check(st, fail)
    }

    pub fn topk_json(&self, k: i32, by: &str) -> Result<String> {
        let b = CString::new(by).unwrap();
        let (mut p, mut n) = (std::ptr::null_mut(), 0usize);
        check(unsafe { sys::cdnx_semantic_topk_json(self.h, k, b.as_ptr(), &mut p, &mut n) }, -1)?;
        Ok(String::from_utf8_lossy(&unsafe { take(p, n) }).into_owned())
    }

    pub fn lookup_json(&self, vec: &[f32]) -> Result<String> {
        let (mut p, mut n) = (std::ptr::null_mut(), 0usize);
        check(unsafe { sys::cdnx_semantic_lookup_json(self.h, vec.as_ptr(), vec.len(), &mut p, &mut n) }, -1)?;
        Ok(String::from_utf8_lossy(&unsafe { take(p, n) }).into_owned())
    }

    pub fn diff_json(&self, other: &Sketch) -> Result<String> {
        let (mut p, mut n) = (std::ptr::null_mut(), 0usize);
        check(unsafe { sys::cdnx_semantic_diff_json(self.h, other.h, &mut p, &mut n) }, -1)?;
        Ok(String::from_utf8_lossy(&unsafe { take(p, n) }).into_owned())
    }
}
