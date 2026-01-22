package memory

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	MaxElements   = 80
	MaxTextDigest = 20
	MaxTextLength = 120
)

type Memory struct {
	ephemeral      *EphemeralMemory
	workingSummary *WorkingSummary
	facts          *Facts
	maxEphemeral   int
	maxSummarySize int
}

type Step struct {
	ID        string      `json:"id"`
	Timestamp time.Time   `json:"timestamp"`
	Action    string      `json:"action"`
	Tool      string      `json:"tool,omitempty"`
	Target    interface{} `json:"target,omitempty"`
	Result    interface{} `json:"result,omitempty"`
	Error     string      `json:"error,omitempty"`
	State     *PageState  `json:"state,omitempty"`
}

type EphemeralMemory struct {
	Steps []Step `json:"steps"`
}

type WorkingSummary struct {
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Facts struct {
	URL            string                 `json:"url"`
	Goal           string                 `json:"goal"`
	CompletedSteps []string               `json:"completed_steps"`
	Blockers       []string               `json:"blockers"`
	Metadata       map[string]interface{} `json:"metadata"`
}

type PageState struct {
	URL        string    `json:"url"`
	Title      string    `json:"title"`
	Viewport   Viewport  `json:"viewport"`
	Overlays   []Overlay `json:"overlays"`
	Elements   []Element `json:"elements"`
	TextDigest []string  `json:"text_digest"`
	Timestamp  time.Time `json:"timestamp"`
}

type Viewport struct {
	Width  int `json:"w"`
	Height int `json:"h"`
}

type Overlay struct {
	Type string `json:"type"` // cookie, modal, drawer, toast, loading
	Text string `json:"text"`
}

type Element struct {
	EID         string `json:"eid"`
	Role        string `json:"role"`
	Name        string `json:"name,omitempty"`
	Text        string `json:"text,omitempty"`
	Label       string `json:"label,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Aria        string `json:"aria,omitempty"`
	Visible     bool   `json:"visible"`
	Disabled    bool   `json:"disabled"`
	BBox        []int  `json:"bbox"` // [x, y, w, h]
	Nearby      string `json:"nearby,omitempty"`
}

func New(maxEphemeral, maxSummarySize int) *Memory {
	return &Memory{
		ephemeral: &EphemeralMemory{
			Steps: make([]Step, 0, maxEphemeral),
		},
		workingSummary: &WorkingSummary{
			Content:   "",
			UpdatedAt: time.Now(),
		},
		facts: &Facts{
			CompletedSteps: make([]string, 0),
			Blockers:       make([]string, 0),
			Metadata:       make(map[string]interface{}),
		},
		maxEphemeral:   maxEphemeral,
		maxSummarySize: maxSummarySize,
	}
}

func (m *Memory) AddStep(step Step) {
	m.ephemeral.Steps = append(m.ephemeral.Steps, step)

	if len(m.ephemeral.Steps) > m.maxEphemeral {
		m.ephemeral.Steps = m.ephemeral.Steps[len(m.ephemeral.Steps)-m.maxEphemeral:]
	}

	m.updateWorkingSummary()
}

func (m *Memory) GetEphemeral() []Step {
	return m.ephemeral.Steps
}

func (m *Memory) GetWorkingSummary() string {
	return m.workingSummary.Content
}

func (m *Memory) GetFacts() *Facts {
	return m.facts
}

func (m *Memory) SetGoal(goal string) {
	m.facts.Goal = goal
}

func (m *Memory) SetURL(url string) {
	m.facts.URL = url
}

func (m *Memory) AddCompletedStep(step string) {
	m.facts.CompletedSteps = append(m.facts.CompletedSteps, step)
}

func (m *Memory) AddBlocker(blocker string) {
	m.facts.Blockers = append(m.facts.Blockers, blocker)
}

func (m *Memory) ClearBlockers() {
	m.facts.Blockers = m.facts.Blockers[:0]
}

func (m *Memory) UpdateMetadata(key string, value interface{}) {
	if m.facts.Metadata == nil {
		m.facts.Metadata = make(map[string]interface{})
	}
	m.facts.Metadata[key] = value
}

func (m *Memory) GetContextForLLM() string {
	var parts []string

	if m.facts.Goal != "" {
		parts = append(parts, fmt.Sprintf("Goal: %s", m.facts.Goal))
	}
	if m.facts.URL != "" {
		parts = append(parts, fmt.Sprintf("Current URL: %s", m.facts.URL))
	}
	if len(m.facts.CompletedSteps) > 0 {
		parts = append(parts, fmt.Sprintf("Completed: %v", m.facts.CompletedSteps))
	}
	if len(m.facts.Blockers) > 0 {
		parts = append(parts, fmt.Sprintf("Blockers: %v", m.facts.Blockers))
	}

	if m.workingSummary.Content != "" {
		parts = append(parts, fmt.Sprintf("Summary: %s", m.workingSummary.Content))
	}

	if len(m.ephemeral.Steps) > 0 {
		lastSteps := m.ephemeral.Steps
		if len(lastSteps) > 3 {
			lastSteps = lastSteps[len(lastSteps)-3:]
		}
		stepsJSON, _ := json.Marshal(lastSteps)
		parts = append(parts, fmt.Sprintf("Recent steps: %s", string(stepsJSON)))
	}

	result := ""
	for _, part := range parts {
		if len(result)+len(part)+1 > m.maxSummarySize {
			break
		}
		if result != "" {
			result += "\n"
		}
		result += part
	}

	return result
}

func (m *Memory) updateWorkingSummary() {
	if len(m.ephemeral.Steps) == 0 {
		return
	}

	var summary string
	for _, step := range m.ephemeral.Steps {
		if len(summary)+len(step.Action)+50 > m.maxSummarySize {
			break
		}
		if summary != "" {
			summary += ". "
		}
		summary += fmt.Sprintf("%s: %s", step.Action, step.Tool)
		if step.Error != "" {
			summary += fmt.Sprintf(" (error: %s)", step.Error)
		}
	}

	m.workingSummary.Content = summary
	m.workingSummary.UpdatedAt = time.Now()
}

func (m *Memory) TruncatePageState(state *PageState) *PageState {
	if state == nil {
		return state
	}

	if len(state.Elements) > MaxElements {
		state.Elements = state.Elements[:MaxElements]
	}

	if len(state.TextDigest) > MaxTextDigest {
		state.TextDigest = state.TextDigest[:MaxTextDigest]
	}

	for i, text := range state.TextDigest {
		if len(text) > MaxTextLength {
			state.TextDigest[i] = text[:MaxTextLength]
		}
	}

	return state
}
