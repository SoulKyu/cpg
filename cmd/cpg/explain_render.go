package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	sigyaml "sigs.k8s.io/yaml"

	"github.com/SoulKyu/cpg/pkg/evidence"
)

const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[2m"
	ansiGreen = "\x1b[32m"
)

type explainOutput struct {
	Policy       evidence.PolicyRef      `json:"policy"`
	Sessions     []evidence.SessionInfo  `json:"sessions"`
	MatchedRules []evidence.RuleEvidence `json:"matched_rules"`
}

func renderText(w io.Writer, pe evidence.PolicyEvidence, matched []evidence.RuleEvidence, samplesLimit int, color bool) error {
	c := colorizer{enabled: color}
	fmt.Fprintf(w, "%sPolicy:%s %s (%s)\n", c.bold(), c.reset(), pe.Policy.Name, pe.Policy.Namespace)
	if len(pe.Sessions) > 0 {
		last := pe.Sessions[len(pe.Sessions)-1]
		fmt.Fprintf(w, "%sLatest session:%s %s → %s (source: %s)\n\n",
			c.dim(), c.reset(),
			last.StartedAt.Format("2006-01-02 15:04"),
			last.EndedAt.Format("15:04"),
			last.Source.Type,
		)
	}

	if len(matched) == 0 {
		fmt.Fprintln(w, "No rules matched the given filters.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Available rules:")
		for _, r := range pe.Rules {
			fmt.Fprintf(w, "  - %s %s %s/%s\n", r.Direction, peerSummary(r.Peer), r.Port, r.Protocol)
		}
		return nil
	}

	for _, r := range matched {
		writeRule(w, c, r, samplesLimit)
	}
	return nil
}

func writeRule(w io.Writer, c colorizer, r evidence.RuleEvidence, limit int) {
	title := strings.ToUpper(r.Direction[:1]) + r.Direction[1:] + " rule"
	fmt.Fprintf(w, "%s%s%s\n", c.green(), title, c.reset())
	fmt.Fprintf(w, "  Peer:        %s\n", peerSummary(r.Peer))
	fmt.Fprintf(w, "  Port:        %s/%s\n", r.Port, r.Protocol)
	fmt.Fprintf(w, "  Flow count:  %d\n", r.FlowCount)
	fmt.Fprintf(w, "  First seen:  %s\n", r.FirstSeen.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "  Last seen:   %s\n", r.LastSeen.Format("2006-01-02 15:04:05"))

	if r.L7 != nil {
		switch r.L7.Protocol {
		case "http":
			fmt.Fprintf(w, "  L7:          HTTP %s %s\n", r.L7.HTTPMethod, r.L7.HTTPPath)
		case "dns":
			fmt.Fprintf(w, "  L7:          DNS %s\n", r.L7.DNSMatchName)
		}
	}

	samples := r.Samples
	if limit > 0 && len(samples) > limit {
		samples = samples[len(samples)-limit:]
	}

	if len(samples) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  Sample flows:")
		for _, s := range samples {
			suffix := ""
			if s.DropReason != "" {
				suffix = "  drop=" + s.DropReason
			}
			fmt.Fprintf(w, "    %s  %s → %s  %s/%d%s\n",
				s.Time.Format("15:04:05"),
				fmtEndpoint(s.Src), fmtEndpoint(s.Dst),
				s.Protocol, s.Port, suffix,
			)
		}
	}
	fmt.Fprintln(w)
}

func peerSummary(p evidence.PeerRef) string {
	switch p.Type {
	case "endpoint":
		keys := make([]string, 0, len(p.Labels))
		for k := range p.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = fmt.Sprintf("%s=%s", k, p.Labels[k])
		}
		return strings.Join(parts, ",") + " (endpoint)"
	case "cidr":
		return p.CIDR + " (cidr)"
	case "entity":
		return p.Entity + " (entity)"
	default:
		return "unknown"
	}
}

func fmtEndpoint(e evidence.FlowEndpoint) string {
	if e.Namespace != "" && e.Workload != "" {
		return e.Namespace + "/" + e.Workload
	}
	if e.IP != "" {
		return e.IP
	}
	return "<unknown>"
}

func renderJSON(w io.Writer, pe evidence.PolicyEvidence, matched []evidence.RuleEvidence) error {
	out := explainOutput{Policy: pe.Policy, Sessions: pe.Sessions, MatchedRules: matched}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func renderYAML(w io.Writer, pe evidence.PolicyEvidence, matched []evidence.RuleEvidence) error {
	out := explainOutput{Policy: pe.Policy, Sessions: pe.Sessions, MatchedRules: matched}
	data, err := sigyaml.Marshal(out)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

type colorizer struct{ enabled bool }

func (c colorizer) bold() string {
	if c.enabled {
		return ansiBold
	}
	return ""
}
func (c colorizer) dim() string {
	if c.enabled {
		return ansiDim
	}
	return ""
}
func (c colorizer) green() string {
	if c.enabled {
		return ansiGreen
	}
	return ""
}
func (c colorizer) reset() string {
	if c.enabled {
		return ansiReset
	}
	return ""
}
