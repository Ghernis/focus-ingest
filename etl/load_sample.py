#!/usr/bin/env python3
"""
Load FOCUS 1.0 sample CSV into staging column layout and emit validation report.
Use with pyodbc against Azure SQL, or run --dry-run locally without a database.

  pip install pyodbc   # optional, for DB load
  python etl/load_sample.py --dry-run
  python etl/load_sample.py --connection "Driver={ODBC Driver 18 for SQL Server};..."
"""

from __future__ import annotations

import argparse
import csv
import hashlib
import json
import sys
from collections import Counter, defaultdict
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_CSV = ROOT / "focus_sample_100000.csv" / "focus_sample_100000.csv"

FOCUS_10_TO_STG = {
    "ProviderName": "Provider",
    "InvoiceIssuerName": "InvoiceIssuer",
    "PublisherName": "Publisher",
    "Tags": "raw_tags_json",
    "Id": "x_source_row_id",
}

PROVIDER_MAP = {
    "AWS": "AWS",
    "Microsoft": "AZURE",
    "Google Cloud": "GCP",
    "Oracle": None,
}


def normalize_provider(name: str | None) -> str | None:
    if not name:
        return None
    return PROVIDER_MAP.get(name.strip())


def map_row(row: dict) -> dict:
    out: dict = {}
    for k, v in row.items():
        target = FOCUS_10_TO_STG.get(k, k)
        if v in ("", "NULL", None):
            out[target] = None
        else:
            out[target] = v
    if out.get("Provider"):
        out["source_provider"] = normalize_provider(out["Provider"])
    return out


def charge_description_hash(desc: str | None) -> str:
    payload = (desc or "").encode("utf-8")
    return hashlib.sha256(payload).hexdigest()


def parse_date(s: str | None) -> str | None:
    if not s:
        return None
    return s.split()[0]


def dry_run_validate(rows: list[dict]) -> dict:
    providers = Counter()
    charge_categories = Counter()
    commitment_status = Counter()
    commitment_types = Counter()
    tag_keys = Counter()
    daily_keys: set[tuple] = set()
    skipped_oracle = 0

    for row in rows:
        mapped = map_row(row)
        prov = mapped.get("source_provider")
        if prov is None:
            skipped_oracle += 1
            continue
        providers[prov] += 1
        charge_categories[mapped.get("ChargeCategory") or "NULL"] += 1
        if mapped.get("CommitmentDiscountStatus"):
            commitment_status[mapped["CommitmentDiscountStatus"]] += 1
        if mapped.get("CommitmentDiscountType"):
            commitment_types[mapped["CommitmentDiscountType"]] += 1
        tags = mapped.get("raw_tags_json")
        if tags:
            try:
                for k in json.loads(tags):
                    tag_keys[k] += 1
            except json.JSONDecodeError:
                pass
        charge_date = parse_date(mapped.get("ChargePeriodStart"))
        if charge_date and mapped.get("BillingAccountId"):
            grain = (
                charge_date,
                prov,
                mapped.get("BillingAccountId"),
                mapped.get("SubAccountId"),
                mapped.get("ResourceId"),
                mapped.get("ServiceName"),
                mapped.get("ChargeCategory"),
                charge_description_hash(mapped.get("ChargeDescription")),
            )
            daily_keys.add(grain)

    return {
        "source_rows": len(rows),
        "rows_after_provider_filter": sum(providers.values()),
        "skipped_oracle": skipped_oracle,
        "providers": dict(providers),
        "charge_categories": dict(charge_categories),
        "commitment_status": dict(commitment_status),
        "commitment_types": dict(commitment_types),
        "distinct_tag_keys": len(tag_keys),
        "top_tag_keys": tag_keys.most_common(10),
        "estimated_daily_fact_rows": len(daily_keys),
        "commitment_rows": sum(commitment_status.values()),
    }


def read_csv(path: Path) -> list[dict]:
    with path.open(newline="", encoding="utf-8") as f:
        return list(csv.DictReader(f))


def build_insert_batch(rows: list[dict], batch_id: int, source_file: str, focus_version: str) -> list[tuple]:
    """Build tuples for parameterized INSERT into stg_focus_cost_line."""
    stg_columns = [
        "ingestion_batch_id", "focus_version", "source_file", "x_source_row_id",
        "AvailabilityZone", "BilledCost", "BillingAccountId", "BillingAccountName",
        "BillingCurrency", "BillingPeriodEnd", "BillingPeriodStart",
        "ChargeCategory", "ChargeClass", "ChargeDescription", "ChargeFrequency",
        "ChargePeriodEnd", "ChargePeriodStart",
        "CommitmentDiscountCategory", "CommitmentDiscountId", "CommitmentDiscountName",
        "CommitmentDiscountStatus", "CommitmentDiscountType",
        "ConsumedQuantity", "ConsumedUnit", "ContractedCost", "ContractedUnitPrice", "EffectiveCost",
        "InvoiceIssuer", "ListCost", "ListUnitPrice", "PricingCategory", "PricingQuantity", "PricingUnit",
        "Provider", "Publisher", "RegionId", "RegionName", "ResourceId", "ResourceName", "ResourceType",
        "ServiceCategory", "ServiceName", "SkuId", "SkuPriceId", "SubAccountId", "SubAccountName",
        "raw_tags_json", "source_provider",
    ]
    inserts = []
    for row in rows:
        m = map_row(row)
        if m.get("source_provider") is None:
            continue
        values = {
            "ingestion_batch_id": batch_id,
            "focus_version": focus_version,
            "source_file": source_file,
            "x_source_row_id": m.get("x_source_row_id"),
            "AvailabilityZone": m.get("AvailabilityZone"),
            "BilledCost": m.get("BilledCost"),
            "BillingAccountId": m.get("BillingAccountId"),
            "BillingAccountName": m.get("BillingAccountName"),
            "BillingCurrency": m.get("BillingCurrency"),
            "BillingPeriodEnd": m.get("BillingPeriodEnd"),
            "BillingPeriodStart": m.get("BillingPeriodStart"),
            "ChargeCategory": m.get("ChargeCategory"),
            "ChargeClass": m.get("ChargeClass"),
            "ChargeDescription": m.get("ChargeDescription"),
            "ChargeFrequency": m.get("ChargeFrequency"),
            "ChargePeriodEnd": m.get("ChargePeriodEnd"),
            "ChargePeriodStart": m.get("ChargePeriodStart"),
            "CommitmentDiscountCategory": m.get("CommitmentDiscountCategory"),
            "CommitmentDiscountId": m.get("CommitmentDiscountId"),
            "CommitmentDiscountName": m.get("CommitmentDiscountName"),
            "CommitmentDiscountStatus": m.get("CommitmentDiscountStatus"),
            "CommitmentDiscountType": m.get("CommitmentDiscountType"),
            "ConsumedQuantity": m.get("ConsumedQuantity"),
            "ConsumedUnit": m.get("ConsumedUnit"),
            "ContractedCost": m.get("ContractedCost"),
            "ContractedUnitPrice": m.get("ContractedUnitPrice"),
            "EffectiveCost": m.get("EffectiveCost"),
            "InvoiceIssuer": m.get("InvoiceIssuer"),
            "ListCost": m.get("ListCost"),
            "ListUnitPrice": m.get("ListUnitPrice"),
            "PricingCategory": m.get("PricingCategory"),
            "PricingQuantity": m.get("PricingQuantity"),
            "PricingUnit": m.get("PricingUnit"),
            "Provider": m.get("Provider"),
            "Publisher": m.get("Publisher"),
            "RegionId": m.get("RegionId"),
            "RegionName": m.get("RegionName"),
            "ResourceId": m.get("ResourceId"),
            "ResourceName": m.get("ResourceName"),
            "ResourceType": m.get("ResourceType"),
            "ServiceCategory": m.get("ServiceCategory"),
            "ServiceName": m.get("ServiceName"),
            "SkuId": m.get("SkuId"),
            "SkuPriceId": m.get("SkuPriceId"),
            "SubAccountId": m.get("SubAccountId"),
            "SubAccountName": m.get("SubAccountName"),
            "raw_tags_json": m.get("raw_tags_json"),
            "source_provider": m.get("source_provider"),
        }
        inserts.append(tuple(values[c] for c in stg_columns))
    return inserts


def load_to_sql(connection_string: str, csv_path: Path, focus_version: str) -> int:
    import pyodbc  # type: ignore

    rows = read_csv(csv_path)
    inserts = build_insert_batch(rows, batch_id=0, source_file=csv_path.name, focus_version=focus_version)
    if not inserts:
        raise SystemExit("No rows to load after provider filter")

    cols = [
        "ingestion_batch_id", "focus_version", "source_file", "x_source_row_id",
        "AvailabilityZone", "BilledCost", "BillingAccountId", "BillingAccountName",
        "BillingCurrency", "BillingPeriodEnd", "BillingPeriodStart",
        "ChargeCategory", "ChargeClass", "ChargeDescription", "ChargeFrequency",
        "ChargePeriodEnd", "ChargePeriodStart",
        "CommitmentDiscountCategory", "CommitmentDiscountId", "CommitmentDiscountName",
        "CommitmentDiscountStatus", "CommitmentDiscountType",
        "ConsumedQuantity", "ConsumedUnit", "ContractedCost", "ContractedUnitPrice", "EffectiveCost",
        "InvoiceIssuer", "ListCost", "ListUnitPrice", "PricingCategory", "PricingQuantity", "PricingUnit",
        "Provider", "Publisher", "RegionId", "RegionName", "ResourceId", "ResourceName", "ResourceType",
        "ServiceCategory", "ServiceName", "SkuId", "SkuPriceId", "SubAccountId", "SubAccountName",
        "raw_tags_json", "source_provider",
    ]
    placeholders = ", ".join("?" for _ in cols)
    col_list = ", ".join(cols)

    with pyodbc.connect(connection_string) as conn:
        cur = conn.cursor()
        cur.execute(
            "INSERT INTO dbo.dim_ingestion_batch (source_provider, focus_version, source_file, status) "
            "OUTPUT INSERTED.ingestion_batch_id VALUES (?, ?, ?, 'LOADING')",
            ("MIXED", focus_version, csv_path.name),
        )
        batch_id = cur.fetchone()[0]
        sql = f"INSERT INTO dbo.stg_focus_cost_line ({col_list}) VALUES ({placeholders})"
        batch_size = 1000
        for i in range(0, len(inserts), batch_size):
            fixed = []
            for t in inserts[i : i + batch_size]:
                row = list(t)
                row[0] = batch_id
                fixed.append(tuple(row))
            cur.fast_executemany = True
            cur.executemany(sql, fixed)
        cur.execute(
            "UPDATE dbo.dim_ingestion_batch SET row_count = ?, status = 'LOADED' WHERE ingestion_batch_id = ?",
            (len(inserts), batch_id),
        )
        conn.commit()
    return batch_id


def main() -> None:
    parser = argparse.ArgumentParser(description="Load FOCUS sample CSV into staging")
    parser.add_argument("--csv", type=Path, default=DEFAULT_CSV)
    parser.add_argument("--dry-run", action="store_true", help="Validate locally without database")
    parser.add_argument("--connection", help="ODBC connection string for Azure SQL")
    parser.add_argument("--focus-version", default="1.0")
    args = parser.parse_args()

    if not args.csv.exists():
        print(f"CSV not found: {args.csv}", file=sys.stderr)
        sys.exit(1)

    rows = read_csv(args.csv)
    report = dry_run_validate(rows)

    print("=== FOCUS Sample Validation Report ===")
    print(f"CSV: {args.csv}")
    print(f"Generated: {datetime.now(timezone.utc).isoformat()}")
    for k, v in report.items():
        print(f"  {k}: {v}")

    if args.dry_run:
        print("\nDry run complete. Expected commitment rows:", report["commitment_rows"])
        print("Run focus_dw.sql, then load with --connection, then etl/02_migrate_dims_and_daily.sql")
        return

    if not args.connection:
        print("Provide --connection or use --dry-run", file=sys.stderr)
        sys.exit(1)

    batch_id = load_to_sql(args.connection, args.csv, args.focus_version)
    print(f"Loaded batch_id={batch_id}. Run etl/02_migrate_dims_and_daily.sql with @IngestionBatchId = {batch_id}")


if __name__ == "__main__":
    main()
