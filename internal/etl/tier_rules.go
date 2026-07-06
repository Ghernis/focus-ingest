package etl

import (
	_ "embed"
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ghernis/focus_dt/internal/focus"
)

//go:embed tier_rules.json
var tierRulesJSON []byte

const (
	tierRankNumericSuffix = "numeric_suffix"
	tierRankLookup        = "lookup"
	tierRankVCoreCount    = "vcore_count"
)

type tierRuleDef struct {
	Provider         string `json:"provider"`
	ServiceName      string `json:"service_name"`
	LineMatchRegex   string `json:"line_match_regex"`
	TierExtractRegex string `json:"tier_extract_regex"`
	TierRankMode     string `json:"tier_rank_mode"`
	Priority         int    `json:"priority"`
}

type tierRankDef struct {
	Provider    string `json:"provider"`
	ServiceName string `json:"service_name"`
	TierCode    string `json:"tier_code"`
	TierRank    int    `json:"tier_rank"`
}

type tierRulesConfig struct {
	Rules []tierRuleDef `json:"rules"`
	Ranks []tierRankDef `json:"ranks"`
}

type compiledTierRule struct {
	tierRuleDef
	lineMatch   *regexp.Regexp
	tierExtract *regexp.Regexp
}

type tierRulesEngine struct {
	rules      []compiledTierRule
	rankLookup map[string]int
}

type skuTierMatch struct {
	TierCode string
	TierRank int
}

var defaultTierEngine *tierRulesEngine

// loadTierRulesEngine loads embedded tier rules. Tests may call resetTierRulesEngine() after mutating rules.
func loadTierRulesEngine() (*tierRulesEngine, error) {
	if defaultTierEngine != nil {
		return defaultTierEngine, nil
	}
	return reloadTierRulesEngine()
}

func reloadTierRulesEngine() (*tierRulesEngine, error) {
	var cfg tierRulesConfig
	if err := json.Unmarshal(tierRulesJSON, &cfg); err != nil {
		return nil, err
	}
	engine := &tierRulesEngine{rankLookup: map[string]int{}}
	for _, r := range cfg.Rules {
		lineRe, err := regexp.Compile(r.LineMatchRegex)
		if err != nil {
			return nil, err
		}
		tierRe, err := regexp.Compile(r.TierExtractRegex)
		if err != nil {
			return nil, err
		}
		engine.rules = append(engine.rules, compiledTierRule{
			tierRuleDef: r,
			lineMatch:   lineRe,
			tierExtract: tierRe,
		})
	}
	sort.Slice(engine.rules, func(i, j int) bool {
		return engine.rules[i].Priority < engine.rules[j].Priority
	})
	for _, r := range cfg.Ranks {
		key := tierRankKey(r.Provider, r.ServiceName, r.TierCode)
		engine.rankLookup[key] = r.TierRank
	}
	defaultTierEngine = engine
	return engine, nil
}

func resetTierRulesEngine() {
	defaultTierEngine = nil
}

func tierRankKey(provider, serviceName, tierCode string) string {
	return strings.ToUpper(strings.TrimSpace(provider)) + "\x1f" +
		strings.TrimSpace(serviceName) + "\x1f" +
		strings.TrimSpace(tierCode)
}

func (e *tierRulesEngine) matchSKU(provider, serviceName, skuPriceID, skuMeter string) (skuTierMatch, bool) {
	provider = normalizeTierProvider(provider)
	if provider == "" {
		return skuTierMatch{}, false
	}
	if match, ok := e.matchSKURules(provider, strings.TrimSpace(serviceName), skuPriceID, skuMeter, true); ok {
		return match, true
	}
	return e.matchSKURules(provider, serviceName, skuPriceID, skuMeter, false)
}

func normalizeTierProvider(raw string) string {
	if p := focus.NormalizeProvider(raw); p != "" {
		return p
	}
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "AZURE", "AWS", "GCP":
		return strings.ToUpper(strings.TrimSpace(raw))
	}
	return ""
}

func (e *tierRulesEngine) matchSKURules(provider, serviceName, skuPriceID, skuMeter string, requireService bool) (skuTierMatch, bool) {
	for _, rule := range e.rules {
		if !strings.EqualFold(rule.Provider, provider) {
			continue
		}
		if requireService && strings.TrimSpace(rule.ServiceName) != serviceName {
			continue
		}
		if !ruleMatchesLine(rule, skuPriceID, skuMeter) {
			continue
		}
		code, ok := extractTierCode(rule.tierExtract, skuMeter)
		if !ok {
			continue
		}
		rank := e.tierRank(rule, code)
		return skuTierMatch{TierCode: code, TierRank: rank}, true
	}
	return skuTierMatch{}, false
}

func ruleMatchesLine(rule compiledTierRule, skuPriceID, skuMeter string) bool {
	price := strings.TrimSpace(skuPriceID)
	meter := strings.TrimSpace(skuMeter)
	candidates := []string{price}
	if meter != "" {
		combined := strings.TrimSpace(price + " " + meter)
		if combined != price {
			candidates = append(candidates, combined)
		}
	}
	for _, lineText := range candidates {
		if rule.lineMatch.MatchString(lineText) {
			return true
		}
	}
	return false
}

func extractTierCode(tierExtract *regexp.Regexp, skuMeter string) (string, bool) {
	meter := strings.TrimSpace(skuMeter)
	if meter == "" {
		return "", false
	}
	candidates := []string{meter}
	if i := strings.LastIndex(meter, "/"); i >= 0 {
		if right := strings.TrimSpace(meter[i+1:]); right != "" {
			candidates = append([]string{right}, candidates...)
		}
	}
	for _, candidate := range candidates {
		sub := tierExtract.FindStringSubmatch(candidate)
		if len(sub) < 2 {
			continue
		}
		code := strings.TrimSpace(sub[1])
		if code != "" {
			return code, true
		}
	}
	return "", false
}

func rankLookupServices(serviceName string) []string {
	s := strings.TrimSpace(serviceName)
	switch s {
	case "Azure Reservations":
		return []string{s, "Virtual Machines", "Azure App Service"}
	case "Virtual Machine Scale Sets":
		return []string{s, "Virtual Machines"}
	default:
		return []string{s}
	}
}

func (e *tierRulesEngine) tierRank(rule compiledTierRule, tierCode string) int {
	for _, svc := range rankLookupServices(rule.ServiceName) {
		if rank, ok := e.rankLookup[tierRankKey(rule.Provider, svc, tierCode)]; ok {
			return rank
		}
	}
	switch rule.TierRankMode {
	case tierRankVCoreCount:
		n, _ := strconv.Atoi(tierCode)
		return n
	case tierRankNumericSuffix:
		return vmNumericTierRank(tierCode)
	default:
		return 0
	}
}

var vmTierRe = regexp.MustCompile(`^([A-Z])(\d+)`)
var vmGenRe = regexp.MustCompile(`v(\d+)`)

func vmNumericTierRank(tierCode string) int {
	m := vmTierRe.FindStringSubmatch(strings.TrimSpace(tierCode))
	if len(m) < 3 {
		return 0
	}
	family := int(m[1][0])
	size, _ := strconv.Atoi(m[2])
	gen := 0
	if gm := vmGenRe.FindStringSubmatch(tierCode); len(gm) > 1 {
		gen, _ = strconv.Atoi(gm[1])
	}
	return family*10000 + size*100 + gen
}

func tierChangeDirection(priorRank, newRank int, priorRate, newRate float64) string {
	if priorRate > 0 && newRate > 0 && math.Abs(priorRate-newRate) > 1e-9 {
		return changeDirection(priorRate, newRate)
	}
	if priorRank > 0 && newRank > 0 && priorRank != newRank {
		if priorRank > newRank {
			return changeDownsize
		}
		return changeUpsize
	}
	return changeNeutral
}
