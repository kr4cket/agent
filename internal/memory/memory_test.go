package memory

import (
	"testing"
	"time"
)

func TestMemory_AddStep(t *testing.T) {
	mem := New(3, 1000)

	step := Step{
		ID:        "step-1",
		Timestamp: time.Now(),
		Action:    "navigate",
		Tool:      "navigate",
	}

	mem.AddStep(step)

	steps := mem.GetEphemeral()
	if len(steps) != 1 {
		t.Errorf("Expected 1 step, got %d", len(steps))
	}

	if steps[0].ID != "step-1" {
		t.Errorf("Expected step ID 'step-1', got '%s'", steps[0].ID)
	}
}

func TestMemory_EphemeralLimit(t *testing.T) {
	mem := New(3, 1000)

	for i := 0; i < 5; i++ {
		step := Step{
			ID:        "step-" + string(rune(i)),
			Timestamp: time.Now(),
			Action:    "action",
			Tool:      "tool",
		}
		mem.AddStep(step)
	}

	steps := mem.GetEphemeral()
	if len(steps) > 3 {
		t.Errorf("Expected max 3 steps, got %d", len(steps))
	}
}

func TestMemory_SetGoal(t *testing.T) {
	mem := New(3, 1000)
	mem.SetGoal("test goal")

	facts := mem.GetFacts()
	if facts.Goal != "test goal" {
		t.Errorf("Expected goal 'test goal', got '%s'", facts.Goal)
	}
}

func TestMemory_TruncatePageState(t *testing.T) {
	mem := New(3, 1000)

	state := &PageState{
		Elements:   make([]Element, 100),
		TextDigest: make([]string, 30),
	}

	for i := 0; i < 100; i++ {
		state.Elements = append(state.Elements, Element{
			EID:  "e" + string(rune(i)),
			Role: "button",
		})
	}

	for i := 0; i < 30; i++ {
		state.TextDigest = append(state.TextDigest, "text "+string(rune(i)))
	}

	truncated := mem.TruncatePageState(state)

	if len(truncated.Elements) > MaxElements {
		t.Errorf("Expected max %d elements, got %d", MaxElements, len(truncated.Elements))
	}

	if len(truncated.TextDigest) > MaxTextDigest {
		t.Errorf("Expected max %d text digest lines, got %d", MaxTextDigest, len(truncated.TextDigest))
	}
}
