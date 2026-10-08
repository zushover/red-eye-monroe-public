package rules

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"weatherbot/internal/domain"
)

var (
	siteRE      = regexp.MustCompile(`(?i)[?&]site=([a-z0-9]{4})(?:&|$)`)
	wunderRE    = regexp.MustCompile(`(?i)/(?:airport/)?([a-z][a-z0-9]{3})(?:[/?]|$)`)
	icaoTextRE  = regexp.MustCompile(`(?i)\b(?:station|airport)\s*\(?([A-Z]{4})\)?\b`)
	descDateRE  = regexp.MustCompile(`(?i)\bon\s+(\d{1,2})\s+([a-z]{3,9})\s+'(\d{2})\b`)
	titleDateRE = regexp.MustCompile(`(?i)\bon\s+([a-z]{3,9})\s+(\d{1,2})\b`)
	numberRE    = regexp.MustCompile(`-?\d+(?:\.\d+)?`)
	rangeRE     = regexp.MustCompile(`(?i)(-?\d+(?:\.\d+)?)\s*(?:-|–|to)\s*(-?\d+(?:\.\d+)?)`)
)

func ParseEvent(e domain.Event) domain.Rule {
	text := e.Description
	if text == "" && len(e.Markets) > 0 {
		text = e.Markets[0].Description
	}
	sourceURL := e.ResolutionSource
	if sourceURL == "" && len(e.Markets) > 0 {
		sourceURL = e.Markets[0].ResolutionSource
	}
	r := domain.Rule{WholeDegree: strings.Contains(strings.ToLower(text), "whole degree"), RevisionExpected: strings.Contains(strings.ToLower(text), "revision")}

	if m := siteRE.FindStringSubmatch(sourceURL); len(m) == 2 {
		r.StationICAO = strings.ToUpper(m[1])
	} else if strings.Contains(strings.ToLower(sourceURL), "wunderground.com") {
		if m := wunderRE.FindAllStringSubmatch(sourceURL, -1); len(m) > 0 {
			r.StationICAO = strings.ToUpper(m[len(m)-1][1])
		}
	} else if m := icaoTextRE.FindStringSubmatch(text); len(m) == 2 {
		r.StationICAO = strings.ToUpper(m[1])
	}

	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "degrees fahrenheit"):
		r.Unit = "F"
	case strings.Contains(lower, "degrees celsius"):
		r.Unit = "C"
	}
	switch {
	case strings.Contains(strings.ToLower(sourceURL), "weather.gov"):
		r.Source = "NOAA"
	case strings.Contains(strings.ToLower(sourceURL), "wunderground.com"):
		r.Source = "WUNDERGROUND"
	default:
		r.Source = "UNKNOWN"
	}

	if m := descDateRE.FindStringSubmatch(text); len(m) == 4 {
		if t, err := time.Parse("2 Jan 06", m[1]+" "+m[2]+" "+m[3]); err == nil {
			r.LocalDate = t.Format("2006-01-02")
		}
	}
	if r.LocalDate == "" {
		year := time.Now().Year()
		if end, err := time.Parse(time.RFC3339, e.EndDate); err == nil {
			year = end.Year()
		}
		if m := titleDateRE.FindStringSubmatch(e.Title); len(m) == 3 {
			if t, err := time.Parse("January 2 2006", m[1]+" "+m[2]+" "+strconv.Itoa(year)); err == nil {
				r.LocalDate = t.Format("2006-01-02")
			}
		}
	}

	if r.StationICAO == "" {
		r.Reasons = append(r.Reasons, "station_not_resolved")
	}
	if r.Unit == "" {
		r.Reasons = append(r.Reasons, "temperature_unit_not_resolved")
	}
	if r.Source == "UNKNOWN" {
		r.Reasons = append(r.Reasons, "unsupported_resolution_source")
	}
	if r.LocalDate == "" {
		r.Reasons = append(r.Reasons, "local_date_not_resolved")
	}
	if !r.WholeDegree {
		r.Reasons = append(r.Reasons, "whole_degree_precision_not_confirmed")
	}
	r.Safe = len(r.Reasons) == 0
	return r
}

func ParseBucket(label string) (domain.Bucket, error) {
	normalized := strings.ToLower(strings.ReplaceAll(label, "−", "-"))
	if match := rangeRE.FindStringSubmatch(normalized); len(match) == 3 {
		lo, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			return domain.Bucket{}, err
		}
		hi, err := strconv.ParseFloat(match[2], 64)
		if err != nil {
			return domain.Bucket{}, err
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		return domain.Bucket{Label: label, Low: &lo, High: &hi}, nil
	}
	values := numberRE.FindAllString(normalized, -1)
	if len(values) == 0 {
		return domain.Bucket{}, fmt.Errorf("no temperature in bucket %q", label)
	}
	nums := make([]float64, 0, len(values))
	for _, raw := range values {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return domain.Bucket{}, err
		}
		nums = append(nums, v)
	}
	b := domain.Bucket{Label: label}
	switch {
	case strings.Contains(normalized, "below") || strings.Contains(normalized, "or lower") || strings.Contains(normalized, "or less"):
		b.High = &nums[0]
	case strings.Contains(normalized, "higher") || strings.Contains(normalized, "or above") || strings.Contains(normalized, "or more"):
		b.Low = &nums[0]
	default:
		b.Low, b.High = &nums[0], &nums[0]
	}
	return b, nil
}

func BucketProbability(b domain.Bucket, probs map[int]float64) float64 {
	var total float64
	for n, p := range probs {
		v := float64(n)
		if b.Low != nil && v < *b.Low {
			continue
		}
		if b.High != nil && v > *b.High {
			continue
		}
		total += p
	}
	return total
}
