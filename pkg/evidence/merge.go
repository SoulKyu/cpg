// pkg/evidence/merge.go
package evidence

import "sort"

// MergeCaps bounds the size of the merged evidence file.
type MergeCaps struct {
	MaxSamples  int // samples kept per rule (newest by time)
	MaxSessions int // sessions kept in total (newest by StartedAt)
}

// Merge folds a new session and its rules into an existing PolicyEvidence
// document in place. Rule identity is the Key field. Samples and sessions are
// capped FIFO by time. first_seen is the earliest time ever recorded for a
// rule; last_seen is the latest.
func Merge(existing *PolicyEvidence, session SessionInfo, newRules []RuleEvidence, caps MergeCaps) {
	// Upsert session by ID — finalize() re-writes the same session with
	// updated flow counters, and we don't want duplicate entries.
	replaced := false
	for i := range existing.Sessions {
		if existing.Sessions[i].ID == session.ID {
			existing.Sessions[i] = session
			replaced = true
			break
		}
	}
	if !replaced {
		existing.Sessions = append(existing.Sessions, session)
	}
	if caps.MaxSessions > 0 && len(existing.Sessions) > caps.MaxSessions {
		sort.SliceStable(existing.Sessions, func(i, j int) bool {
			return existing.Sessions[i].StartedAt.Before(existing.Sessions[j].StartedAt)
		})
		drop := len(existing.Sessions) - caps.MaxSessions
		existing.Sessions = existing.Sessions[drop:]
	}

	byKey := make(map[string]int, len(existing.Rules))
	for i, r := range existing.Rules {
		byKey[r.Key] = i
	}

	for _, nr := range newRules {
		if idx, ok := byKey[nr.Key]; ok {
			merged := mergeRule(existing.Rules[idx], nr, caps.MaxSamples)
			existing.Rules[idx] = merged
			continue
		}
		// New rule: ensure samples are capped and sorted as well.
		nr.Samples = capSamples(nr.Samples, caps.MaxSamples)
		existing.Rules = append(existing.Rules, nr)
		byKey[nr.Key] = len(existing.Rules) - 1
	}

	sort.Slice(existing.Rules, func(i, j int) bool {
		if existing.Rules[i].Direction != existing.Rules[j].Direction {
			return existing.Rules[i].Direction < existing.Rules[j].Direction
		}
		return existing.Rules[i].Key < existing.Rules[j].Key
	})
}

func mergeRule(a, b RuleEvidence, maxSamples int) RuleEvidence {
	out := a
	out.FlowCount += b.FlowCount
	if !b.FirstSeen.IsZero() && (out.FirstSeen.IsZero() || b.FirstSeen.Before(out.FirstSeen)) {
		out.FirstSeen = b.FirstSeen
	}
	if b.LastSeen.After(out.LastSeen) {
		out.LastSeen = b.LastSeen
	}
	out.ContributingSessions = append(out.ContributingSessions, b.ContributingSessions...)
	out.Samples = capSamples(append(append([]FlowSample{}, a.Samples...), b.Samples...), maxSamples)
	return out
}

// capSamples sorts samples by time ascending and keeps the newest maxSamples.
// A non-positive maxSamples keeps all samples.
func capSamples(s []FlowSample, maxSamples int) []FlowSample {
	sort.SliceStable(s, func(i, j int) bool {
		return s[i].Time.Before(s[j].Time)
	})
	if maxSamples > 0 && len(s) > maxSamples {
		s = s[len(s)-maxSamples:]
	}
	return s
}
