package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sovereign-l1/l1/x/aez/client/cli"
)

// These are smoke tests for the CLI wiring itself: they drive the exact
// cobra command trees module.go's AppModule.GetTxCmd()/GetQueryCmd() return,
// the same trees the SDK's generic module manager collects into `l1d tx` and
// `l1d query`. They exist as a standalone verification path for when the
// full l1d binary cannot be built (e.g. an unrelated package elsewhere in
// the tree is mid-refactor) -- see the caller's own report for why that
// applied when this file was added.

func runCmd(t *testing.T, root *cobra.Command, args ...string) (string, error) {
	t.Helper()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestTxCmdHelpListsUpdateRoutingTable(t *testing.T) {
	root := &cobra.Command{Use: "l1d"}
	root.AddCommand(cli.GetTxCmd())

	out, err := runCmd(t, root, "aez", "--help")
	if err != nil {
		t.Fatalf("l1d tx aez --help returned error: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "update-routing-table") {
		t.Fatalf("expected update-routing-table listed in `tx aez --help` output, got:\n%s", out)
	}
}

func TestTxUpdateRoutingTableHelpDocumentsFlags(t *testing.T) {
	root := &cobra.Command{Use: "l1d"}
	root.AddCommand(cli.GetTxCmd())

	out, err := runCmd(t, root, "aez", "update-routing-table", "--help")
	if err != nil {
		t.Fatalf("l1d tx aez update-routing-table --help returned error: %v\noutput:\n%s", err, out)
	}
	for _, want := range []string{"--routing-table-file", "--authority", "activation_height", "buckets"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in `tx aez update-routing-table --help` output, got:\n%s", want, out)
		}
	}
}

func TestTxUpdateRoutingTableRequiresFile(t *testing.T) {
	root := &cobra.Command{Use: "l1d"}
	root.AddCommand(cli.GetTxCmd())

	_, err := runCmd(t, root, "aez", "update-routing-table")
	if err == nil {
		t.Fatal("expected an error when --routing-table-file is omitted, got nil")
	}
	if !strings.Contains(err.Error(), "routing-table-file") {
		t.Fatalf("expected error to mention routing-table-file, got: %v", err)
	}
}

func TestQueryCmdHelpListsAllRPCs(t *testing.T) {
	root := &cobra.Command{Use: "l1d"}
	root.AddCommand(cli.GetQueryCmd())

	out, err := runCmd(t, root, "aez", "--help")
	if err != nil {
		t.Fatalf("l1d query aez --help returned error: %v\noutput:\n%s", err, out)
	}
	for _, want := range []string{"params", "routing-table", "pending-routing-table", "zones", "zone-of"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q listed in `query aez --help` output, got:\n%s", want, out)
		}
	}
}

func TestQueryZoneOfRequiresTwoArgs(t *testing.T) {
	root := &cobra.Command{Use: "l1d"}
	root.AddCommand(cli.GetQueryCmd())

	_, err := runCmd(t, root, "aez", "zone-of", "address")
	if err == nil {
		t.Fatal("expected an error when zone-of is given only one argument, got nil")
	}
}
