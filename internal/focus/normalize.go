package focus

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

var providerMap = map[string]string{
	"AWS":           "AWS",
	"Amazon Web Services": "AWS",
	"Microsoft":     "AZURE",
	"Azure":         "AZURE",
	"Google Cloud":  "GCP",
	"GCP":           "GCP",
	"Google":        "GCP",
}

func NormalizeProvider(raw string) string {
	if raw == "" {
		return ""
	}
	if p, ok := providerMap[strings.TrimSpace(raw)]; ok {
		return p
	}
	return ""
}

func NormalizeChargeCategory(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "usage":
		return "Usage"
	case "purchase":
		return "Purchase"
	case "tax":
		return "Tax"
	case "credit":
		return "Credit"
	case "adjustment":
		return "Adjustment"
	default:
		return strings.TrimSpace(raw)
	}
}

func NormalizePricingCategory(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "standard":
		return "Standard"
	case "committed":
		return "Committed"
	case "dynamic":
		return "Dynamic"
	case "":
		return ""
	default:
		return "Other"
	}
}

func ChargeDescriptionHash(desc string) string {
	sum := sha256.Sum256([]byte(desc))
	return hex.EncodeToString(sum[:])
}

func CoalesceServiceName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "UNKNOWN"
	}
	return name
}

func PtrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func EmptyToNil(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "NULL") {
		return nil
	}
	return &s
}

func DateOnly(ts string) string {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return ""
	}
	if i := strings.IndexByte(ts, 'T'); i >= 0 {
		return ts[:i]
	}
	if i := strings.IndexByte(ts, ' '); i >= 0 {
		return ts[:i]
	}
	return ts
}

func MonthStart(date string) string {
	date = DateOnly(date)
	if len(date) < 7 {
		return date
	}
	return date[:7] + "-01"
}
