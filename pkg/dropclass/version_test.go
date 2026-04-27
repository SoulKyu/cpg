package dropclass

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestClassifierVersionMatchesGoMod is a drift guard: when go.mod bumps
// github.com/cilium/cilium to a new version, this test fails until
// ClassifierVersion is also bumped. The author MUST audit pkg/dropclass
// for new DropReason enum values before bumping the version suffix.
//
// To fix a failing test:
//  1. Update ClassifierVersion in pkg/dropclass/version.go to match the new cilium version.
//  2. Audit pkg/dropclass/classifier.go for any new DropReason enum values in the updated cilium module.
//  3. Assign each new reason a DropClass bucket in dropReasonClass.
func TestClassifierVersionMatchesGoMod(t *testing.T) {
	// go.mod path relative to this test file (pkg/dropclass/) is ../../go.mod.
	data, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	re := regexp.MustCompile(`(?m)^\s*github\.com/cilium/cilium\s+v(\S+)\s*$`)
	m := re.FindStringSubmatch(string(data))
	if len(m) != 2 {
		t.Fatalf("could not find github.com/cilium/cilium version in go.mod")
	}
	ciliumVer := m[1] // e.g. "1.19.1"
	wantSuffix := "-cilium" + ciliumVer
	if !strings.HasSuffix(ClassifierVersion, wantSuffix) {
		t.Fatalf(
			"ClassifierVersion drift: %q does not end with %q.\n"+
				"go.mod has cilium v%s. Bump ClassifierVersion AND audit pkg/dropclass for new DropReason enum values.",
			ClassifierVersion, wantSuffix, ciliumVer,
		)
	}
}
