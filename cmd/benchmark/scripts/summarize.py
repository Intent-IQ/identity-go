#!/usr/bin/env python3
"""Print a summary of k6 results and benchmark-server metrics."""
import json
import pathlib
import re
import sys


def prometheus(path):
    rows = []
    for line in path.read_text().splitlines():
        if not line or line.startswith("#"):
            continue
        series, _, value = line.rpartition(" ")
        name, _, rest = series.partition("{")
        labels = {}
        for pair in re.findall(r'(\w+)="((?:[^"\\]|\\.)*)"', rest):
            labels[pair[0]] = pair[1]
        rows.append((name.strip(), labels, float(value)))
    return rows


def total(rows, name, **match):
    return sum(
        value
        for series, labels, value in rows
        if series == name and all(labels.get(key) == want for key, want in match.items())
    )


def k6_metric(summary, name, field):
    metric = summary.get("metrics", {}).get(name)
    if not metric:
        return None
    values = metric.get("values", metric)
    return values.get(field)


def main(root):
    directories = sorted(p for p in pathlib.Path(root).iterdir() if p.is_dir())
    header = ("case", "requests", "enriched", "enriched %", "delivered", "abandoned",
              "expired", "timeouts", "p50", "p90", "p99", "budget", "cache hits")
    widths = (16, 9, 9, 11, 10, 10, 8, 9, 8, 8, 8, 9, 11)
    print("".join(f"{name:>{width}}" for name, width in zip(header, widths)))

    for directory in directories:
        scrape = directory / "metrics.txt"
        export = directory / "k6.json"
        if not scrape.exists() or not export.exists():
            continue
        rows = prometheus(scrape)
        summary = json.loads(export.read_text())
        requests = total(rows, "iiq_identity_requests_total")
        enriched = total(rows, "iiq_identity_enriched_total")
        expired = total(rows, "iiq_identity_not_enriched_total", reason="wait_expired")
        timeouts = total(rows, "iiq_identity_api_error_total", reason="timeout")
        hits = total(rows, "iiq_identity_cache_lookup_total", result="hit")
        row = (
            directory.name,
            f"{requests:.0f}",
            f"{enriched:.0f}",
            f"{(enriched / requests * 100) if requests else 0:.1f}%",
            f"{k6_metric(summary, 'enrichment_delivered', 'count') or 0:.0f}",
            f"{k6_metric(summary, 'enrichment_hook_abandoned', 'count') or 0:.0f}",
            f"{expired:.0f}",
            f"{timeouts:.0f}",
            f"{k6_metric(summary, 'http_req_duration', 'med') or 0:.0f}",
            f"{k6_metric(summary, 'http_req_duration', 'p(90)') or 0:.0f}",
            f"{k6_metric(summary, 'http_req_duration', 'p(99)') or 0:.0f}",
            f"{k6_metric(summary, 'enrichment_auction_budget', 'avg') or 0:.0f}",
            f"{hits:.0f}",
        )
        print("".join(f"{value:>{width}}" for value, width in zip(row, widths)))


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else "./results")
