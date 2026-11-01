// ASP.NET minimal API hosting one semantic sketch in-process.
using Cardinex;

var app = WebApplication.CreateBuilder(args).Build();
var sketch = new Sketch("semantic", """{"dim":384,"tau":0.6,"seed":1,"capacity":4096}""");
var gate = new object();

app.MapPost("/add", (AddRequest r) =>
{
    lock (gate) sketch.AddBatch(r.Vectors.SelectMany(v => v).ToArray(), 384, r.Values, r.Ids);
    return Results.Ok(new { added = r.Ids.Length });
});
app.MapGet("/topk", (int k, string? by) => Results.Content(sketch.TopKJson(k, by ?? "count"), "application/json"));
app.Lifetime.ApplicationStopping.Register(sketch.Dispose);
app.Run();

record AddRequest(float[][] Vectors, double[] Values, ulong[] Ids);
