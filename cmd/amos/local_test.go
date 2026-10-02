package main

import "testing"

func TestLocalCLIRequiresExplicitProject(t *testing.T) {
	for _, command := range []string{"dev", "status", "clean"} {
		if got := run([]string{command}); got != 2 {
			t.Fatalf("%s without explicit scope returned %d", command, got)
		}
	}
	if got := run([]string{"clean", "--project", "example", "--dir", "missing"}); got != 2 {
		t.Fatal("clean executed without explicit advisory plan flag")
	}
}

func TestCleanupRequiresOneExplicitMode(t *testing.T) {
	if got := run([]string{"clean", "--project", "example", "--dir", "missing", "--plan", "--execute"}); got != 2 {
		t.Fatal("ambiguous cleanup mode was accepted")
	}
	if got := run([]string{"clean", "--project", "example", "--dir", "missing", "--execute"}); got != 1 {
		t.Fatal("cleanup did not fail closed for missing owned state")
	}
}
