package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wepala/weos/v3/application"
)

type gateProbeInput struct{}

type gateProbeOutput struct {
	Ran bool `json:"ran"`
}

// gatedTestServer builds a server holding one gated and one ungated tool, plus
// a record of whether each handler ran — which is what separates a refusal
// from a call that went through and merely reported an error.
func gatedTestServer(t *testing.T, gate FeatureGate) (*mcp.Server, *bool, *bool) {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "weos-test", Version: "0.1.0"}, nil)
	gates := NewFeatureGates()
	server.AddReceivingMiddleware(featureGateMiddleware(gates, gate))

	gatedRan, plainRan := false, false
	AddGatedTool(server, gates, "ledger-export", &mcp.Tool{Name: "gated_tool"}, func(
		_ context.Context, _ *mcp.CallToolRequest, _ gateProbeInput,
	) (*mcp.CallToolResult, gateProbeOutput, error) {
		gatedRan = true
		return nil, gateProbeOutput{Ran: true}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "plain_tool"}, func(
		_ context.Context, _ *mcp.CallToolRequest, _ gateProbeInput,
	) (*mcp.CallToolResult, gateProbeOutput, error) {
		plainRan = true
		return nil, gateProbeOutput{Ran: true}, nil
	})
	return server, &gatedRan, &plainRan
}

func connectForGate(t *testing.T, ctx context.Context, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("connect server: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "probe", Version: "0.1.0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func listedNames(t *testing.T, cs *mcp.ClientSession, ctx context.Context) []string {
	t.Helper()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func has(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// TestGateHidesAndRefusesOnlyTheGatedTool is the pair the story turns on: the
// listing is a convenience, the refusal is the control, and an ungated tool is
// touched by neither.
func TestGateHidesAndRefusesOnlyTheGatedTool(t *testing.T) {
	ctx := context.Background()
	server, gatedRan, plainRan := gatedTestServer(t, func(context.Context, string) bool { return false })
	cs := connectForGate(t, ctx, server)

	names := listedNames(t, cs, ctx)
	if has(names, "gated_tool") {
		t.Fatal("a tool whose feature is off was offered to the caller")
	}
	if !has(names, "plain_tool") {
		t.Fatalf("an ungated tool went missing: %v", names)
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "gated_tool"})
	if err != nil {
		t.Fatalf("the refusal came back as a protocol error, which a model cannot read: %v", err)
	}
	if !res.IsError {
		t.Fatal("the call was not refused")
	}
	if *gatedRan {
		t.Fatal("the handler ran despite the refusal")
	}
	if res.StructuredContent != nil {
		t.Fatalf("the refusal carried a partial result: %v", res.StructuredContent)
	}
	text := textFromResult(res)
	if !strings.Contains(text, "gated_tool") || !strings.Contains(text, "not enabled for you") {
		t.Fatalf("the refusal does not say what happened: %q", text)
	}

	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "plain_tool"}); err != nil {
		t.Fatalf("an ungated tool was disturbed by the gate: %v", err)
	}
	if !*plainRan {
		t.Fatal("the ungated tool's handler did not run")
	}
}

// TestGateLetsAnEnabledToolThroughUnchanged pins that a gate costs a tool
// nothing when its feature is on.
func TestGateLetsAnEnabledToolThroughUnchanged(t *testing.T) {
	ctx := context.Background()
	server, gatedRan, _ := gatedTestServer(t, func(context.Context, string) bool { return true })
	cs := connectForGate(t, ctx, server)

	if names := listedNames(t, cs, ctx); !has(names, "gated_tool") {
		t.Fatalf("a tool whose feature is on was withheld: %v", names)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "gated_tool"})
	if err != nil || res.IsError {
		t.Fatalf("the call did not go through: %v %v", err, textFromResult(res))
	}
	if !*gatedRan {
		t.Fatal("the handler did not run")
	}
}

// TestToolInventoryWidensTheListingButNeverACall is the safety property of the
// boot-time inventory: it can enumerate a tool the caller may not use, and it
// still cannot run it.
func TestToolInventoryWidensTheListingButNeverACall(t *testing.T) {
	ctx := WithToolInventory(context.Background())
	server, gatedRan, _ := gatedTestServer(t, func(context.Context, string) bool { return false })
	cs := connectForGate(t, ctx, server)

	if names := listedNames(t, cs, ctx); !has(names, "gated_tool") {
		t.Fatalf("the inventory did not enumerate every registered tool: %v", names)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "gated_tool"})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatal("the inventory marker let a gated call through; it must only widen a listing")
	}
	if *gatedRan {
		t.Fatal("the handler ran under the inventory marker")
	}
}

// TestNilGateGatesNothing covers the low-level constructor's contract: a build
// with no feature wiring behaves exactly as it did before gates existed.
func TestNilGateGatesNothing(t *testing.T) {
	ctx := context.Background()
	server, gatedRan, _ := gatedTestServer(t, nil)
	cs := connectForGate(t, ctx, server)

	if names := listedNames(t, cs, ctx); !has(names, "gated_tool") {
		t.Fatalf("a nil gate hid a tool: %v", names)
	}
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "gated_tool"}); err != nil {
		t.Fatalf("a nil gate refused a call: %v", err)
	}
	if !*gatedRan {
		t.Fatal("the handler did not run under a nil gate")
	}
}

// TestEpisodicRecallCarriesItsGate proves the shipped tool is wired to the
// feature, at the surface a client actually reaches. Without it the mechanism
// could be perfect and attached to nothing.
func TestEpisodicRecallCarriesItsGate(t *testing.T) {
	ctx := context.Background()
	asked := map[string]int{}
	server, err := NewMCPServer(
		&stubResourceTypeService{}, &stubResourceService{}, nil, nil, stubEpisodicRecall{}, nil,
		func(_ context.Context, key string) bool {
			asked[key]++
			return false
		}, nil,
	)
	if err != nil {
		t.Fatalf("NewMCPServer: %v", err)
	}
	cs := connectForGate(t, ctx, server)

	names := listedNames(t, cs, ctx)
	if has(names, "episodic_recall") {
		t.Fatal("episodic_recall was offered while its feature was off")
	}
	if !has(names, "episodic_event_get") {
		t.Fatalf("an ungated companion tool went missing: %v", names)
	}
	if asked["episodic-recall"] == 0 {
		t.Fatalf("the gate was never asked about episodic-recall; it was asked about %v", asked)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "episodic_recall"})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !res.IsError || !strings.Contains(textFromResult(res), "episodic-recall") {
		t.Fatalf("calling episodic_recall was not refused: %v", textFromResult(res))
	}
}

func textFromResult(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// stubEpisodicRecall satisfies application.EpisodicRecall so the episodic tool
// group registers. Nothing calls through it: the gate refuses first, which is
// the point.
type stubEpisodicRecall struct{}

func (stubEpisodicRecall) Recall(
	context.Context, application.EpisodicQuery,
) (*application.EpisodicRecallResult, error) {
	return &application.EpisodicRecallResult{}, nil
}

func (stubEpisodicRecall) Similar(
	context.Context, application.SimilarQuery,
) (*application.SimilarResult, error) {
	return &application.SimilarResult{}, nil
}

func (stubEpisodicRecall) EventByURN(context.Context, string) (*application.FetchedEvent, error) {
	return &application.FetchedEvent{}, nil
}
