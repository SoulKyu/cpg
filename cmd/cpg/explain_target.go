package main

import (
	"fmt"
	"os"
	"strings"

	sigyaml "sigs.k8s.io/yaml"
)

type explainTarget struct {
	Namespace string
	Workload  string
}

// resolveExplainTarget accepts "NAMESPACE/WORKLOAD" or a path to a YAML policy
// file and returns the target. YAML files must carry a `cpg-` prefix on the
// policy name — `cpg explain` only documents cpg-generated policies.
func resolveExplainTarget(arg string) (explainTarget, error) {
	if strings.HasSuffix(arg, ".yaml") || strings.HasSuffix(arg, ".yml") {
		return resolveFromYAML(arg)
	}
	parts := strings.SplitN(arg, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return explainTarget{}, fmt.Errorf("invalid target %q: expected NAMESPACE/WORKLOAD or a policy YAML path", arg)
	}
	return explainTarget{Namespace: parts[0], Workload: parts[1]}, nil
}

func resolveFromYAML(path string) (explainTarget, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return explainTarget{}, fmt.Errorf("reading %s: %w", path, err)
	}
	type meta struct {
		Metadata struct {
			Name      string `yaml:"name"`
			Namespace string `yaml:"namespace"`
		} `yaml:"metadata"`
	}
	var m meta
	if err := sigyaml.Unmarshal(data, &m); err != nil {
		return explainTarget{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if m.Metadata.Name == "" || m.Metadata.Namespace == "" {
		return explainTarget{}, fmt.Errorf("%s: missing metadata.name or metadata.namespace", path)
	}
	if !strings.HasPrefix(m.Metadata.Name, "cpg-") {
		return explainTarget{}, fmt.Errorf("%s: policy name %q does not start with 'cpg-' — explain is scoped to cpg-generated policies", path, m.Metadata.Name)
	}
	return explainTarget{
		Namespace: m.Metadata.Namespace,
		Workload:  strings.TrimPrefix(m.Metadata.Name, "cpg-"),
	}, nil
}
