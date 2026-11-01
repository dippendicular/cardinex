import httpx


class CardinexClient:
    def __init__(self, base_url: str):
        self.base_url = base_url.rstrip("/")
        self.client = httpx.Client()

    def create(self, sketch_type: str, **params):
        r = self.client.post(f"{self.base_url}/sketches/{sketch_type}", json=params)
        r.raise_for_status()
        return r.json()

    def add(self, sketch_id: str, item: str):
        r = self.client.post(f"{self.base_url}/sketches/{sketch_id}/add", json={"item": item})
        r.raise_for_status()
        return r.json()

    def query(self, sketch_id: str, item: str = ""):
        r = self.client.get(f"{self.base_url}/sketches/{sketch_id}/query", params={"item": item})
        r.raise_for_status()
        return r.json()

    def merge(self, into: str, from_id: str):
        r = self.client.post(f"{self.base_url}/sketches/merge", json={"into": into, "from": from_id})
        r.raise_for_status()
        return r.json()

    def stats(self, sketch_id: str):
        r = self.client.get(f"{self.base_url}/sketches/{sketch_id}/stats")
        r.raise_for_status()
        return r.json()

    def delete(self, sketch_id: str):
        r = self.client.delete(f"{self.base_url}/sketches/{sketch_id}")
        r.raise_for_status()
