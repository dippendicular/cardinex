using System;
using System.Runtime.InteropServices;

namespace Cardinex;

[StructLayout(LayoutKind.Sequential)]
public struct NativeEstimate
{
    public double Value, Lower, Upper;
    public int Empty;
}

internal static unsafe class Native
{
    // Resolves libcardinex.so / libcardinex.dylib / cardinex.dll from runtimes/<rid>/native.
    private const string Lib = "cardinex";

    [DllImport(Lib)] public static extern int cdnx_last_error(byte* buf, nuint len);
    [DllImport(Lib)] public static extern void cdnx_bytes_free(byte* p);
    [DllImport(Lib)] public static extern int cdnx_new(byte* kind, byte* paramsJson, out ulong h);
    [DllImport(Lib)] public static extern int cdnx_unmarshal(byte* buf, nuint len, out ulong h);
    [DllImport(Lib)] public static extern int cdnx_marshal(ulong h, out byte* outp, out nuint len);
    [DllImport(Lib)] public static extern int cdnx_merge(ulong into, ulong from);
    [DllImport(Lib)] public static extern int cdnx_kind(ulong h, out ushort kind);
    [DllImport(Lib)] public static extern int cdnx_stats_json(ulong h, out byte* outp, out nuint len);
    [DllImport(Lib)] public static extern int cdnx_free(ulong h);
    [DllImport(Lib)] public static extern int cdnx_add_key(ulong h, byte* key, nuint len, ulong n);
    [DllImport(Lib)] public static extern int cdnx_hll_estimate(ulong h, out NativeEstimate e);
    [DllImport(Lib)] public static extern int cdnx_cms_count(ulong h, byte* key, nuint len, out NativeEstimate e);
    [DllImport(Lib)] public static extern int cdnx_bloom_contains(ulong h, byte* key, nuint len, out int r);
    [DllImport(Lib)] public static extern int cdnx_semantic_add(ulong h, float* vec, nuint dim, double value, ulong id);
    [DllImport(Lib)] public static extern int cdnx_semantic_add_batch(ulong h, float* vecs, nuint n, nuint dim, double* values, ulong* ids, out long failIndex);
    [DllImport(Lib)] public static extern int cdnx_semantic_topk_json(ulong h, int k, byte* by, out byte* outp, out nuint len);
    [DllImport(Lib)] public static extern int cdnx_semantic_lookup_json(ulong h, float* vec, nuint dim, out byte* outp, out nuint len);
    [DllImport(Lib)] public static extern int cdnx_semantic_diff_json(ulong a, ulong b, out byte* outp, out nuint len);
}
