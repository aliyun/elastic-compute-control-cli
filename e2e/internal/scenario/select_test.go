package scenario

import "testing"

func suites() []*Suite {
	return []*Suite{
		{Resource: "ecs/instance", Path: "cases/ecs/instance-lifecycle.yaml", Steps: []Step{{Name: "create"}, {Name: "get"}, {Name: "delete"}}},
		{Resource: "ecs/eni", Path: "cases/ecs/eni-lifecycle.yaml", Steps: []Step{{Name: "create"}}},
		{Resource: "vpc/vpc", Path: "cases/vpc/vpc-lifecycle.yaml", Steps: []Step{{Name: "create"}, {Name: "update"}}},
	}
}

func paths(ss []*Suite) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Path
	}
	return out
}

func TestSelectNoFilter(t *testing.T) {
	got, err := Select(suites(), Selection{})
	if err != nil || len(got) != 3 {
		t.Fatalf("want 3, got %d (%v)", len(got), err)
	}
}

func TestSelectTargetDir(t *testing.T) {
	got, err := Select(suites(), Selection{Targets: []string{"cases/ecs/"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 ecs cases, got %v", paths(got))
	}
}

func TestSelectTargetFile(t *testing.T) {
	got, err := Select(suites(), Selection{Targets: []string{"cases/vpc/vpc-lifecycle.yaml"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Resource != "vpc/vpc" {
		t.Fatalf("want vpc only, got %v", paths(got))
	}
}

func TestSelectNodeIDTruncates(t *testing.T) {
	got, err := Select(suites(), Selection{Targets: []string{"cases/vpc/vpc-lifecycle.yaml::create"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Steps) != 1 || got[0].Steps[0].Name != "create" {
		t.Fatalf("want vpc truncated to [create], got %+v", got)
	}
	// Source suite must be untouched.
	if len(suites()[2].Steps) != 2 {
		t.Fatal("source mutated")
	}
}

func TestSelectNodeIDUnknownStep(t *testing.T) {
	_, err := Select(suites(), Selection{Targets: []string{"cases/vpc/vpc-lifecycle.yaml::nope"}})
	if err == nil {
		t.Fatal("want error for unknown step")
	}
}

func TestSelectKeyword(t *testing.T) {
	cases := []struct {
		expr string
		want int
	}{
		{"vpc", 1},
		{"vpc or eni", 2},
		{"ecs", 2},
		{"ecs and not eni", 1},
		{"not ecs", 1},
		{"(vpc or eni) and not instance", 2},
	}
	for _, c := range cases {
		got, err := Select(suites(), Selection{Keyword: c.expr})
		if err != nil {
			t.Fatalf("%q: %v", c.expr, err)
		}
		if len(got) != c.want {
			t.Errorf("%q: want %d, got %v", c.expr, c.want, paths(got))
		}
	}
}

func TestSelectKeywordSyntaxError(t *testing.T) {
	for _, expr := range []string{"vpc and", "(vpc", "or eni", "vpc )"} {
		if _, err := Select(suites(), Selection{Keyword: expr}); err == nil {
			t.Errorf("%q: want syntax error", expr)
		}
	}
}

func TestSelectSurface(t *testing.T) {
	all := []*Suite{
		{Surface: SurfacePublic, Resource: "ecs/instance", Path: "cases/ecs/instance.yaml", Steps: []Step{{Name: "list"}}},
		{Surface: SurfaceFull, Resource: "lingjun/vcc", Path: "cases/lingjun/vcc.yaml", Steps: []Step{{Name: "list"}}},
	}
	got, err := Select(all, Selection{Surface: SurfacePublic})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Resource != "ecs/instance" {
		t.Fatalf("public selection = %+v, want only ecs/instance", paths(got))
	}
}

func TestSelectRejectsUnknownSurface(t *testing.T) {
	if _, err := Select(suites(), Selection{Surface: Surface("private")}); err == nil {
		t.Fatal("expected error for unknown surface")
	}
}

func TestSelectSurfaceConstrainsTargets(t *testing.T) {
	all := []*Suite{
		{Surface: SurfacePublic, Resource: "ecs/instance", Path: "cases/ecs/instance.yaml", Steps: []Step{{Name: "list"}}},
		{Surface: SurfaceFull, Resource: "lingjun/vcc", Path: "cases/lingjun/vcc.yaml", Steps: []Step{{Name: "list"}}},
	}

	got, err := Select(all, Selection{
		Surface: SurfacePublic,
		Targets: []string{"cases/lingjun/vcc.yaml"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("public target selection leaked full case: %+v", paths(got))
	}
}
