// C# runner of conformance/spec.json. Usage: dotnet run -- <path/to/conformance>
using System.Text.Json;
using Cardinex;

var root = args.Length > 0 ? args[0] : "conformance";
using var spec = JsonDocument.Parse(File.ReadAllText(Path.Combine(root, "spec.json")));
int failed = 0;
foreach (var c in spec.RootElement.GetProperty("cases").EnumerateArray())
{
    var kind = c.GetProperty("kind").GetString()!;
    using var s = new Sketch(kind, c.GetProperty("params").GetRawText());
    if (kind == "semantic")
    {
        var op = c.GetProperty("vectors");
        int dim = op.GetProperty("dim").GetInt32(), ents = op.GetProperty("entities").GetInt32(), items = op.GetProperty("items").GetInt32();
        var v = new float[dim];
        for (int i = 0; i < items; i++)
        {
            int j = i % ents;
            for (int t = 0; t < dim; t++)
            {
                double noise = 0.01 * (double)((i * 7 + t * 13) % 11 - 5) / 5;
                v[t] = (float)((t == j % dim ? 1.0 : 0.0) + noise);
            }
            s.Add(v, (double)((i * 37) % 101) / 100, (ulong)(i + 1));
        }
    }
    else
    {
        var op = c.GetProperty("keys");
        int count = op.GetProperty("count").GetInt32();
        int mod = op.TryGetProperty("modulo", out var m) ? m.GetInt32() : 0;
        ulong n = c.TryGetProperty("count_per_key", out var cp) ? cp.GetUInt64() : 1;
        var prefix = op.GetProperty("prefix").GetString();
        for (int i = 0; i < count; i++) s.AddKey(prefix + (mod > 0 ? i % mod : i), kind == "cms" ? n : 1);
    }
    var golden = File.ReadAllBytes(Path.Combine(root, c.GetProperty("golden").GetString()!));
    bool ok = s.ToBytes().AsSpan().SequenceEqual(golden);
    Console.WriteLine($"{(ok ? "ok  " : "FAIL")} {c.GetProperty("name").GetString()}");
    if (!ok) failed++;
}
return failed == 0 ? 0 : 1;
