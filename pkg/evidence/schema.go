// Package evidence stores per-rule attribution for policies produced by cpg.
// It answers "which flows caused this rule?" for `cpg explain`.
package evidence

import "time"

// SchemaVersion is bumped whenever the on-disk format is not backwards
// compatible. Readers must refuse unknown versions.
//
// v1 → v2 (cpg v1.2): added optional `L7 *L7Ref` on RuleEvidence to record
// HTTP method/path or DNS matchName attribution. There is no back-compat
// reader for v1: v1.1 shipped 2026-04-24 and no v1 caches are expected in
// production. Readers reject any non-v2 file with a wipe instruction
// naming `$XDG_CACHE_HOME/cpg/evidence/`.
const SchemaVersion = 2

// PolicyEvidence is the root document persisted to
// <evidence-dir>/<output-dir-hash>/<namespace>/<workload>.json.
type PolicyEvidence struct {
	SchemaVersion int            `json:"schema_version"`
	Policy        PolicyRef      `json:"policy"`
	Sessions      []SessionInfo  `json:"sessions"`
	Rules         []RuleEvidence `json:"rules"`
}

// PolicyRef identifies the CiliumNetworkPolicy this evidence file documents.
type PolicyRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Workload  string `json:"workload"`
}

// SessionInfo records one invocation of generate or replay.
type SessionInfo struct {
	ID             string     `json:"id"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        time.Time  `json:"ended_at"`
	CPGVersion     string     `json:"cpg_version"`
	Source         SourceInfo `json:"source"`
	FlowsIngested  int64      `json:"flows_ingested"`
	FlowsUnhandled int64      `json:"flows_unhandled"`
}

// SourceInfo describes where flows came from for a session.
type SourceInfo struct {
	Type   string `json:"type"` // "live" | "replay"
	File   string `json:"file,omitempty"`
	Server string `json:"server,omitempty"`
}

// RuleEvidence attributes a single rule emitted in the policy YAML.
type RuleEvidence struct {
	Key                  string       `json:"key"`
	Direction            string       `json:"direction"` // "ingress" | "egress"
	Peer                 PeerRef      `json:"peer"`
	Port                 string       `json:"port"`
	Protocol             string       `json:"protocol"`
	L7                   *L7Ref       `json:"l7,omitempty"`
	FlowCount            int64        `json:"flow_count"`
	FirstSeen            time.Time    `json:"first_seen"`
	LastSeen             time.Time    `json:"last_seen"`
	ContributingSessions []string     `json:"contributing_sessions"`
	Samples              []FlowSample `json:"samples"`
}

// L7Ref attributes a rule to an L7 (HTTP or DNS) observation. Reserved on
// disk by Phase 7 (cpg v1.2); populated by the HTTP codegen path (Phase 8)
// and the DNS codegen path (Phase 9). For L4-only rules the surrounding
// RuleEvidence.L7 is nil and the field is omitted from JSON.
type L7Ref struct {
	Protocol     string `json:"protocol"` // "http" | "dns"
	HTTPMethod   string `json:"http_method,omitempty"`
	HTTPPath     string `json:"http_path,omitempty"`
	DNSMatchName string `json:"dns_matchname,omitempty"`
}

// PeerRef encodes the rule peer in a uniform shape across endpoint, CIDR, and
// entity peers. Only the field corresponding to Type is populated.
type PeerRef struct {
	Type   string            `json:"type"` // "endpoint" | "cidr" | "entity"
	Labels map[string]string `json:"labels,omitempty"`
	CIDR   string            `json:"cidr,omitempty"`
	Entity string            `json:"entity,omitempty"`
}

// FlowSample is a compact record of one contributing flow.
type FlowSample struct {
	Time       time.Time    `json:"time"`
	Src        FlowEndpoint `json:"src"`
	Dst        FlowEndpoint `json:"dst"`
	Port       uint32       `json:"port"`
	Protocol   string       `json:"protocol"`
	Verdict    string       `json:"verdict"`
	DropReason string       `json:"drop_reason,omitempty"`
}

// FlowEndpoint identifies a participant in a flow sample.
type FlowEndpoint struct {
	Namespace string `json:"namespace,omitempty"`
	Workload  string `json:"workload,omitempty"`
	Pod       string `json:"pod,omitempty"`
	IP        string `json:"ip,omitempty"`
}

// NewSkeleton returns an empty evidence document for a freshly observed policy.
func NewSkeleton(ref PolicyRef) PolicyEvidence {
	return PolicyEvidence{
		SchemaVersion: SchemaVersion,
		Policy:        ref,
	}
}
