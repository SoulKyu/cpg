package dropclass

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
)

// TestClassifierVersionMatchesGoMod is a drift guard: when go.mod bumps
// github.com/cilium/cilium to a new version, this test fails until
// ClassifierVersion is also bumped. The author MUST audit pkg/dropclass
// for new DropReason enum values before bumping the version suffix.
//
// Uses golang.org/x/mod/modfile (I-6/I-7) instead of regex parsing to
// correctly handle replace directives, indirect requires, and future
// go.mod syntax changes (e.g. tool directives).
//
// To fix a failing test:
//  1. Update ClassifierVersion in pkg/dropclass/version.go to match the new cilium version.
//  2. Audit pkg/dropclass/classifier.go for any new DropReason enum values in the updated cilium module.
//  3. Assign each new reason a DropClass bucket in dropReasonClass.
func TestClassifierVersionMatchesGoMod(t *testing.T) {
	data, err := os.ReadFile("../../go.mod")
	require.NoError(t, err, "reading go.mod")
	mf, err := modfile.Parse("go.mod", data, nil)
	require.NoError(t, err, "parsing go.mod")
	for _, r := range mf.Require {
		if r.Mod.Path == "github.com/cilium/cilium" {
			version := strings.TrimPrefix(r.Mod.Version, "v")
			expected := "cilium" + version
			require.Truef(t, strings.HasSuffix(ClassifierVersion, expected),
				"ClassifierVersion %q must end with %q (go.mod has cilium %s); bump pkg/dropclass/version.go after auditing pkg/dropclass/classifier.go for new DropReason values",
				ClassifierVersion, expected, r.Mod.Version)
			return
		}
	}
	t.Fatalf("github.com/cilium/cilium not found in go.mod require directives")
}
