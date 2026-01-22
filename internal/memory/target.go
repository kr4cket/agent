package memory

type Target struct {
	Role         string   `json:"role"`
	Name         string   `json:"name,omitempty"`
	TextContains string   `json:"text_contains,omitempty"`
	Label        string   `json:"label,omitempty"`
	Placeholder  string   `json:"placeholder,omitempty"`
	Nth          int      `json:"nth"`
	Hints        []string `json:"hints,omitempty"`
	NearbyText   string   `json:"nearby_text,omitempty"`
}
