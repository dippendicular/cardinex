package io.github.dippendicular.cardinex;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.Test;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;

/** Java runner of conformance/spec.json. */
class ConformanceTest {
    static final Path ROOT = Path.of(System.getProperty("cardinex.conformance", "../../conformance"));

    @Test
    void goldenBytes() throws Exception {
        JsonNode spec = new ObjectMapper().readTree(ROOT.resolve("spec.json").toFile());
        for (JsonNode c : spec.get("cases")) {
            String kind = c.get("kind").asText();
            try (Sketch s = new Sketch(kind, c.get("params").toString())) {
                if (kind.equals("semantic")) {
                    JsonNode op = c.get("vectors");
                    int dim = op.get("dim").asInt(), ents = op.get("entities").asInt(), items = op.get("items").asInt();
                    float[] v = new float[dim];
                    for (int i = 0; i < items; i++) {
                        int j = i % ents;
                        for (int t = 0; t < dim; t++) {
                            double noise = 0.01 * (double) ((i * 7 + t * 13) % 11 - 5) / 5;
                            v[t] = (float) ((t == j % dim ? 1.0 : 0.0) + noise);
                        }
                        s.add(v, (double) ((i * 37) % 101) / 100, i + 1);
                    }
                } else {
                    JsonNode op = c.get("keys");
                    int count = op.get("count").asInt(), mod = op.path("modulo").asInt(0);
                    long n = c.path("count_per_key").asLong(1);
                    for (int i = 0; i < count; i++) {
                        s.addKey(op.get("prefix").asText() + (mod > 0 ? i % mod : i), kind.equals("cms") ? n : 1);
                    }
                }
                byte[] golden = Files.readAllBytes(ROOT.resolve(c.get("golden").asText()));
                assertArrayEquals(golden, s.toBytes(), c.get("name").asText());
            }
        }
    }
}
