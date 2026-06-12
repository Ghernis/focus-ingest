package etl

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/ghernis/focus_dt/internal/focus"
)

func (p *Processor) buildTags(ctx context.Context, tx *sql.Tx, batchID int64, rows []normRow) error {
	cache, err := p.loadDimCache(ctx, tx)
	if err != nil {
		return err
	}

	idMap := map[string]int64{}
	idQ := p.q(`
		SELECT cost_daily_id, charge_date, billing_account_sk,
		  IFNULL(sub_account_sk,-1), IFNULL(resource_sk,-1), service_sk, charge_category_sk,
		  charge_description_hash, billing_period_start
		FROM fact_focus_cost_daily WHERE ingestion_batch_id = ?`)
	if p.Dialect == "sqlserver" {
		idQ = `
		SELECT cost_daily_id, charge_date, billing_account_sk,
		  ISNULL(sub_account_sk,-1), ISNULL(resource_sk,-1), service_sk, charge_category_sk,
		  charge_description_hash, billing_period_start
		FROM fact_focus_cost_daily WHERE ingestion_batch_id = @p1`
	}
	idRows, err := tx.QueryContext(ctx, idQ, batchID)
	if err != nil {
		return err
	}
	for idRows.Next() {
		var id, accSK, subSK, resSK, svcSK, catSK int64
		var chargeDate, hash, billStart string
		if err := idRows.Scan(&id, &chargeDate, &accSK, &subSK, &resSK, &svcSK, &catSK, &hash, &billStart); err != nil {
			idRows.Close()
			return err
		}
		key := grainLookupKey(chargeDate, accSK, subSK, resSK, svcSK, catSK, hash, billStart)
		idMap[key] = id
	}
	idRows.Close()

	type tagPair struct {
		CostDailyID int64
		Key         string
		Value       string
	}
	seenTags := map[string]struct{}{}
	var pairs []tagPair

	for _, r := range rows {
		if r.RawTagsJSON == nil || strings.TrimSpace(*r.RawTagsJSON) == "" {
			continue
		}
		var tags map[string]interface{}
		if err := json.Unmarshal([]byte(*r.RawTagsJSON), &tags); err != nil {
			continue
		}
		accSK := cache.account[r.ProviderCode+"|"+focus.PtrStr(r.BillingAccountId)]
		svcSK := cache.service[r.ProviderCode+"|"+r.ServiceCode]
		catSK := cache.chargeCat[r.ChargeCategoryNorm]
		subSK := int64(-1)
		if id := cache.sub[r.ProviderCode+"|"+focus.PtrStr(r.SubAccountId)]; id != 0 {
			subSK = id
		}
		resSK := int64(-1)
		if id := cache.resource[r.ProviderCode+"|"+focus.PtrStr(r.ResourceId)]; id != 0 {
			resSK = id
		}
		key := grainLookupKey(r.ChargeDate, accSK, subSK, resSK, svcSK, catSK, r.ChargeDescriptionHash, r.BillingPeriodStart)
		costID := idMap[key]
		if costID == 0 {
			continue
		}
		for k, v := range tags {
			val := tagValue(v)
			if val == "" {
				continue
			}
			if len(val) > 512 {
				val = val[:512]
			}
			pairs = append(pairs, tagPair{CostDailyID: costID, Key: k, Value: val})
			seenTags[k+"\x00"+val] = struct{}{}
		}
	}

	for k := range seenTags {
		parts := strings.SplitN(k, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		if p.Dialect == "sqlite" {
			nk := parts[0] + "\x00" + parts[1]
			existed, err := p.dimExists(ctx, tx, "dim_tag", nk)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO dim_tag (tag_key, tag_value) VALUES (?, ?)`, parts[0], parts[1]); err != nil {
				return err
			}
			if p.TrackPendingDims && !existed {
				var sk int64
				if err := tx.QueryRowContext(ctx, `SELECT tag_sk FROM dim_tag WHERE tag_key = ? AND tag_value = ?`, parts[0], parts[1]).Scan(&sk); err == nil {
					_ = p.recordPendingDim(ctx, tx, "dim_tag", nk, sk)
				}
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
				IF NOT EXISTS (SELECT 1 FROM dim_tag WHERE tag_key = @p1 AND tag_value = @p2)
				  INSERT INTO dim_tag (tag_key, tag_value) VALUES (@p1, @p2)`, parts[0], parts[1]); err != nil {
				return err
			}
		}
	}

	tagSKs, err := p.loadTagSKMap(ctx, tx)
	if err != nil {
		return err
	}

	var bridgeStmt *sql.Stmt
	if p.Dialect == "sqlite" {
		bridgeStmt, err = tx.PrepareContext(ctx, `INSERT OR IGNORE INTO bridge_cost_tag (cost_daily_id, tag_sk) VALUES (?, ?)`)
	} else {
		bridgeStmt, err = tx.PrepareContext(ctx, `
			IF NOT EXISTS (SELECT 1 FROM bridge_cost_tag WHERE cost_daily_id = @p1 AND tag_sk = @p2)
			  INSERT INTO bridge_cost_tag (cost_daily_id, tag_sk) VALUES (@p1, @p2)`)
	}
	if err != nil {
		return err
	}
	defer bridgeStmt.Close()

	for _, pair := range pairs {
		tagSK := tagSKs[pair.Key+"\x00"+pair.Value]
		if tagSK == 0 {
			continue
		}
		if _, err := bridgeStmt.ExecContext(ctx, pair.CostDailyID, tagSK); err != nil {
			return err
		}
	}
	return nil
}

func (p *Processor) loadTagSKMap(ctx context.Context, tx *sql.Tx) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT tag_key, tag_value, tag_sk FROM dim_tag`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]int64{}
	for rows.Next() {
		var key, val string
		var sk int64
		if err := rows.Scan(&key, &val, &sk); err != nil {
			return nil, err
		}
		m[key+"\x00"+val] = sk
	}
	return m, rows.Err()
}

// GrainLookupKey matches fact rows to staging tag rows (subset of full fact grain).
func GrainLookupKey(chargeDate string, accSK, subSK, resSK, svcSK, catSK int64, hash, billStart string) string {
	return grainLookupKey(chargeDate, accSK, subSK, resSK, svcSK, catSK, hash, billStart)
}

func grainLookupKey(chargeDate string, accSK, subSK, resSK, svcSK, catSK int64, hash, billStart string) string {
	return strings.Join([]string{
		chargeDate,
		strconv.FormatInt(accSK, 10),
		strconv.FormatInt(subSK, 10),
		strconv.FormatInt(resSK, 10),
		strconv.FormatInt(svcSK, 10),
		strconv.FormatInt(catSK, 10),
		hash, billStart,
	}, "|")
}

func tagValue(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
}

var _ = sql.ErrNoRows
