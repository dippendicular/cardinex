package io.github.dippendicular.cardinex;

import java.lang.foreign.*;
import java.lang.invoke.MethodHandle;
import java.lang.ref.Cleaner;
import java.nio.charset.StandardCharsets;
import java.nio.file.Path;

import static java.lang.foreign.ValueLayout.*;

/**
 * A Cardinex sketch over libcardinex via the Panama FFM API (JDK 22+).
 * Close it (try-with-resources) or let the Cleaner free it.
 */
public final class Sketch implements AutoCloseable {
    public record Estimate(double value, double lower, double upper, boolean empty) {}

    public static final class CardinexException extends RuntimeException {
        public final int code;
        public final long index;
        CardinexException(int code, String msg, long index) { super(msg); this.code = code; this.index = index; }
    }

    private static final Linker LINKER = Linker.nativeLinker();
    private static final SymbolLookup LIB = load();
    private static final Cleaner CLEANER = Cleaner.create();
    private static final StructLayout ESTIMATE = MemoryLayout.structLayout(
            JAVA_DOUBLE.withName("value"), JAVA_DOUBLE.withName("lower"), JAVA_DOUBLE.withName("upper"),
            JAVA_INT.withName("empty"), MemoryLayout.paddingLayout(4));

    private static SymbolLookup load() {
        String p = System.getProperty("cardinex.lib", System.getenv("CARDINEX_LIB"));
        if (p != null) return SymbolLookup.libraryLookup(Path.of(p), Arena.global());
        return SymbolLookup.libraryLookup(System.mapLibraryName("cardinex"), Arena.global());
    }

    private static MethodHandle fn(String name, MemoryLayout ret, MemoryLayout... args) {
        FunctionDescriptor d = ret == null ? FunctionDescriptor.ofVoid(args) : FunctionDescriptor.of(ret, args);
        return LINKER.downcallHandle(LIB.find(name).orElseThrow(), d);
    }

    private static final MethodHandle LAST_ERROR = fn("cdnx_last_error", JAVA_INT, ADDRESS, JAVA_LONG);
    private static final MethodHandle BYTES_FREE = fn("cdnx_bytes_free", null, ADDRESS);
    private static final MethodHandle NEW = fn("cdnx_new", JAVA_INT, ADDRESS, ADDRESS, ADDRESS);
    private static final MethodHandle UNMARSHAL = fn("cdnx_unmarshal", JAVA_INT, ADDRESS, JAVA_LONG, ADDRESS);
    private static final MethodHandle MARSHAL = fn("cdnx_marshal", JAVA_INT, JAVA_LONG, ADDRESS, ADDRESS);
    private static final MethodHandle MERGE = fn("cdnx_merge", JAVA_INT, JAVA_LONG, JAVA_LONG);
    private static final MethodHandle STATS = fn("cdnx_stats_json", JAVA_INT, JAVA_LONG, ADDRESS, ADDRESS);
    private static final MethodHandle FREE = fn("cdnx_free", JAVA_INT, JAVA_LONG);
    private static final MethodHandle ADD_KEY = fn("cdnx_add_key", JAVA_INT, JAVA_LONG, ADDRESS, JAVA_LONG, JAVA_LONG);
    private static final MethodHandle HLL_EST = fn("cdnx_hll_estimate", JAVA_INT, JAVA_LONG, ADDRESS);
    private static final MethodHandle CMS_COUNT = fn("cdnx_cms_count", JAVA_INT, JAVA_LONG, ADDRESS, JAVA_LONG, ADDRESS);
    private static final MethodHandle BLOOM_HAS = fn("cdnx_bloom_contains", JAVA_INT, JAVA_LONG, ADDRESS, JAVA_LONG, ADDRESS);
    private static final MethodHandle SEM_ADD = fn("cdnx_semantic_add", JAVA_INT, JAVA_LONG, ADDRESS, JAVA_LONG, JAVA_DOUBLE, JAVA_LONG);
    private static final MethodHandle SEM_BATCH = fn("cdnx_semantic_add_batch", JAVA_INT, JAVA_LONG, ADDRESS, JAVA_LONG, JAVA_LONG, ADDRESS, ADDRESS, ADDRESS);
    private static final MethodHandle SEM_TOPK = fn("cdnx_semantic_topk_json", JAVA_INT, JAVA_LONG, JAVA_INT, ADDRESS, ADDRESS, ADDRESS);
    private static final MethodHandle SEM_LOOKUP = fn("cdnx_semantic_lookup_json", JAVA_INT, JAVA_LONG, ADDRESS, JAVA_LONG, ADDRESS, ADDRESS);
    private static final MethodHandle SEM_DIFF = fn("cdnx_semantic_diff_json", JAVA_INT, JAVA_LONG, JAVA_LONG, ADDRESS, ADDRESS);

    private final long h;
    private final Cleaner.Cleanable cleanable;

    private Sketch(long h) {
        this.h = h;
        this.cleanable = CLEANER.register(this, () -> invoke(FREE, h));
    }

    /** kind: semantic, hll, cms or bloom; params use the HTTP API JSON shape. */
    public Sketch(String kind, String paramsJson) {
        this(create(kind, paramsJson));
    }

    private static long create(String kind, String params) {
        try (Arena a = Arena.ofConfined()) {
            MemorySegment out = a.allocate(JAVA_LONG);
            check((int) invoke(NEW, a.allocateFrom(kind), a.allocateFrom(params == null ? "{}" : params), out), -1);
            return out.get(JAVA_LONG, 0);
        }
    }

    public static Sketch load(byte[] bytes) {
        try (Arena a = Arena.ofConfined()) {
            MemorySegment out = a.allocate(JAVA_LONG);
            check((int) invoke(UNMARSHAL, a.allocateFrom(JAVA_BYTE, bytes), (long) bytes.length, out), -1);
            return new Sketch(out.get(JAVA_LONG, 0));
        }
    }

    @Override public void close() { cleanable.clean(); }

    public void merge(Sketch other) { check((int) invoke(MERGE, h, other.h), -1); }

    public byte[] toBytes() { return out(MARSHAL); }

    public String statsJson() { return new String(out(STATS), StandardCharsets.UTF_8); }

    public void addKey(String key, long n) {
        try (Arena a = Arena.ofConfined()) {
            byte[] k = key.getBytes(StandardCharsets.UTF_8);
            check((int) invoke(ADD_KEY, h, a.allocateFrom(JAVA_BYTE, k), (long) k.length, n), -1);
        }
    }

    public Estimate hllEstimate() {
        try (Arena a = Arena.ofConfined()) {
            MemorySegment e = a.allocate(ESTIMATE);
            check((int) invoke(HLL_EST, h, e), -1);
            return est(e);
        }
    }

    public Estimate cmsCount(String key) {
        try (Arena a = Arena.ofConfined()) {
            byte[] k = key.getBytes(StandardCharsets.UTF_8);
            MemorySegment e = a.allocate(ESTIMATE);
            check((int) invoke(CMS_COUNT, h, a.allocateFrom(JAVA_BYTE, k), (long) k.length, e), -1);
            return est(e);
        }
    }

    public boolean bloomContains(String key) {
        try (Arena a = Arena.ofConfined()) {
            byte[] k = key.getBytes(StandardCharsets.UTF_8);
            MemorySegment r = a.allocate(JAVA_INT);
            check((int) invoke(BLOOM_HAS, h, a.allocateFrom(JAVA_BYTE, k), (long) k.length, r), -1);
            return r.get(JAVA_INT, 0) != 0;
        }
    }

    public void add(float[] vec, double value, long id) {
        try (Arena a = Arena.ofConfined()) {
            check((int) invoke(SEM_ADD, h, a.allocateFrom(JAVA_FLOAT, vec), (long) vec.length, value, id), -1);
        }
    }

    /** All-or-nothing; vecs is a native or heap segment of n×dim floats, row-major. */
    public void addBatch(MemorySegment vecs, int n, int dim, double[] values, long[] ids) {
        try (Arena a = Arena.ofConfined()) {
            MemorySegment fail = a.allocate(JAVA_LONG);
            int st = (int) invoke(SEM_BATCH, h, vecs, (long) n, (long) dim,
                    a.allocateFrom(JAVA_DOUBLE, values), a.allocateFrom(JAVA_LONG, ids), fail);
            check(st, fail.get(JAVA_LONG, 0));
        }
    }

    public String topKJson(int k, String by) {
        try (Arena a = Arena.ofConfined()) {
            MemorySegment p = a.allocate(ADDRESS), n = a.allocate(JAVA_LONG);
            check((int) invoke(SEM_TOPK, h, k, a.allocateFrom(by), p, n), -1);
            return new String(take(p, n), StandardCharsets.UTF_8);
        }
    }

    public String lookupJson(float[] vec) {
        try (Arena a = Arena.ofConfined()) {
            MemorySegment p = a.allocate(ADDRESS), n = a.allocate(JAVA_LONG);
            check((int) invoke(SEM_LOOKUP, h, a.allocateFrom(JAVA_FLOAT, vec), (long) vec.length, p, n), -1);
            return new String(take(p, n), StandardCharsets.UTF_8);
        }
    }

    public String diffJson(Sketch other) {
        try (Arena a = Arena.ofConfined()) {
            MemorySegment p = a.allocate(ADDRESS), n = a.allocate(JAVA_LONG);
            check((int) invoke(SEM_DIFF, h, other.h, p, n), -1);
            return new String(take(p, n), StandardCharsets.UTF_8);
        }
    }

    private byte[] out(MethodHandle f) {
        try (Arena a = Arena.ofConfined()) {
            MemorySegment p = a.allocate(ADDRESS), n = a.allocate(JAVA_LONG);
            check((int) invoke(f, h, p, n), -1);
            return take(p, n);
        }
    }

    private static byte[] take(MemorySegment p, MemorySegment n) {
        MemorySegment buf = p.get(ADDRESS, 0).reinterpret(n.get(JAVA_LONG, 0));
        try {
            return buf.toArray(JAVA_BYTE);
        } finally {
            invoke(BYTES_FREE, p.get(ADDRESS, 0));
        }
    }

    private static Estimate est(MemorySegment e) {
        return new Estimate(e.get(JAVA_DOUBLE, 0), e.get(JAVA_DOUBLE, 8), e.get(JAVA_DOUBLE, 16), e.get(JAVA_INT, 24) != 0);
    }

    private static Object invoke(MethodHandle m, Object... args) {
        try {
            return m.invokeWithArguments(args);
        } catch (Throwable t) {
            throw new RuntimeException(t);
        }
    }

    private static void check(int st, long index) {
        if (st == 0) return;
        try (Arena a = Arena.ofConfined()) {
            MemorySegment buf = a.allocate(1024);
            invoke(LAST_ERROR, buf, 1024L);
            throw new CardinexException(st, buf.getString(0), index);
        }
    }
}
