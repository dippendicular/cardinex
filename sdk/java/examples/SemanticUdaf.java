// Spark UDAF: one semantic sketch per group, merged across partitions.
// Compile against spark-sql 3.5 and the cardinex jar; libcardinex must be on
// every executor (spark.executor.extraLibraryPath or -Dcardinex.lib).
import io.github.dippendicular.cardinex.Sketch;
import org.apache.spark.sql.Encoder;
import org.apache.spark.sql.Encoders;
import org.apache.spark.sql.Row;
import org.apache.spark.sql.expressions.Aggregator;

public class SemanticUdaf extends Aggregator<Row, byte[], String> {
    static final String PARAMS = "{\"dim\":384,\"tau\":0.6,\"seed\":1,\"capacity\":4096}";

    @Override public byte[] zero() {
        try (Sketch s = new Sketch("semantic", PARAMS)) { return s.toBytes(); }
    }

    // Row: (embedding array<float>, value double, id long)
    @Override public byte[] reduce(byte[] buf, Row r) {
        try (Sketch s = Sketch.load(buf)) {
            float[] v = new float[r.getList(0).size()];
            for (int i = 0; i < v.length; i++) v[i] = ((Number) r.getList(0).get(i)).floatValue();
            s.add(v, r.getDouble(1), r.getLong(2));
            return s.toBytes();
        }
    }

    @Override public byte[] merge(byte[] a, byte[] b) {
        try (Sketch x = Sketch.load(a); Sketch y = Sketch.load(b)) { x.merge(y); return x.toBytes(); }
    }

    @Override public String finish(byte[] buf) {
        try (Sketch s = Sketch.load(buf)) { return s.topKJson(20, "count"); }
    }

    @Override public Encoder<byte[]> bufferEncoder() { return Encoders.BINARY(); }
    @Override public Encoder<String> outputEncoder() { return Encoders.STRING(); }
}
