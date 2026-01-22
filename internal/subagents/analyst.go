package subagents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.uber.org/zap"
	"testTask/internal/llm"
	"testTask/internal/logging"
	"testTask/internal/memory"
)

type DOMAnalyst struct {
	llm    llm.LLMClient
	logger *zap.Logger
}

func NewDOMAnalyst(llm llm.LLMClient, logger *logging.Logger) *DOMAnalyst {
	return &DOMAnalyst{
		llm:    llm,
		logger: logger.Console(),
	}
}

func (a *DOMAnalyst) AnalyzePage(ctx context.Context, state *memory.PageState, query string, screenshotPath string) (*memory.PageState, error) {
	if screenshotPath != "" {
		return a.analyzeWithImage(ctx, state, query, screenshotPath)
	}

	return a.analyzeTextOnly(ctx, state, query)
}

func (a *DOMAnalyst) analyzeWithImage(ctx context.Context, state *memory.PageState, query string, screenshotPath string) (*memory.PageState, error) {
	prompt := a.buildPrompt(state, query)

	messages := []llm.Message{
		{
			Role:    "system",
			Content: a.getSystemPrompt(),
		},
		{
			Role:    "user",
			Content: prompt,
		},
	}

	_, err := a.llm.ChatWithImage(ctx, messages, screenshotPath, nil)
	if err != nil {
		a.logger.Warn("Image analysis failed, falling back to text-only", zap.Error(err))
		return a.analyzeTextOnly(ctx, state, query)
	}

	return state, nil
}

func (a *DOMAnalyst) analyzeTextOnly(ctx context.Context, state *memory.PageState, query string) (*memory.PageState, error) {
	prompt := a.buildPrompt(state, query)

	messages := []llm.Message{
		{
			Role:    "system",
			Content: a.getSystemPrompt(),
		},
		{
			Role:    "user",
			Content: prompt,
		},
	}

	resp, err := a.llm.Chat(ctx, messages, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze page: %w", err)
	}

	a.logger.Info("LLM DOM analysis response",
		zap.String("query", query),
		zap.String("full_response", resp.Content),
		zap.String("finish_reason", resp.FinishReason),
	)

	return state, nil
}

func (a *DOMAnalyst) getSystemPrompt() string {
	return `You are a DOM-Analyst subagent. Your task is to analyze the page state and provide insights about available elements and their structure.

Focus on:
- Identifying interactive elements (buttons, links, inputs)
- Understanding page structure
- Finding elements matching specific queries
- Detecting overlays, modals, cookie banners

Return your analysis as structured text describing the page state.`
}

func (a *DOMAnalyst) buildPrompt(state *memory.PageState, query string) string {
	prompt := fmt.Sprintf("Analyze the current page state:\n")
	prompt += fmt.Sprintf("- URL: %s\n", state.URL)
	prompt += fmt.Sprintf("- Title: %s\n", state.Title)

	if len(state.Elements) > 0 {
		prompt += fmt.Sprintf("\nAvailable elements (%d):\n", len(state.Elements))
		for i, elem := range state.Elements {
			if i >= 20 { // ограничиваем вывод
				prompt += fmt.Sprintf("... and %d more\n", len(state.Elements)-20)
				break
			}
			prompt += fmt.Sprintf("- [%s] %s (role: %s, text: %s)\n", elem.EID, elem.Name, elem.Role, elem.Text)
		}
	}

	if len(state.Overlays) > 0 {
		prompt += "\nOverlays detected:\n"
		for _, overlay := range state.Overlays {
			prompt += fmt.Sprintf("- %s: %s\n", overlay.Type, overlay.Text)
		}
	}

	if query != "" {
		prompt += fmt.Sprintf("\nFocus query: %s\n", query)
		prompt += "Identify elements that match this query."
	}

	return prompt
}

type ClickCoordinates struct {
	X           int    `json:"x"`
	Y           int    `json:"y"`
	Found       bool   `json:"found"`
	Reason      string `json:"reason,omitempty"`
	ElementText string `json:"element_text,omitempty"`
}

func (a *DOMAnalyst) FindClickCoordinates(ctx context.Context, screenshotPath string, target *memory.Target, description string, pageContext ...string) (*ClickCoordinates, error) {
	if screenshotPath == "" {
		return nil, fmt.Errorf("screenshot path is required")
	}

	prompt := a.buildClickPrompt(target, description, pageContext...)

	messages := []llm.Message{
		{
			Role:    "system",
			Content: a.getClickSystemPrompt(),
		},
		{
			Role:    "user",
			Content: prompt,
		},
	}

	resp, err := a.llm.ChatWithImage(ctx, messages, screenshotPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze screenshot: %w", err)
	}

	a.logger.Info("LLM vision analysis response",
		zap.String("screenshot", screenshotPath),
		zap.String("full_response", resp.Content),
		zap.String("finish_reason", resp.FinishReason),
	)

	coords, err := a.parseClickResponse(resp.Content)
	if err != nil {
		a.logger.Warn("Failed to parse click coordinates from LLM response, trying to extract from text",
			zap.Error(err),
			zap.String("response", resp.Content),
		)
		coords = a.extractCoordinatesFromText(resp.Content)
	}

	return coords, nil
}

type ProgressCheckResult struct {
	ProgressOk   bool   `json:"progress_ok"`
	WhatHappened string `json:"what_happened"`
	NextAction   string `json:"next_action"`
}

func (a *DOMAnalyst) CheckProgress(ctx context.Context, screenshotPath string, prompt string) (*ProgressCheckResult, error) {
	if screenshotPath == "" {
		return nil, fmt.Errorf("screenshot path is required")
	}

	messages := []llm.Message{
		{
			Role:    "system",
			Content: "You are a Progress Analyst. Your task is to analyze screenshots and determine if the agent's actions are progressing correctly toward the goal. Be critical and accurate in your assessment. Return your response as a JSON object with fields: progress_ok (boolean), what_happened (string), next_action (string).",
		},
		{
			Role:    "user",
			Content: prompt,
		},
	}

	resp, err := a.llm.ChatWithImage(ctx, messages, screenshotPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to check progress: %w", err)
	}

	a.logger.Info("LLM progress check response",
		zap.String("screenshot", screenshotPath),
		zap.String("full_response", resp.Content),
		zap.String("finish_reason", resp.FinishReason),
	)

	startIdx := strings.Index(resp.Content, "{")
	endIdx := strings.LastIndex(resp.Content, "}")
	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		return nil, fmt.Errorf("no JSON found in response: %s", resp.Content)
	}

	jsonStr := resp.Content[startIdx : endIdx+1]
	jsonStr = removeJSONComments(jsonStr)

	var result ProgressCheckResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse progress check result: %w, json: %s", err, jsonStr)
	}

	return &result, nil
}

func (a *DOMAnalyst) getClickSystemPrompt() string {
	return `You are an expert Vision Analyst specialized in web UI element detection and interaction. Your task is to analyze a screenshot of a web page and determine the exact pixel coordinates (x, y) where to click to interact with a specific element.

ANALYSIS METHODOLOGY:
1. First, scan the entire screenshot to understand the page structure (header, navigation, main content, footer)
2. Identify the general area where the target element should be located based on its role and context
3. Look for visual patterns that match the element type (input boxes, buttons, links)
4. Consider text variations and synonyms (e.g., "Поиск", "Найти", "Search", "Найти товары" all indicate search fields)
5. Verify the element is visible, clickable, and not obscured by overlays or popups
6. Calculate the center point of the clickable area

OUTPUT FORMAT:
You must return your response as a JSON object with the following structure:
{
  "x": <integer>,  // X coordinate in pixels (center of element)
  "y": <integer>,  // Y coordinate in pixels (center of element)
  "found": <boolean>,  // true if element was found, false otherwise
  "reason": "<string>",  // Optional: explanation if not found or details about what was found
  "element_text": "<string>"  // CRITICAL: The actual text visible on the element in the screenshot (e.g., "Найти", "Поиск", "Search", button text, placeholder text, etc.)
}

CRITICAL RULES FOR COORDINATES:
- Coordinates are in pixels, starting from top-left corner (0,0)
- ALWAYS click in the CENTER of the visible, clickable area of the element
- For input fields/textboxes: click in the center of the input box, not on placeholder text or border
- For buttons: click in the center of the button area, not on text or icon specifically
- For links: click in the center of the clickable text area
- Be EXTREMELY precise - coordinates will be used directly without adjustment
- Account for any padding or margins - click where a user would naturally click
- IMPORTANT: Verify coordinates are within the viewport (typically 0-1920 for width, 0-1080 for height)
- If element is partially visible, click on the visible part, not the hidden part

ELEMENT DETECTION STRATEGY:
- Text matching should be flexible: case-insensitive, partial matches, synonyms, and translations are acceptable
- Placeholder text may vary (e.g., "Поиск" vs "Найти товары" vs "Search") - focus on element type
- Element type (textbox, button, link) is more important than exact text match
- Look for visual patterns: search boxes usually have magnifying glass icons, buttons are typically rectangular with borders
- Consider page context: search boxes are typically in header/navigation, buttons can be anywhere
- If multiple similar elements exist, prefer the most prominent or first visible one
- Check for overlays, modals, or popups that might hide the element
- SCROLLING: If the target element is NOT visible in the screenshot (e.g. below the fold, above the viewport), return found: false and in "reason" state that the page should be scrolled to find it (e.g. "Element not visible in current view; scroll down/up to search for it"). Do NOT guess coordinates for off-screen elements.

COMMON ELEMENT PATTERNS:
- Search boxes: rectangular input fields, often with placeholder text, usually in header, may have search icon
- Buttons: rectangular elements with text/icon, often colored or bordered, clickable area
- Links: underlined or colored text, sometimes in navigation menus
- Form inputs: rectangular boxes, may have labels above or to the side`
}

func (a *DOMAnalyst) buildClickPrompt(target *memory.Target, description string, pageContext ...string) string {
	var prompt strings.Builder

	prompt.WriteString("=== SCREENSHOT ANALYSIS TASK ===\n\n")

	if len(pageContext) > 0 && pageContext[0] != "" {
		prompt.WriteString(fmt.Sprintf("PAGE CONTEXT: %s\n\n", pageContext[0]))
	}

	if description != "" {
		prompt.WriteString(fmt.Sprintf("ACTION TO PERFORM: %s\n\n", description))
	}

	prompt.WriteString("STEP-BY-STEP ANALYSIS:\n")
	prompt.WriteString("1. Examine the entire screenshot to understand the page layout\n")
	prompt.WriteString("2. Look for the target element anywhere on the page based on the characteristics below\n")
	prompt.WriteString("3. Verify the element is visible, clickable, and not obscured by overlays, popups, or dropdown menus\n")
	prompt.WriteString("4. Read the ACTUAL TEXT visible on the element in the screenshot\n")
	prompt.WriteString("5. Calculate the EXACT center coordinates of the clickable area (account for padding and margins)\n")
	prompt.WriteString("6. Double-check that coordinates are within the visible viewport (0-1920 for width, 0-1080 for height)\n\n")

	if target != nil {
		prompt.WriteString("=== TARGET ELEMENT SPECIFICATIONS ===\n\n")

		if target.Role != "" {
			prompt.WriteString(fmt.Sprintf("ELEMENT TYPE: %s\n", target.Role))

			switch target.Role {
			case "textbox":
				prompt.WriteString("\nSEARCH FOR: Text input field / Search box\n")
				prompt.WriteString("VISUAL CHARACTERISTICS:\n")
				prompt.WriteString("  • Rectangular input box, usually with rounded or square corners\n")
				prompt.WriteString("  • Often has a border (may be subtle or prominent)\n")
				prompt.WriteString("  • May contain placeholder text inside the box (e.g., 'Найти товары', 'Поиск', 'Search')\n")
				prompt.WriteString("  • Often has a magnifying glass icon (🔍) or search icon nearby or inside\n")
				prompt.WriteString("  • Typically located in the header/navigation bar at the TOP of the page\n")
				prompt.WriteString("  • On Yandex Market: Look for a YELLOW search bar in the header area\n")
				prompt.WriteString("  • On Yandex Market: The search box is usually CENTERED in the header, between logo and user icons\n")
				prompt.WriteString("  • May be centered or positioned on the left/right side\n")
				prompt.WriteString("  • Usually has a light background (white, gray, or colored like yellow on Yandex Market)\n")
				prompt.WriteString("  • IMPORTANT: Search boxes are almost ALWAYS in the TOP 100-150 pixels of the page\n")
				prompt.WriteString("  • IMPORTANT: Look for the LARGEST, MOST PROMINENT search box in the header area\n")
				prompt.WriteString("COMMON PLACEHOLDER VARIATIONS:\n")
				prompt.WriteString("  • Placeholder text may vary: 'Найти товары', 'Поиск по каталогу', 'Поиск товаров', 'Search', 'Find products'\n")
				prompt.WriteString("  • Look for any text that suggests searching or finding content\n")
				prompt.WriteString("  • Focus on the element type (textbox) rather than exact placeholder text\n")
				prompt.WriteString("  • On Yandex Market specifically: Look for yellow search bar with placeholder 'Найти товары'\n")

			case "button":
				prompt.WriteString("\nSEARCH FOR: Clickable button\n")
				prompt.WriteString("VISUAL CHARACTERISTICS:\n")
				prompt.WriteString("  • Rectangular or rounded rectangular shape\n")
				prompt.WriteString("  • Usually has a colored background (blue, green, yellow, etc.) or border\n")
				prompt.WriteString("  • Contains text, icon, or both\n")
				prompt.WriteString("  • Often has hover effects or appears elevated\n")
				prompt.WriteString("  • May be part of a form, navigation, or action area\n")
				prompt.WriteString("  • Can be located ANYWHERE on the page - look at the entire screenshot\n")
				prompt.WriteString("CRITICAL: TEXT READING:\n")
				prompt.WriteString("  • You MUST read the ACTUAL TEXT visible on the button in the screenshot\n")
				prompt.WriteString("  • Include this text in the 'element_text' field of your response\n")
				prompt.WriteString("  • The text may be in any language (Russian, English, etc.)\n")
				prompt.WriteString("  • Read the text exactly as it appears in the screenshot\n")

			case "link":
				prompt.WriteString("\nSEARCH FOR: Clickable link\n")
				prompt.WriteString("VISUAL CHARACTERISTICS:\n")
				prompt.WriteString("  • Usually underlined text or text with distinct color\n")
				prompt.WriteString("  • May be in navigation menus, breadcrumbs, or content area\n")
				prompt.WriteString("  • Often blue or colored differently from regular text\n")

			default:
				prompt.WriteString(fmt.Sprintf("\nSEARCH FOR: Element with role '%s'\n", target.Role))
			}
			prompt.WriteString("\n")
		}

		if target.Name != "" {
			prompt.WriteString(fmt.Sprintf("ELEMENT TEXT/NAME: '%s'\n", target.Name))
			prompt.WriteString("TEXT MATCHING RULES:\n")
			prompt.WriteString("  • Look for exact match first\n")
			prompt.WriteString("  • If not found, try case-insensitive match\n")
			prompt.WriteString("  • Try partial matches (element contains this text)\n")
			prompt.WriteString("  • Consider synonyms and translations\n")
			prompt.WriteString("  • For Russian sites: 'Найти' ≈ 'Поиск' ≈ 'Search'\n")
			prompt.WriteString("\n")
		}

		if target.TextContains != "" {
			prompt.WriteString(fmt.Sprintf("ELEMENT CONTAINS TEXT: '%s'\n", target.TextContains))
			prompt.WriteString("  • The element should contain this text (partial match is acceptable)\n")
			prompt.WriteString("  • Text can be in button label, link text, or nearby label\n")
			prompt.WriteString("\n")
		}

		if target.Placeholder != "" {
			prompt.WriteString(fmt.Sprintf("PLACEHOLDER TEXT: '%s'\n", target.Placeholder))
			prompt.WriteString("PLACEHOLDER MATCHING:\n")
			prompt.WriteString("  • This is hint text inside an input field\n")
			prompt.WriteString("  • The actual placeholder may differ slightly\n")
			prompt.WriteString("  • Common variations:\n")
			prompt.WriteString("    - 'Поиск' → may appear as 'Найти товары', 'Поиск по сайту', 'Search'\n")
			prompt.WriteString("    - 'Search' → may appear as 'Поиск', 'Найти', 'Искать'\n")
			prompt.WriteString("  • Focus on finding ANY search input field if placeholder doesn't match exactly\n")
			prompt.WriteString("  • The element type (textbox) is more important than exact placeholder text\n")
			prompt.WriteString("\n")
		}

		if target.Label != "" {
			prompt.WriteString(fmt.Sprintf("LABEL: '%s'\n", target.Label))
			prompt.WriteString("  • Look for associated label text near the element\n")
			prompt.WriteString("\n")
		}

		if target.Nth > 0 {
			prompt.WriteString(fmt.Sprintf("ELEMENT INDEX: %d (if multiple matches, use the %d-th one)\n\n", target.Nth, target.Nth))
		}
	}

	prompt.WriteString("=== SEARCH STRATEGY ===\n\n")
	prompt.WriteString("1. HEADER/NAVIGATION AREA (top of page):\n")
	prompt.WriteString("   • Most search boxes are located here\n")
	prompt.WriteString("   • Look for input fields with search-related icons\n")
	prompt.WriteString("   • Check center, left, and right areas of the header\n\n")

	prompt.WriteString("2. MAIN CONTENT AREA:\n")
	prompt.WriteString("   • Buttons and links can be anywhere\n")
	prompt.WriteString("   • Look for prominent, visible elements first\n")
	prompt.WriteString("   • Check for colored or bordered elements\n\n")

	prompt.WriteString("3. VISIBILITY CHECK:\n")
	prompt.WriteString("   • Element must be fully visible (not cut off or hidden)\n")
	prompt.WriteString("   • Not obscured by overlays, popups, or modals\n")
	prompt.WriteString("   • Not disabled or grayed out\n\n")

	prompt.WriteString("=== COORDINATE CALCULATION ===\n\n")
	prompt.WriteString("When you find the element:\n")
	prompt.WriteString("1. Identify the clickable area (the entire element, not just text/icon)\n")
	prompt.WriteString("2. Find the geometric center of that area\n")
	prompt.WriteString("3. For input fields: center of the input box\n")
	prompt.WriteString("4. For buttons: center of the button rectangle\n")
	prompt.WriteString("5. Account for any padding - click where a user would naturally click\n\n")

	prompt.WriteString("=== OUTPUT ===\n\n")
	prompt.WriteString("Return your answer as a JSON object:\n")
	prompt.WriteString("{\n")
	prompt.WriteString("  \"x\": <integer>,  // Center X coordinate in pixels\n")
	prompt.WriteString("  \"y\": <integer>,  // Center Y coordinate in pixels\n")
	prompt.WriteString("  \"found\": <boolean>,  // true if element found, false otherwise\n")
	prompt.WriteString("  \"reason\": \"<string>\"  // Brief explanation (required if found=false)\n")
	prompt.WriteString("}\n\n")

	prompt.WriteString("If found=true: provide exact pixel coordinates of the center point.\n")
	prompt.WriteString("If found=false: explain why in the 'reason' field (e.g., 'Element not visible', 'No matching element found', 'Element obscured by overlay', 'Element not in view—scroll down/up to find it'). When the element is likely off-screen, always suggest scrolling the page to search for it.\n\n")
	prompt.WriteString("REMINDER: The page can be scrolled. If the element is not visible in the screenshot, return found: false and indicate that the agent should scroll (scroll down/up) to continue searching.")

	return prompt.String()
}

func (a *DOMAnalyst) parseClickResponse(response string) (*ClickCoordinates, error) {
	response = strings.TrimSpace(response)

	startIdx := strings.Index(response, "{")
	endIdx := strings.LastIndex(response, "}")
	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		return nil, fmt.Errorf("no JSON found in response")
	}

	jsonStr := response[startIdx : endIdx+1]

	lines := strings.Split(jsonStr, "\n")
	var cleanedLines []string
	for _, line := range lines {
		if commentIdx := strings.Index(line, "//"); commentIdx != -1 {
			beforeComment := line[:commentIdx]
			quoteCount := strings.Count(beforeComment, `"`) - strings.Count(beforeComment, `\"`)
			if quoteCount%2 == 0 {
				line = strings.TrimSpace(beforeComment)
				if line != "" && !strings.HasSuffix(line, ",") {
				}
			}
		}
		line = strings.TrimSpace(line)
		if line != "" {
			cleanedLines = append(cleanedLines, line)
		}
	}
	jsonStr = strings.Join(cleanedLines, "\n")

	var coords ClickCoordinates
	if err := json.Unmarshal([]byte(jsonStr), &coords); err != nil {
		jsonStr = removeJSONComments(response[startIdx : endIdx+1])
		if err2 := json.Unmarshal([]byte(jsonStr), &coords); err2 != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w (original: %v)", err2, err)
		}
	}

	return &coords, nil
}

func removeJSONComments(jsonStr string) string {
	var result strings.Builder
	inString := false
	escapeNext := false

	for i := 0; i < len(jsonStr); i++ {
		char := jsonStr[i]

		if escapeNext {
			result.WriteByte(char)
			escapeNext = false
			continue
		}

		if char == '\\' {
			escapeNext = true
			result.WriteByte(char)
			continue
		}

		if char == '"' {
			inString = !inString
			result.WriteByte(char)
			continue
		}

		if !inString && char == '/' && i+1 < len(jsonStr) {
			nextChar := jsonStr[i+1]
			if nextChar == '/' {
				for i < len(jsonStr) && jsonStr[i] != '\n' {
					i++
				}
				continue
			} else if nextChar == '*' {
				i += 2
				for i+1 < len(jsonStr) {
					if jsonStr[i] == '*' && jsonStr[i+1] == '/' {
						i += 2
						break
					}
					i++
				}
				continue
			}
		}

		result.WriteByte(char)
	}

	return result.String()
}

func (a *DOMAnalyst) extractCoordinatesFromText(response string) *ClickCoordinates {
	coords := &ClickCoordinates{Found: false}

	coords.Reason = "Could not extract coordinates from LLM response"

	return coords
}
