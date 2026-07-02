package etl

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func (p *Processor) enrichSkuTier(ctx context.Context, tx *sql.Tx, provider, skuID, skuPriceID string) error {
	engine, err := loadTierRulesEngine()
	if err != nil {
		return err
	}
	var serviceName, skuMeter, skuPriceIDVal sql.NullString
	q := `SELECT service_name, sku_meter, sku_price_id FROM dim_sku WHERE provider = ? AND sku_id = ? AND IFNULL(sku_price_id,'') = ?`
	if p.Dialect == "sqlserver" {
		q = `SELECT service_name, sku_meter, sku_price_id FROM dim_sku WHERE provider = @p1 AND sku_id = @p2 AND ISNULL(sku_price_id,'') = ISNULL(@p3,'')`
	}
	if err := tx.QueryRowContext(ctx, p.q(q), provider, skuID, skuPriceID).Scan(&serviceName, &skuMeter, &skuPriceIDVal); err != nil {
		return err
	}
	match, ok := engine.matchSKU(provider, serviceName.String, skuPriceIDVal.String, skuMeter.String)
	updateSQL := `UPDATE dim_sku SET tier_code = ?, tier_rank = ?, is_tier_meter = ? WHERE provider = ? AND sku_id = ? AND IFNULL(sku_price_id,'') = ?`
	if p.Dialect == "sqlserver" {
		updateSQL = `UPDATE dim_sku SET tier_code = @p1, tier_rank = @p2, is_tier_meter = @p3 WHERE provider = @p4 AND sku_id = @p5 AND ISNULL(sku_price_id,'') = ISNULL(@p6,'')`
	}
	if !ok {
		_, err := tx.ExecContext(ctx, p.q(updateSQL), nil, nil, 0, provider, skuID, skuPriceID)
		return err
	}
	_, err = tx.ExecContext(ctx, p.q(updateSQL), match.TierCode, match.TierRank, 1, provider, skuID, skuPriceID)
	return err
}

func (p *Processor) enrichAllSkuTiers(ctx context.Context, tx *sql.Tx) error {
	if err := p.backfillSkuServiceNames(ctx, tx); err != nil {
		return fmt.Errorf("backfill sku service names: %w", err)
	}
	if err := p.repairSkuTierFlags(ctx, tx); err != nil {
		return fmt.Errorf("repair sku tier flags: %w", err)
	}
	serviceBySKU, err := p.loadDominantServiceNameBySKU(ctx, tx)
	if err != nil {
		return fmt.Errorf("load sku service names: %w", err)
	}
	engine, err := loadTierRulesEngine()
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT sku_sk, provider, service_name, sku_price_id, sku_meter FROM dim_sku`)
	if err != nil {
		return err
	}
	defer rows.Close()

	updateSQL := `UPDATE dim_sku SET tier_code = ?, tier_rank = ?, is_tier_meter = ? WHERE sku_sk = ?`
	if p.Dialect == "sqlserver" {
		updateSQL = `UPDATE dim_sku SET tier_code = @p1, tier_rank = @p2, is_tier_meter = @p3 WHERE sku_sk = @p4`
	}

	for rows.Next() {
		var sk int64
		var provider string
		var serviceName, skuPriceID, skuMeter sql.NullString
		if err := rows.Scan(&sk, &provider, &serviceName, &skuPriceID, &skuMeter); err != nil {
			return err
		}
		svcName := serviceName.String
		if dominant := strings.TrimSpace(serviceBySKU[sk]); dominant != "" && (svcName == "" || strings.EqualFold(svcName, "UNKNOWN")) {
			svcName = dominant
		}
		match, ok := engine.matchSKU(provider, svcName, skuPriceID.String, skuMeter.String)
		if !ok {
			if _, err := tx.ExecContext(ctx, p.q(updateSQL), nil, nil, 0, sk); err != nil {
				return fmt.Errorf("sku_sk %d: %w", sk, err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, p.q(updateSQL), match.TierCode, match.TierRank, 1, sk); err != nil {
			return fmt.Errorf("sku_sk %d: %w", sk, err)
		}
	}
	return rows.Err()
}

func (p *Processor) backfillSkuServiceNames(ctx context.Context, tx *sql.Tx) error {
	if p.Dialect == "sqlserver" {
		_, err := tx.ExecContext(ctx, `
			UPDATE s SET service_name = d.service_name
			FROM dim_sku s
			INNER JOIN (
				SELECT f.sku_sk, svc.service_name,
					ROW_NUMBER() OVER (PARTITION BY f.sku_sk ORDER BY COUNT(*) DESC, svc.service_name) AS rn
				FROM fact_focus_cost_daily f
				INNER JOIN dim_service svc ON f.service_sk = svc.service_sk
				WHERE svc.service_name IS NOT NULL AND LTRIM(RTRIM(svc.service_name)) <> ''
				GROUP BY f.sku_sk, svc.service_name
			) d ON s.sku_sk = d.sku_sk AND d.rn = 1
			WHERE s.service_name IS NULL OR LTRIM(RTRIM(s.service_name)) = '' OR LTRIM(RTRIM(s.service_name)) = 'UNKNOWN'`)
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE dim_sku SET service_name = (
			SELECT svc.service_name
			FROM fact_focus_cost_daily f
			INNER JOIN dim_service svc ON f.service_sk = svc.service_sk
			WHERE f.sku_sk = dim_sku.sku_sk
			  AND svc.service_name IS NOT NULL AND TRIM(svc.service_name) <> ''
			GROUP BY svc.service_name
			ORDER BY COUNT(*) DESC, svc.service_name
			LIMIT 1
		)
		WHERE service_name IS NULL OR TRIM(service_name) = '' OR TRIM(service_name) = 'UNKNOWN'`)
	return err
}

func (p *Processor) repairSkuTierFlags(ctx context.Context, tx *sql.Tx) error {
	q := `UPDATE dim_sku SET is_tier_meter = 1 WHERE tier_code IS NOT NULL AND TRIM(tier_code) <> '' AND IFNULL(is_tier_meter, 0) = 0`
	if p.Dialect == "sqlserver" {
		q = `UPDATE dim_sku SET is_tier_meter = 1 WHERE tier_code IS NOT NULL AND LTRIM(RTRIM(tier_code)) <> '' AND ISNULL(is_tier_meter, 0) = 0`
	}
	_, err := tx.ExecContext(ctx, q)
	return err
}

func (p *Processor) loadDominantServiceNameBySKU(ctx context.Context, tx *sql.Tx) (map[int64]string, error) {
	out := map[int64]string{}
	var q string
	if p.Dialect == "sqlserver" {
		q = `
			SELECT sku_sk, service_name FROM (
				SELECT f.sku_sk, svc.service_name,
					ROW_NUMBER() OVER (PARTITION BY f.sku_sk ORDER BY COUNT(*) DESC, svc.service_name) AS rn
				FROM fact_focus_cost_daily f
				INNER JOIN dim_service svc ON f.service_sk = svc.service_sk
				WHERE f.sku_sk IS NOT NULL
				GROUP BY f.sku_sk, svc.service_name
			) x WHERE rn = 1`
	} else {
		q = `
			SELECT f.sku_sk, svc.service_name
			FROM fact_focus_cost_daily f
			INNER JOIN dim_service svc ON f.service_sk = svc.service_sk
			WHERE f.sku_sk IS NOT NULL
			GROUP BY f.sku_sk, svc.service_name
			HAVING COUNT(*) = (
				SELECT MAX(cnt) FROM (
					SELECT COUNT(*) AS cnt
					FROM fact_focus_cost_daily f2
					WHERE f2.sku_sk = f.sku_sk
					GROUP BY f2.service_sk
				)
			)`
	}
	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sk int64
		var name string
		if err := rows.Scan(&sk, &name); err != nil {
			return nil, err
		}
		if strings.TrimSpace(name) != "" {
			out[sk] = strings.TrimSpace(name)
		}
	}
	return out, rows.Err()
}
