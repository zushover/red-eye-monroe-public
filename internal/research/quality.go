package research

import (
	"math"
	"time"
)

type Quality struct {
	State         string   `json:"state"`
	Reasons       []string `json:"reasons"`
	Spread        *float64 `json:"spread_c,omitempty"`
	Revision      *float64 `json:"revision_c,omitempty"`
	Age           *float64 `json:"observation_age_minutes,omitempty"`
	ExpectedHours int      `json:"expected_hours"`
	StableRounds  int      `json:"stable_rounds"`
}

func maximum(points []Point) float64 {
	m := math.Inf(-1)
	for _, p := range points {
		if p.C > m {
			m = p.C
		}
	}
	return m
}
func validRun(r Run, start, end, now time.Time) bool {
	if r.Error != "" || r.Received.IsZero() || r.Received.After(now) || now.Sub(r.Received) > 30*time.Minute {
		return false
	}
	seen := map[int64]bool{}
	for _, p := range r.Points {
		if math.IsNaN(p.C) || math.IsInf(p.C, 0) || p.At.Before(start) || !p.At.Before(end) {
			return false
		}
		seen[p.At.Unix()] = true
	}
	for t := start; t.Before(end); t = t.Add(time.Hour) {
		if !seen[t.Unix()] {
			return false
		}
	}
	return true
}
func Assess(icao, zone string, day Day, history []Snapshot, now time.Time) Quality {
	q := Quality{State: "数据不足", Reasons: []string{}}
	loc, e := time.LoadLocation(zone)
	if e != nil {
		q.Reasons = append(q.Reasons, "时区无效")
		return q
	}
	start, e := time.ParseInLocation("2006-01-02", day.Date, loc)
	if e != nil {
		q.Reasons = append(q.Reasons, "日期无效")
		return q
	}
	end := start.AddDate(0, 0, 1)
	q.ExpectedHours = int(end.Sub(start).Hours())
	if len(day.Runs) != 2 {
		q.Reasons = append(q.Reasons, "需要两个模式")
	}
	for _, r := range day.Runs {
		if !validRun(r, start, end, now) {
			q.Reasons = append(q.Reasons, r.Model+" 预报缺测或过期")
		}
	}
	if now.In(loc).Format("2006-01-02") == day.Date {
		if len(day.Observed) == 0 {
			q.Reasons = append(q.Reasons, "今天尚无有效实况")
		} else {
			latest := day.Observed[0].At
			for _, p := range day.Observed {
				if p.At.After(latest) {
					latest = p.At
				}
			}
			age := now.Sub(latest).Minutes()
			q.Age = &age
			if age > 90 || age < 0 {
				q.Reasons = append(q.Reasons, "实况超过90分钟或时间异常")
			}
		}
	}
	if len(q.Reasons) > 0 {
		return q
	}
	spread := math.Abs(maximum(day.Runs[0].Points) - maximum(day.Runs[1].Points))
	q.Spread = &spread
	q.State = "继续观察"
	q.Reasons = append(q.Reasons, "尚未具备连续三轮稳定证据")
	stable := 1
	var oldest time.Time
	for i := len(history) - 1; i >= 0 && stable < 3; i-- {
		h := history[i]
		if !h.At.Before(now) || now.Sub(h.At) > 45*time.Minute {
			continue
		}
		var prior *Day
		for _, s := range h.Sites {
			if s.ICAO == icao {
				for j := range s.Days {
					if s.Days[j].Date == day.Date {
						d := s.Days[j]
						prior = &d
						break
					}
				}
			}
		}
		if prior == nil || len(prior.Runs) != 2 {
			break
		}
		delta := 0.0
		matched := 0
		for _, r := range day.Runs {
			for _, p := range prior.Runs {
				if p.Model == r.Model && validRun(p, start, end, h.At) {
					matched++
					delta = math.Max(delta, math.Abs(maximum(r.Points)-maximum(p.Points)))
				}
			}
		}
		if matched != 2 {
			break
		}
		if q.Revision == nil {
			v := delta
			q.Revision = &v
		}
		if delta > .3 {
			break
		}
		stable++
		oldest = h.At
	}
	q.StableRounds = stable
	if spread >= 1.5 || (q.Revision != nil && *q.Revision >= .5) {
		q.State = "变化明显"
		q.Reasons = []string{"模式峰值差异≥1.5°C 或较上一轮变化≥0.5°C"}
	} else if stable >= 3 && now.Sub(oldest) >= 20*time.Minute && spread < .8 {
		q.State = "趋于稳定"
		q.Reasons = []string{"三轮峰值变化≤0.3°C、模式差异<0.8°C；仅代表短期输出稳定"}
	}
	return q
}
