package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SoulKyu/cpg/pkg/evidence"
)

func newExplainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain <NAMESPACE/WORKLOAD | path/to/policy.yaml>",
		Short: "Explain which flows produced a policy's rules",
		Long: `Inspect the per-rule evidence for a generated policy. Evidence is captured
automatically by 'cpg generate' and 'cpg replay' and stored under
$XDG_CACHE_HOME/cpg/evidence (Linux: ~/.cache/cpg/evidence).

Examples:

  cpg explain production/api-server
  cpg explain ./policies/production/api-server.yaml
  cpg explain production/api-server --ingress --port 8080
  cpg explain production/api-server --peer app=frontend --json`,
		Args: cobra.ExactArgs(1),
		RunE: runExplain,
	}
	f := cmd.Flags()
	f.StringP("output-dir", "o", "./policies", "output directory the policy was generated into (used to locate evidence)")
	f.String("evidence-dir", "", "override evidence storage lookup path")
	f.Bool("ingress", false, "filter: ingress rules only")
	f.Bool("egress", false, "filter: egress rules only")
	f.String("port", "", "filter: rules using this port")
	f.String("peer", "", "filter: endpoint peer with KEY=VAL")
	f.String("peer-cidr", "", "filter: CIDR peer contained in this CIDR")
	f.String("http-method", "", "filter: rules attributed to this HTTP method (exact, case-insensitive)")
	f.String("http-path", "", "filter: rules attributed to this HTTP path (literal exact match)")
	f.String("dns-pattern", "", "filter: rules attributed to this DNS matchName (literal exact, trailing dot stripped)")
	f.Duration("since", 0, "filter: flows last seen within this duration")
	f.Int("samples-limit", 10, "max samples to display per rule")
	f.Bool("json", false, "output JSON instead of formatted text")
	f.String("format", "text", "output format: text | json | yaml")
	return cmd
}

func runExplain(cmd *cobra.Command, args []string) error {
	target, err := resolveExplainTarget(args[0])
	if err != nil {
		return err
	}

	outputDir, _ := cmd.Flags().GetString("output-dir")
	evDir, _ := cmd.Flags().GetString("evidence-dir")
	if evDir == "" {
		evDir, err = evidence.DefaultEvidenceDir()
		if err != nil {
			return err
		}
	}

	absOutDir := outputDir
	hash := evidence.HashOutputDir(absOutDir)
	reader := evidence.NewReader(evDir, hash)
	pe, err := reader.Read(target.Namespace, target.Workload)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			path := evidence.ResolvePolicyPath(evDir, hash, target.Namespace, target.Workload)
			return fmt.Errorf("no evidence found for %s/%s at %s (run `cpg generate` or `cpg replay` with the same --output-dir first)", target.Namespace, target.Workload, path)
		}
		return err
	}

	filter, err := buildFilter(cmd)
	if err != nil {
		return err
	}
	matched := make([]evidence.RuleEvidence, 0, len(pe.Rules))
	for _, r := range pe.Rules {
		if filter.match(r) {
			matched = append(matched, r)
		}
	}

	samplesLimit, _ := cmd.Flags().GetInt("samples-limit")
	jsonFlag, _ := cmd.Flags().GetBool("json")
	format, _ := cmd.Flags().GetString("format")
	if jsonFlag {
		format = "json"
	}

	out := cmd.OutOrStdout()
	switch format {
	case "json":
		return renderJSON(out, pe, matched)
	case "yaml":
		return renderYAML(out, pe, matched)
	case "text":
		// TTY detection: only color when writing directly to a terminal, not
		// when tests capture via bytes.Buffer.
		color := false
		if f, ok := out.(*os.File); ok {
			color = isTerminal(f)
		}
		return renderText(out, pe, matched, samplesLimit, color)
	default:
		return fmt.Errorf("unknown format %q: expected text | json | yaml", format)
	}
}

func buildFilter(cmd *cobra.Command) (explainFilter, error) {
	f := explainFilter{Now: time.Now()}
	ing, _ := cmd.Flags().GetBool("ingress")
	eg, _ := cmd.Flags().GetBool("egress")
	if ing && eg {
		return f, fmt.Errorf("--ingress and --egress are mutually exclusive")
	}
	if ing {
		f.Direction = "ingress"
	}
	if eg {
		f.Direction = "egress"
	}
	f.Port, _ = cmd.Flags().GetString("port")

	peer, _ := cmd.Flags().GetString("peer")
	if peer != "" {
		k, v, ok := parsePeerLabel(peer)
		if !ok {
			return f, fmt.Errorf("--peer must be KEY=VAL")
		}
		f.PeerLabel.Set = true
		f.PeerLabel.Key = k
		f.PeerLabel.Value = v
	}

	peerCIDR, _ := cmd.Flags().GetString("peer-cidr")
	if peerCIDR != "" {
		_, ipnet, err := net.ParseCIDR(peerCIDR)
		if err != nil {
			return f, fmt.Errorf("--peer-cidr %q: %w", peerCIDR, err)
		}
		f.PeerCIDR = ipnet
	}

	f.Since, _ = cmd.Flags().GetDuration("since")

	method, _ := cmd.Flags().GetString("http-method")
	f.HTTPMethod = strings.ToUpper(strings.TrimSpace(method))

	path, _ := cmd.Flags().GetString("http-path")
	f.HTTPPath = path

	dns, _ := cmd.Flags().GetString("dns-pattern")
	f.DNSPattern = strings.TrimSuffix(strings.TrimSpace(dns), ".")
	return f, nil
}
