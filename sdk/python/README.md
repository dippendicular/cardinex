# cardinex (Python)

```python
import numpy as np, cardinex

with cardinex.Semantic(dim=384, tau=0.6, seed=1, capacity=4096) as s:
    s.add_batch(np.random.randn(1000, 384).astype(np.float32), values=np.random.rand(1000))
    print(s.topk(5, by="mean"))
```

`cardinex.http` is a client for the hosted HTTP service.
