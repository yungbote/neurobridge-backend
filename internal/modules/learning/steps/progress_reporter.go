package steps

import (
	"math"
	"sync"
	"time"
)

type progressReporter struct {
	stage       string
	report      func(stage string, pct int, message string)
	minInterval time.Duration
	lastPct     int
	lastMsg     string
	lastAt      time.Time
	mu          sync.Mutex
}

func newProgressReporter(stage string, report func(stage string, pct int, message string), base int, minInterval time.Duration) *progressReporter {
	if minInterval <= 0 {
		minInterval = 2 * time.Second
	}
	if base < 0 {
		base = 0
	}
	if base > 99 {
		base = 99
	}
	return &progressReporter{
		stage:       stage,
		report:      report,
		minInterval: minInterval,
		lastPct:     base,
	}
}

func (p *progressReporter) Update(pct int, msg string) {
	if p == nil || p.report == nil {
		return
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 99 {
		pct = 99
	}
	now := time.Now()
	p.mu.Lock()
	if pct < p.lastPct {
		pct = p.lastPct
	}
	if stringsTrimSpace(msg) == "" {
		msg = p.lastMsg
	}
	if pct == p.lastPct && msg == p.lastMsg && !p.lastAt.IsZero() && now.Sub(p.lastAt) < p.minInterval {
		p.mu.Unlock()
		return
	}
	p.lastPct = pct
	p.lastMsg = msg
	p.lastAt = now
	p.mu.Unlock()
	p.report(p.stage, pct, msg)
}

func (p *progressReporter) UpdateRange(done, total, start, end int, msg string) {
	if p == nil {
		return
	}
	if end < start {
		end = start
	}
	if total <= 0 {
		p.Update(start, msg)
		return
	}
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	span := end - start
	pct := start
	if span > 0 {
		pct = start + int(math.Round(float64(done)/float64(total)*float64(span)))
	}
	p.Update(pct, msg)
}

func stringsTrimSpace(s string) string {
	if s == "" {
		return ""
	}
	i := 0
	j := len(s)
	for i < j {
		if s[i] != ' ' && s[i] != '\n' && s[i] != '\t' && s[i] != '\r' {
			break
		}
		i++
	}
	for j > i {
		b := s[j-1]
		if b != ' ' && b != '\n' && b != '\t' && b != '\r' {
			break
		}
		j--
	}
	if i == 0 && j == len(s) {
		return s
	}
	if j <= i {
		return ""
	}
	return s[i:j]
}
