using System;
using System.Runtime.InteropServices;
using System.Text;
using Microsoft.Win32.SafeHandles;

namespace Cardinex;

public sealed class CardinexException : Exception
{
    public int Code { get; }
    public long Index { get; }
    public CardinexException(int code, string message, long index = -1) : base(message) { Code = code; Index = index; }
}

public readonly record struct Estimate(double Value, double Lower, double Upper, bool Empty)
{
    internal static Estimate From(NativeEstimate e) => new(e.Value, e.Lower, e.Upper, e.Empty != 0);
}

internal sealed class SketchHandle : SafeHandleZeroOrMinusOneIsInvalid
{
    public SketchHandle(ulong h) : base(true) => SetHandle((IntPtr)(long)h);
    public ulong Id => (ulong)(long)handle;
    protected override bool ReleaseHandle() => Native.cdnx_free(Id) == 0;
}

/// <summary>A Cardinex sketch. Dispose it (or rely on the finalizer) to free native memory.</summary>
public unsafe class Sketch : IDisposable
{
    private readonly SketchHandle _h;
    internal ulong H => _h.Id;

    internal Sketch(ulong h) => _h = new SketchHandle(h);

    /// <param name="kind">semantic, hll, cms or bloom</param>
    /// <param name="paramsJson">constructor params, same JSON shape as the HTTP API</param>
    public Sketch(string kind, string paramsJson = "{}")
    {
        fixed (byte* k = Z(kind), p = Z(paramsJson))
        {
            Check(Native.cdnx_new(k, p, out var h));
            _h = new SketchHandle(h);
        }
    }

    public static Sketch Load(ReadOnlySpan<byte> bytes)
    {
        fixed (byte* b = bytes)
        {
            Check(Native.cdnx_unmarshal(b, (nuint)bytes.Length, out var h));
            return new Sketch(h);
        }
    }

    public ushort Kind { get { Check(Native.cdnx_kind(H, out var k)); return k; } }
    public void Merge(Sketch other) => Check(Native.cdnx_merge(H, other.H));

    public byte[] ToBytes()
    {
        Check(Native.cdnx_marshal(H, out var p, out var n));
        return Take(p, n);
    }

    public string StatsJson()
    {
        Check(Native.cdnx_stats_json(H, out var p, out var n));
        return Encoding.UTF8.GetString(Take(p, n));
    }

    // Classic sketches.
    public void AddKey(ReadOnlySpan<byte> key, ulong n = 1)
    {
        fixed (byte* k = key) Check(Native.cdnx_add_key(H, k, (nuint)key.Length, n));
    }
    public void AddKey(string key, ulong n = 1) => AddKey(Encoding.UTF8.GetBytes(key), n);

    public Estimate HllEstimate() { Check(Native.cdnx_hll_estimate(H, out var e)); return Estimate.From(e); }

    public Estimate CmsCount(string key)
    {
        var b = Encoding.UTF8.GetBytes(key);
        fixed (byte* k = b) { Check(Native.cdnx_cms_count(H, k, (nuint)b.Length, out var e)); return Estimate.From(e); }
    }

    public bool BloomContains(string key)
    {
        var b = Encoding.UTF8.GetBytes(key);
        fixed (byte* k = b) { Check(Native.cdnx_bloom_contains(H, k, (nuint)b.Length, out var r)); return r != 0; }
    }

    // Semantic sketch.
    public void Add(ReadOnlySpan<float> vec, double value, ulong id)
    {
        fixed (float* v = vec) Check(Native.cdnx_semantic_add(H, v, (nuint)vec.Length, value, id));
    }

    /// <summary>All-or-nothing. vecs is row-major n×dim and is read in place.</summary>
    public void AddBatch(ReadOnlySpan<float> vecs, int dim, ReadOnlySpan<double> values, ReadOnlySpan<ulong> ids)
    {
        int n = values.Length;
        if (vecs.Length != n * dim || ids.Length != n) throw new ArgumentException("shape mismatch");
        fixed (float* v = vecs) fixed (double* x = values) fixed (ulong* i = ids)
        {
            int st = Native.cdnx_semantic_add_batch(H, v, (nuint)n, (nuint)dim, x, i, out var fail);
            Check(st, fail);
        }
    }

    public string TopKJson(int k, string by = "count")
    {
        fixed (byte* b = Z(by)) { Check(Native.cdnx_semantic_topk_json(H, k, b, out var p, out var n)); return Encoding.UTF8.GetString(Take(p, n)); }
    }

    public string LookupJson(ReadOnlySpan<float> vec)
    {
        fixed (float* v = vec) { Check(Native.cdnx_semantic_lookup_json(H, v, (nuint)vec.Length, out var p, out var n)); return Encoding.UTF8.GetString(Take(p, n)); }
    }

    public string DiffJson(Sketch other)
    {
        Check(Native.cdnx_semantic_diff_json(H, other.H, out var p, out var n));
        return Encoding.UTF8.GetString(Take(p, n));
    }

    public void Dispose() { _h.Dispose(); GC.SuppressFinalize(this); }

    private static byte[] Z(string s) => Encoding.UTF8.GetBytes(s + "\0");

    private static byte[] Take(byte* p, nuint n)
    {
        try { return new ReadOnlySpan<byte>(p, checked((int)n)).ToArray(); }
        finally { Native.cdnx_bytes_free(p); }
    }

    internal static void Check(int st, long index = -1)
    {
        if (st == 0) return;
        var buf = stackalloc byte[1024];
        Native.cdnx_last_error(buf, 1024);
        throw new CardinexException(st, Marshal.PtrToStringUTF8((IntPtr)buf) ?? "", index);
    }
}
