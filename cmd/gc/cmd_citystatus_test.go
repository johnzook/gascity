package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/runtime"
)

func TestCityStatusEmptyCity(t *testing.T) {
	sp := runtime.NewFake()
	dops := newFakeDrainOps()
	cfg := &config.City{
		Workspace: config.Workspace{Name: "bright-lights"},
	}

	var stdout, stderr bytes.Buffer
	code := doCityStatus(sp, dops, cfg, "/home/user/bright-lights", &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "bright-lights") {
		t.Errorf("stdout missing city name, got:\n%s", out)
	}
	if !strings.Contains(out, "/home/user/bright-lights") {
		t.Errorf("stdout missing city path, got:\n%s", out)
	}
	if !strings.Contains(out, "Controller: stopped") {
		t.Errorf("stdout missing controller status, got:\n%s", out)
	}
	if !strings.Contains(out, "Suspended:  no") {
		t.Errorf("stdout missing 'Suspended:  no', got:\n%s", out)
	}
	// No agents section when there are no agents.
	if strings.Contains(out, "Agents:") {
		t.Errorf("stdout should not have Agents section for empty city, got:\n%s", out)
	}
}

func TestCityStatusWithAgents(t *testing.T) {
	sp := runtime.NewFake()
	// Start one agent session.
	if err := sp.Start(context.Background(), "mayor", runtime.Config{Command: "echo"}); err != nil {
		t.Fatal(err)
	}
	dops := newFakeDrainOps()
	cfg := &config.City{
		Workspace: config.Workspace{Name: "city"},
		Agents: []config.Agent{
			{Name: "mayor", MaxActiveSessions: intPtr(1)},
			{Name: "worker", MaxActiveSessions: intPtr(1)},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doCityStatus(sp, dops, cfg, "/home/user/city", &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}
	out := stdout.String()

	if !strings.Contains(out, "Agents:") {
		t.Errorf("stdout missing 'Agents:', got:\n%s", out)
	}
	if !strings.Contains(out, "mayor") {
		t.Errorf("stdout missing 'mayor', got:\n%s", out)
	}
	if !strings.Contains(out, "worker") {
		t.Errorf("stdout missing 'worker', got:\n%s", out)
	}
	if !strings.Contains(out, "1/2 agents running") {
		t.Errorf("stdout missing '1/2 agents running', got:\n%s", out)
	}
}

func TestCityStatusSuspended(t *testing.T) {
	sp := runtime.NewFake()
	dops := newFakeDrainOps()
	cfg := &config.City{
		Workspace: config.Workspace{Name: "city", Suspended: true, MaxActiveSessions: intPtr(1)},
		Agents:    []config.Agent{{Name: "mayor", MaxActiveSessions: intPtr(1)}},
	}

	var stdout, stderr bytes.Buffer
	code := doCityStatus(sp, dops, cfg, "/tmp/city", &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	out := stdout.String()
	if !strings.Contains(out, "Suspended:  yes") {
		t.Errorf("stdout missing 'Suspended:  yes', got:\n%s", out)
	}
}

func TestCityStatusPoolExpansion(t *testing.T) {
	sp := runtime.NewFake()
	// Start 2 of 3 pool instances.
	if err := sp.Start(context.Background(), "hw--polecat-1", runtime.Config{Command: "echo"}); err != nil {
		t.Fatal(err)
	}
	if err := sp.Start(context.Background(), "hw--polecat-2", runtime.Config{Command: "echo"}); err != nil {
		t.Fatal(err)
	}
	dops := newFakeDrainOps()
	dops.draining["hw--polecat-2"] = true

	cfg := &config.City{
		Workspace: config.Workspace{Name: "city"},
		Agents: []config.Agent{
			{Name: "polecat", Dir: "hw", MinActiveSessions: intPtr(1), MaxActiveSessions: intPtr(3), ScaleCheck: "echo 1"},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doCityStatus(sp, dops, cfg, "/tmp/city", &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}
	out := stdout.String()

	// Pool header line.
	if !strings.Contains(out, "pool (min=1, max=3)") {
		t.Errorf("stdout missing pool header, got:\n%s", out)
	}
	// Instance lines.
	if !strings.Contains(out, "polecat-1") {
		t.Errorf("stdout missing polecat-1, got:\n%s", out)
	}
	if !strings.Contains(out, "polecat-2") {
		t.Errorf("stdout missing polecat-2, got:\n%s", out)
	}
	if !strings.Contains(out, "polecat-3") {
		t.Errorf("stdout missing polecat-3, got:\n%s", out)
	}
	// polecat-2 draining.
	if !strings.Contains(out, "running  (draining)") {
		t.Errorf("stdout missing 'running  (draining)', got:\n%s", out)
	}
	// Summary: 2/3 running.
	if !strings.Contains(out, "2/3 agents running") {
		t.Errorf("stdout missing '2/3 agents running', got:\n%s", out)
	}
}

// injectCliStoreCache overrides the package-level cliStoreCache with the
// given store and registers cleanup to restore the prior contents. Tests
// that exercise bead-store lookups from CLI functions need this because
// the cache is process-wide and would otherwise leak between tests.
func injectCliStoreCache(t *testing.T, cityPath string, store beads.Store) {
	t.Helper()
	cliStoreCache.mu.Lock()
	prevPath := cliStoreCache.path
	prevStore := cliStoreCache.store
	cliStoreCache.path = cityPath
	cliStoreCache.store = store
	cliStoreCache.mu.Unlock()
	t.Cleanup(func() {
		cliStoreCache.mu.Lock()
		cliStoreCache.path = prevPath
		cliStoreCache.store = prevStore
		cliStoreCache.mu.Unlock()
	})
}

// TestCityStatusPoolUsesBeadDerivedSessionNames verifies that gc status
// resolves pool instance running state by consulting the bead store for
// the actual session_name (e.g. "polecat-lx-abc"), not the synthetic
// name that cliSessionName would compute from the instance qualified
// name. Regression for gc-124: without this lookup, pool slots whose
// runtime session uses a bead-derived name are incorrectly reported
// as stopped.
func TestCityStatusPoolUsesBeadDerivedSessionNames(t *testing.T) {
	sp := runtime.NewFake()
	if err := sp.Start(context.Background(), "polecat-lx-abc", runtime.Config{Command: "echo"}); err != nil {
		t.Fatal(err)
	}
	dops := newFakeDrainOps()

	cityPath := t.TempDir()
	store := beads.NewMemStore()
	if _, err := store.Create(beads.Bead{
		Type:   sessionBeadType,
		Labels: []string{sessionBeadLabel, "agent:gascity/polecat"},
		Metadata: map[string]string{
			"agent_name":   "gascity/polecat",
			"template":     "gascity/polecat",
			"pool_slot":    "1",
			"pool_managed": boolMetadata(true),
			"session_name": "polecat-lx-abc",
		},
	}); err != nil {
		t.Fatal(err)
	}
	injectCliStoreCache(t, cityPath, store)

	cfg := &config.City{
		Workspace: config.Workspace{Name: "loomington"},
		Agents: []config.Agent{
			{Name: "polecat", Dir: "gascity", MinActiveSessions: intPtr(0), MaxActiveSessions: intPtr(3), ScaleCheck: "echo 1"},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doCityStatus(sp, dops, cfg, cityPath, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}
	out := stdout.String()

	if !strings.Contains(out, "pool (min=0, max=3)") {
		t.Errorf("stdout missing pool header, got:\n%s", out)
	}
	// Slot 1 is backed by the fake runtime session polecat-lx-abc.
	// The display name stays the instance-qualified form.
	if !strings.Contains(out, "gascity/polecat-1") {
		t.Errorf("stdout missing gascity/polecat-1, got:\n%s", out)
	}
	// With the fix, slot 1 should be reported as running; without it,
	// every slot would show stopped and the summary would be 0/3.
	if !strings.Contains(out, "1/3 agents running") {
		t.Errorf("stdout missing '1/3 agents running' (bead-derived session lookup failed), got:\n%s", out)
	}
}

// TestCityStatusJSONPoolUsesBeadDerivedSessionNames mirrors the above
// for the JSON output path.
func TestCityStatusJSONPoolUsesBeadDerivedSessionNames(t *testing.T) {
	sp := runtime.NewFake()
	if err := sp.Start(context.Background(), "polecat-lx-def", runtime.Config{Command: "echo"}); err != nil {
		t.Fatal(err)
	}

	cityPath := t.TempDir()
	store := beads.NewMemStore()
	if _, err := store.Create(beads.Bead{
		Type:   sessionBeadType,
		Labels: []string{sessionBeadLabel, "agent:gascity/polecat"},
		Metadata: map[string]string{
			"agent_name":   "gascity/polecat",
			"template":     "gascity/polecat",
			"pool_slot":    "2",
			"pool_managed": boolMetadata(true),
			"session_name": "polecat-lx-def",
		},
	}); err != nil {
		t.Fatal(err)
	}
	injectCliStoreCache(t, cityPath, store)

	cfg := &config.City{
		Workspace: config.Workspace{Name: "loomington"},
		Agents: []config.Agent{
			{Name: "polecat", Dir: "gascity", MinActiveSessions: intPtr(0), MaxActiveSessions: intPtr(3), ScaleCheck: "echo 1"},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doCityStatusJSON(sp, cfg, cityPath, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}

	var status StatusJSON
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal: %v; output: %s", err, stdout.String())
	}
	if status.Summary.RunningAgents != 1 {
		t.Errorf("Summary.RunningAgents = %d, want 1 (bead-derived session lookup failed)", status.Summary.RunningAgents)
	}
	var slot2 *StatusAgentJSON
	for i := range status.Agents {
		if status.Agents[i].QualifiedName == "gascity/polecat-2" {
			slot2 = &status.Agents[i]
			break
		}
	}
	if slot2 == nil {
		t.Fatalf("gascity/polecat-2 missing from Agents: %#v", status.Agents)
	}
	if !slot2.Running {
		t.Errorf("gascity/polecat-2.Running = false, want true")
	}
}

func TestCityStatusRigs(t *testing.T) {
	sp := runtime.NewFake()
	dops := newFakeDrainOps()
	cfg := &config.City{
		Workspace: config.Workspace{Name: "city"},
		Agents:    []config.Agent{{Name: "mayor", MaxActiveSessions: intPtr(1)}},
		Rigs: []config.Rig{
			{Name: "hello-world", Path: "/home/user/hello-world"},
			{Name: "frontend", Path: "/home/user/frontend", Suspended: true},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doCityStatus(sp, dops, cfg, "/tmp/city", &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	out := stdout.String()
	if !strings.Contains(out, "Rigs:") {
		t.Errorf("stdout missing 'Rigs:', got:\n%s", out)
	}
	if !strings.Contains(out, "hello-world") {
		t.Errorf("stdout missing 'hello-world', got:\n%s", out)
	}
	if !strings.Contains(out, "/home/user/hello-world") {
		t.Errorf("stdout missing hello-world path, got:\n%s", out)
	}
	if !strings.Contains(out, "frontend") {
		t.Errorf("stdout missing 'frontend', got:\n%s", out)
	}
	if !strings.Contains(out, "(suspended)") {
		t.Errorf("stdout missing '(suspended)' for frontend, got:\n%s", out)
	}
}

func TestCityStatusJSONEmpty(t *testing.T) {
	sp := runtime.NewFake()
	cfg := &config.City{
		Workspace: config.Workspace{Name: "bright-lights"},
	}

	var stdout, stderr bytes.Buffer
	code := doCityStatusJSON(sp, cfg, "/home/user/bright-lights", &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}

	var status StatusJSON
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal: %v; output: %s", err, stdout.String())
	}
	if status.CityName != "bright-lights" {
		t.Errorf("city_name = %q, want %q", status.CityName, "bright-lights")
	}
	if status.CityPath != "/home/user/bright-lights" {
		t.Errorf("city_path = %q, want %q", status.CityPath, "/home/user/bright-lights")
	}
	if status.Controller.Running {
		t.Error("controller should not be running")
	}
	if status.Suspended {
		t.Error("suspended should be false")
	}
	if status.Summary.TotalAgents != 0 {
		t.Errorf("total_agents = %d, want 0", status.Summary.TotalAgents)
	}
}

func TestCityStatusJSONWithAgents(t *testing.T) {
	sp := runtime.NewFake()
	// Start one agent session (default session name = agent name, no city prefix).
	if err := sp.Start(context.Background(), "mayor", runtime.Config{Command: "echo"}); err != nil {
		t.Fatal(err)
	}

	cfg := &config.City{
		Workspace: config.Workspace{Name: "city"},
		Agents: []config.Agent{
			{Name: "mayor", MaxActiveSessions: intPtr(1)},
			{Name: "polecat", Dir: "myrig", MinActiveSessions: intPtr(0), MaxActiveSessions: intPtr(3)},
		},
		Rigs: []config.Rig{
			{Name: "myrig", Path: "/home/user/myrig"},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doCityStatusJSON(sp, cfg, "/home/user/city", &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}

	var status StatusJSON
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal: %v; output: %s", err, stdout.String())
	}

	// Mayor singleton + 3 pool instances = 4 agents.
	if status.Summary.TotalAgents != 4 {
		t.Errorf("total_agents = %d, want 4", status.Summary.TotalAgents)
	}
	if status.Summary.RunningAgents != 1 {
		t.Errorf("running_agents = %d, want 1", status.Summary.RunningAgents)
	}
	if len(status.Agents) != 4 {
		t.Fatalf("got %d agents, want 4", len(status.Agents))
	}

	// First agent: mayor (singleton, running).
	if status.Agents[0].Name != "mayor" {
		t.Errorf("agents[0].name = %q, want %q", status.Agents[0].Name, "mayor")
	}
	if status.Agents[0].Scope != "city" {
		t.Errorf("agents[0].scope = %q, want %q", status.Agents[0].Scope, "city")
	}
	if !status.Agents[0].Running {
		t.Error("agents[0] should be running")
	}
	if status.Agents[0].Pool != nil {
		t.Error("agents[0].pool should be nil for singleton")
	}

	// Second agent: polecat-1 (pool, not running).
	if status.Agents[1].QualifiedName != "myrig/polecat-1" {
		t.Errorf("agents[1].qualified_name = %q, want %q", status.Agents[1].QualifiedName, "myrig/polecat-1")
	}
	if status.Agents[1].Scope != "rig" {
		t.Errorf("agents[1].scope = %q, want %q", status.Agents[1].Scope, "rig")
	}
	if status.Agents[1].Pool == nil {
		t.Fatal("agents[1].pool should not be nil")
	}
	if status.Agents[1].Pool.Max != 3 {
		t.Errorf("agents[1].pool.max = %d, want 3", status.Agents[1].Pool.Max)
	}

	// Rigs.
	if len(status.Rigs) != 1 {
		t.Fatalf("got %d rigs, want 1", len(status.Rigs))
	}
	if status.Rigs[0].Name != "myrig" {
		t.Errorf("rigs[0].name = %q, want %q", status.Rigs[0].Name, "myrig")
	}
}

func TestCityStatusAgentSuspendedByRig(t *testing.T) {
	sp := runtime.NewFake()
	dops := newFakeDrainOps()
	cfg := &config.City{
		Workspace: config.Workspace{Name: "city"},
		Agents: []config.Agent{
			{Name: "polecat", Dir: "myrig", MaxActiveSessions: intPtr(1)},
		},
		Rigs: []config.Rig{
			{Name: "myrig", Path: "/tmp/myrig", Suspended: true},
		},
	}

	var stdout, stderr bytes.Buffer
	code := doCityStatus(sp, dops, cfg, "/tmp/city", &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	out := stdout.String()
	// Agent in suspended rig should show "stopped  (suspended)".
	if !strings.Contains(out, "stopped  (suspended)") {
		t.Errorf("stdout missing 'stopped  (suspended)' for rig-suspended agent, got:\n%s", out)
	}
}

func TestControllerStatusLine(t *testing.T) {
	tests := []struct {
		name string
		ctrl ControllerJSON
		want string
	}{
		{
			name: "supervisor not running",
			ctrl: ControllerJSON{Mode: "supervisor"},
			want: "supervisor-managed (supervisor not running)",
		},
		{
			name: "supervisor city stopped",
			ctrl: ControllerJSON{Mode: "supervisor", PID: 4321},
			want: "supervisor (PID 4321, city stopped)",
		},
		{
			name: "supervisor city starting bead store",
			ctrl: ControllerJSON{Mode: "supervisor", PID: 4321, Status: "starting_bead_store"},
			want: "supervisor (PID 4321, starting bead store)",
		},
		{
			name: "supervisor city init failed",
			ctrl: ControllerJSON{Mode: "supervisor", PID: 4321, Status: "init_failed"},
			want: "supervisor (PID 4321, init failed)",
		},
		{
			name: "supervisor running",
			ctrl: ControllerJSON{Mode: "supervisor", PID: 4321, Running: true},
			want: "supervisor (PID 4321)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := controllerStatusLine(tt.ctrl); got != tt.want {
				t.Fatalf("controllerStatusLine(%+v) = %q, want %q", tt.ctrl, got, tt.want)
			}
		})
	}
}
