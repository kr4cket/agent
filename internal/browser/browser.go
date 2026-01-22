package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
	"go.uber.org/zap"
	"testTask/internal/config"
	"testTask/internal/logging"
	"testTask/internal/memory"
)

type Browser interface {
	Navigate(ctx context.Context, url string) error
	Observe(ctx context.Context, focused bool, query string) (*memory.PageState, error)
	Click(ctx context.Context, target *memory.Target) error
	ClickAt(ctx context.Context, x, y int) error
	Type(ctx context.Context, target *memory.Target, text string) error
	TypeAt(ctx context.Context, x, y int, text string) error
	Press(ctx context.Context, key string) error
	Scroll(ctx context.Context, direction string, amount int) error
	Wait(ctx context.Context, condition string, timeout time.Duration) error
	Back(ctx context.Context) error
	Screenshot(ctx context.Context, path string) error
	GetURL(ctx context.Context) (string, error)
	GetTitle(ctx context.Context) (string, error)
	Evaluate(ctx context.Context, script string, result interface{}) error
	Close(ctx context.Context) error
}

type PlaywrightBrowser struct {
	pw      *playwright.Playwright
	browser playwright.Browser
	page    playwright.Page
	ctx     playwright.BrowserContext
	config  *config.Config
	logger  *zap.Logger
}

func (b *PlaywrightBrowser) ensurePage() error {
	if b.ctx == nil {
		return fmt.Errorf("browser context is not initialized")
	}

	if b.page == nil || b.page.IsClosed() {
		p, err := b.ctx.NewPage()
		if err != nil {
			return fmt.Errorf("failed to create page: %w", err)
		}
		p.SetDefaultTimeout(float64(b.config.Browser.NavigationTimeout.Milliseconds()))
		b.page = p
		b.logger.Info("Created new page for browser context")
	}

	return nil
}

func New(cfg *config.Config, logger *logging.Logger) (Browser, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to start playwright: %w", err)
	}

	launchTimeout := cfg.Browser.Timeout
	if launchTimeout < 60*time.Second {
		launchTimeout = 60 * time.Second
	}

	args := []string{
		"--disable-blink-features=AutomationControlled",
		"--exclude-switches=enable-automation",
		"--disable-dev-shm-usage",
		"--no-sandbox",
		"--disable-setuid-sandbox",
	}

	opts := playwright.BrowserTypeLaunchPersistentContextOptions{
		Headless: playwright.Bool(cfg.Browser.Headless),
		Channel:  playwright.String("chrome"),
		Viewport: &playwright.Size{
			Width:  cfg.Browser.ViewportWidth,
			Height: cfg.Browser.ViewportHeight,
		},
		Timeout: playwright.Float(launchTimeout.Seconds() * 1000),
		Args:    args,
	}

	profileDir := cfg.Browser.ProfileDir
	if !filepath.IsAbs(profileDir) {
		absPath, err := filepath.Abs(profileDir)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve profile dir: %w", err)
		}
		profileDir = absPath
	}
	profileDir = filepath.Clean(profileDir)

	logger.Console().Info("Launching browser",
		zap.String("profile_dir", profileDir),
		zap.Bool("headless", cfg.Browser.Headless),
		zap.Float64("timeout_seconds", launchTimeout.Seconds()),
	)

	ctx, err := pw.Chromium.LaunchPersistentContext(profileDir, opts)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "target closed") || strings.Contains(errMsg, "Target page, context or browser has been closed") {
			return nil, fmt.Errorf("failed to launch persistent context: Chrome is already running with this profile (%s). Please close all Chrome windows and try again. Error: %w", profileDir, err)
		}
		return nil, fmt.Errorf("failed to launch persistent context (profile: %s): %w. Make sure Chrome is closed and the profile path is correct", profileDir, err)
	}

	initScript := `
		Object.defineProperty(navigator, 'webdriver', {
			get: () => undefined
		});
		delete window.cdc_adoQpoasnfa76pfcZLmcfl_Array;
		delete window.cdc_adoQpoasnfa76pfcZLmcfl_Promise;
		delete window.cdc_adoQpoasnfa76pfcZLmcfl_Symbol;
	`
	err = ctx.AddInitScript(playwright.Script{
		Content: playwright.String(initScript),
	})
	if err != nil {
		logger.Console().Warn("Failed to add init script for automation detection bypass", zap.Error(err))
	}

	var page playwright.Page
	pages := ctx.Pages()
	if len(pages) > 0 {
		page = pages[0]
		logger.Console().Info("Using existing page from persistent context", zap.Int("page_count", len(pages)))
	} else {
		page, err = ctx.NewPage()
		if err != nil {
			return nil, fmt.Errorf("failed to create page: %w", err)
		}
		logger.Console().Info("Created new page for persistent context")
	}

	page.SetDefaultTimeout(float64(cfg.Browser.NavigationTimeout.Milliseconds()))

	return &PlaywrightBrowser{
		pw:      pw,
		browser: nil,
		page:    page,
		ctx:     ctx,
		config:  cfg,
		logger:  logger.Console(),
	}, nil
}

func (b *PlaywrightBrowser) Navigate(ctx context.Context, url string) error {
	if err := b.ensurePage(); err != nil {
		return fmt.Errorf("navigate failed: %w", err)
	}

	currentURL := b.page.URL()
	b.logger.Info("Navigating",
		zap.String("url", url),
		zap.String("current_url", currentURL),
	)

	normalizeURL := func(u string) string {
		u = strings.ToLower(strings.TrimSpace(u))
		if strings.HasSuffix(u, "/") && len(u) > 1 {
			u = u[:len(u)-1]
		}
		return u
	}

	normalizedCurrent := normalizeURL(currentURL)
	normalizedTarget := normalizeURL(url)

	if normalizedCurrent == normalizedTarget {
		b.logger.Info("Already on target URL, skipping navigation",
			zap.String("url", url),
		)
		return nil
	}

	if currentURL == "about:blank" || currentURL == "" {
		b.logger.Info("Page is blank, will navigate to target URL")
	}

	_, err := b.page.Goto(url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(float64(b.config.Browser.NavigationTimeout.Milliseconds())),
	})
	if err != nil {
		if strings.Contains(err.Error(), "timeout") {
			actualURL := b.page.URL()
			if normalizeURL(actualURL) == normalizedTarget {
				b.logger.Warn("Navigation timeout, but page loaded correctly",
					zap.String("target_url", url),
					zap.String("actual_url", actualURL),
				)
				return nil
			}
		}
		return fmt.Errorf("navigation failed: %w", err)
	}

	b.logger.Info("Navigation successful", zap.String("url", url))
	return nil
}

func (b *PlaywrightBrowser) Observe(ctx context.Context, focused bool, query string) (*memory.PageState, error) {
	if err := b.ensurePage(); err != nil {
		return nil, fmt.Errorf("observe failed: %w", err)
	}

	b.logger.Debug("Starting observe", zap.Bool("focused", focused), zap.String("query", query))

	observeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := b.page.URL()
	b.logger.Debug("Current URL", zap.String("url", url))

	title, _ := b.page.Title()

	viewport := b.page.ViewportSize()
	if viewport == nil {
		viewport = &playwright.Size{
			Width:  b.config.Browser.ViewportWidth,
			Height: b.config.Browser.ViewportHeight,
		}
	}

	state := &memory.PageState{
		URL:   url,
		Title: title,
		Viewport: memory.Viewport{
			Width:  viewport.Width,
			Height: viewport.Height,
		},
		Overlays:   []memory.Overlay{},
		Elements:   []memory.Element{},
		TextDigest: []string{},
		Timestamp:  time.Now(),
	}

	if url == "about:blank" || url == "" {
		b.logger.Info("Observing blank page, skipping element extraction", zap.String("url", url))
		return state, nil
	}

	done := make(chan struct{}, 1)
	var locators []playwright.Locator
	var locErr error

	go func() {
		defer func() {
			if r := recover(); r != nil {
				b.logger.Warn("Panic in locator extraction", zap.Any("error", r))
				locErr = fmt.Errorf("panic: %v", r)
			}
			done <- struct{}{}
		}()
		locators, locErr = b.page.Locator("button, a, input, select, textarea, [role]").All()
	}()

	select {
	case <-done:
	case <-observeCtx.Done():
		b.logger.Warn("Observe timeout while getting locators", zap.Error(observeCtx.Err()))
		return state, nil
	}

	if locErr != nil {
		b.logger.Warn("Failed to get locators", zap.Error(locErr))
	} else {
		maxElements := memory.MaxElements
		if maxElements > 30 {
			maxElements = 30
		}

		for i, loc := range locators {
			if i >= maxElements {
				break
			}

			if observeCtx.Err() != nil {
				b.logger.Warn("Observe timeout, stopping element extraction", zap.Int("extracted", i))
				break
			}

			element, err := b.extractElement(loc, i)
			if err != nil {
				continue
			}

			if element.Visible {
				state.Elements = append(state.Elements, *element)
			}
		}
	}

	if observeCtx.Err() == nil {
		textContent, err := b.page.TextContent("body")
		if err == nil && textContent != "" {
			lines := splitText(textContent, memory.MaxTextDigest, memory.MaxTextLength)
			state.TextDigest = lines
		}
	}

	if observeCtx.Err() == nil {
		b.detectOverlays(observeCtx, state)
	}

	return state, nil
}

func (b *PlaywrightBrowser) extractElement(loc playwright.Locator, index int) (*memory.Element, error) {
	visible, _ := loc.IsVisible()
	if !visible {
		return nil, fmt.Errorf("element not visible")
	}

	disabled, _ := loc.IsDisabled()

	text, _ := loc.TextContent()

	bbox, err := loc.BoundingBox()
	if err != nil {
		return nil, err
	}

	role, _ := loc.GetAttribute("role")
	if role == "" {
		var tagName string
		loc.Evaluate("el => el.tagName.toLowerCase()", &tagName)
		switch tagName {
		case "button":
			role = "button"
		case "a":
			role = "link"
		case "input":
			role = "textbox"
		case "select":
			role = "combobox"
		default:
			role = "generic"
		}
	}

	name, _ := loc.GetAttribute("name")
	label, _ := loc.GetAttribute("aria-label")
	placeholder, _ := loc.GetAttribute("placeholder")

	eid := fmt.Sprintf("e%d", index)

	return &memory.Element{
		EID:         eid,
		Role:        role,
		Name:        name,
		Text:        truncate(text, 100),
		Label:       label,
		Placeholder: placeholder,
		Visible:     visible,
		Disabled:    disabled,
		BBox: []int{
			int(bbox.X),
			int(bbox.Y),
			int(bbox.Width),
			int(bbox.Height),
		},
	}, nil
}

func (b *PlaywrightBrowser) detectOverlays(ctx context.Context, state *memory.PageState) {
	modal := b.page.Locator("[role='dialog'], .modal, .cookie-banner, [class*='cookie'], [class*='modal']")
	count, _ := modal.Count()
	if count > 0 {
		text, _ := modal.First().TextContent()
		state.Overlays = append(state.Overlays, memory.Overlay{
			Type: "modal",
			Text: truncate(text, 200),
		})
	}
}

func (b *PlaywrightBrowser) Click(ctx context.Context, target *memory.Target) error {
	if err := b.ensurePage(); err != nil {
		return fmt.Errorf("click failed: %w", err)
	}

	b.logger.Info("Clicking", zap.Any("target", target))

	loc := b.buildLocator(target)

	timeoutMs := float64(b.config.Browser.ActionTimeout.Milliseconds())
	if timeoutMs == 0 {
		timeoutMs = 60000 // Fallback: 60 секунд
	}

	err := loc.Click(playwright.LocatorClickOptions{
		Timeout: playwright.Float(timeoutMs),
	})
	if err != nil {
		return fmt.Errorf("click failed: %w", err)
	}

	return nil
}

func (b *PlaywrightBrowser) ClickAt(ctx context.Context, x, y int) error {
	if err := b.ensurePage(); err != nil {
		return fmt.Errorf("clickAt failed: %w", err)
	}

	b.logger.Info("Clicking at coordinates", zap.Int("x", x), zap.Int("y", y))

	mouse := b.page.Mouse()
	err := mouse.Click(float64(x), float64(y))
	if err != nil {
		return fmt.Errorf("click at coordinates failed: %w", err)
	}

	return nil
}

func (b *PlaywrightBrowser) Type(ctx context.Context, target *memory.Target, text string) error {
	if err := b.ensurePage(); err != nil {
		return fmt.Errorf("type failed: %w", err)
	}

	b.logger.Info("Typing", zap.Any("target", target), zap.String("text", text))

	loc := b.buildLocator(target)

	if loc == nil {
		return fmt.Errorf("failed to build locator for target: locator is nil")
	}

	count, err := loc.Count()
	if err != nil {
		return fmt.Errorf("failed to count locators: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("failed to find element with target: role=%s, placeholder=%s, name=%s",
			target.Role, target.Placeholder, target.Name)
	}
	if count > 1 {
		b.logger.Warn("Multiple elements found, using first", zap.Int("count", count))
		loc = loc.First()
	}

	waitTimeoutMs := float64(b.config.Browser.ActionTimeout.Milliseconds())
	if waitTimeoutMs == 0 {
		waitTimeoutMs = 60000
	}
	if waitTimeoutMs < 5000 {
		waitTimeoutMs = 5000 // Минимум 5 секунд для WaitFor
	}

	if err := loc.WaitFor(playwright.LocatorWaitForOptions{Timeout: playwright.Float(waitTimeoutMs)}); err != nil {
		return fmt.Errorf("failed to wait for element: %w", err)
	}

	timeoutMs := float64(b.config.Browser.ActionTimeout.Milliseconds())
	if timeoutMs == 0 {
		timeoutMs = 60000 // Fallback: 60 секунд
	}

	err = loc.Fill(text, playwright.LocatorFillOptions{
		Timeout: playwright.Float(timeoutMs),
	})
	if err != nil {
		return fmt.Errorf("type failed: %w", err)
	}

	return nil
}

func (b *PlaywrightBrowser) TypeAt(ctx context.Context, x, y int, text string) error {
	if err := b.ensurePage(); err != nil {
		return fmt.Errorf("typeAt failed: %w", err)
	}

	b.logger.Info("Typing at coordinates", zap.Int("x", x), zap.Int("y", y), zap.String("text", text))

	mouse := b.page.Mouse()
	if err := mouse.Click(float64(x), float64(y)); err != nil {
		return fmt.Errorf("failed to click at coordinates: %w", err)
	}

	kb := b.page.Keyboard()
	if err := kb.Type(text); err != nil {
		return fmt.Errorf("failed to type text: %w", err)
	}

	return nil
}

func (b *PlaywrightBrowser) Press(ctx context.Context, key string) error {
	if err := b.ensurePage(); err != nil {
		return fmt.Errorf("press failed: %w", err)
	}

	b.logger.Info("Pressing key", zap.String("key", key))
	kb := b.page.Keyboard()
	return kb.Press(key)
}

func (b *PlaywrightBrowser) Scroll(ctx context.Context, direction string, amount int) error {
	if err := b.ensurePage(); err != nil {
		return fmt.Errorf("scroll failed: %w", err)
	}

	b.logger.Info("Scrolling", zap.String("direction", direction), zap.Int("amount", amount))

	mouse := b.page.Mouse()
	if direction == "down" {
		mouse.Wheel(0, float64(amount))
	} else if direction == "up" {
		mouse.Wheel(0, -float64(amount))
	}

	return nil
}

func (b *PlaywrightBrowser) Wait(ctx context.Context, condition string, timeout time.Duration) error {
	if err := b.ensurePage(); err != nil {
		return fmt.Errorf("wait failed: %w", err)
	}

	b.logger.Info("Waiting", zap.String("condition", condition))

	to := timeout
	if to > 30*time.Second {
		to = 30 * time.Second
	}

	time.Sleep(to)
	return nil
}

func (b *PlaywrightBrowser) Back(ctx context.Context) error {
	if err := b.ensurePage(); err != nil {
		return fmt.Errorf("back failed: %w", err)
	}

	b.logger.Info("Going back")
	_, err := b.page.GoBack(playwright.PageGoBackOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	})
	return err
}

func (b *PlaywrightBrowser) Screenshot(ctx context.Context, path string) error {
	if err := b.ensurePage(); err != nil {
		return fmt.Errorf("screenshot failed: %w", err)
	}

	_, err := b.page.Screenshot(playwright.PageScreenshotOptions{
		Path:     playwright.String(path),
		FullPage: playwright.Bool(false),
	})
	if err != nil {
		return fmt.Errorf("screenshot failed: %w", err)
	}
	b.logger.Info("Screenshot saved", zap.String("path", path))
	return nil
}

func (b *PlaywrightBrowser) GetURL(ctx context.Context) (string, error) {
	if err := b.ensurePage(); err != nil {
		return "", fmt.Errorf("getURL failed: %w", err)
	}
	return b.page.URL(), nil
}

func (b *PlaywrightBrowser) GetTitle(ctx context.Context) (string, error) {
	if err := b.ensurePage(); err != nil {
		return "", fmt.Errorf("getTitle failed: %w", err)
	}
	return b.page.Title()
}

func (b *PlaywrightBrowser) Evaluate(ctx context.Context, script string, result interface{}) error {
	if err := b.ensurePage(); err != nil {
		return fmt.Errorf("evaluate failed: %w", err)
	}

	evalResult, err := b.page.Evaluate(script)
	if err != nil {
		return fmt.Errorf("failed to evaluate script: %w", err)
	}

	if result == nil {
		return fmt.Errorf("result pointer is nil")
	}

	if resultMap, ok := result.(*map[string]interface{}); ok {
		if evalMap, ok := evalResult.(map[string]interface{}); ok {
			*resultMap = evalMap
		} else {
			jsonBytes, err := json.Marshal(evalResult)
			if err != nil {
				return fmt.Errorf("failed to marshal result: %w", err)
			}
			if err := json.Unmarshal(jsonBytes, resultMap); err != nil {
				return fmt.Errorf("failed to unmarshal result: %w", err)
			}
		}
	} else {
		jsonBytes, err := json.Marshal(evalResult)
		if err != nil {
			return fmt.Errorf("failed to marshal result: %w", err)
		}
		if err := json.Unmarshal(jsonBytes, result); err != nil {
			return fmt.Errorf("failed to unmarshal result: %w", err)
		}
	}

	return nil
}

func (b *PlaywrightBrowser) Close(ctx context.Context) error {
	if b.ctx != nil {
		if err := b.ctx.Close(); err != nil {
			return fmt.Errorf("failed to close context: %w", err)
		}
	}
	if b.pw != nil {
		if err := b.pw.Stop(); err != nil {
			return fmt.Errorf("failed to stop playwright: %w", err)
		}
	}
	return nil
}

func (b *PlaywrightBrowser) buildLocator(target *memory.Target) playwright.Locator {
	if target == nil {
		var empty playwright.Locator
		return empty
	}

	b.logger.Debug("Building locator",
		zap.String("role", target.Role),
		zap.String("name", target.Name),
		zap.String("placeholder", target.Placeholder),
		zap.String("label", target.Label),
		zap.String("text_contains", target.TextContains),
		zap.Int("nth", target.Nth),
	)

	var loc playwright.Locator

	if target.Role != "" && target.Role != "generic" && target.Name != "" {
		loc = b.page.GetByRole(playwright.AriaRole(target.Role))
		textLoc := b.page.GetByText(target.Name)
		count, _ := textLoc.Count()
		if count > 0 {
			loc = textLoc.First()
			b.logger.Debug("Using getByText with first match for name",
				zap.String("role", target.Role),
				zap.String("name", target.Name),
				zap.Int("count", count),
			)
		} else {
			loc = b.page.GetByRole(playwright.AriaRole(target.Role)).First()
			b.logger.Debug("Using getByRole with First() as fallback", zap.String("role", target.Role), zap.String("name", target.Name))
		}
	} else if target.Placeholder != "" {
		loc = b.page.GetByPlaceholder(target.Placeholder)
		b.logger.Debug("Using getByPlaceholder", zap.String("placeholder", target.Placeholder))

		count, countErr := loc.Count()
		if countErr != nil || count == 0 {
			b.logger.Debug("getByPlaceholder found nothing, trying CSS selector with partial match",
				zap.String("placeholder", target.Placeholder),
			)
			selector := fmt.Sprintf("input[placeholder*='%s'], textarea[placeholder*='%s'], [role='textbox'][placeholder*='%s']",
				escapeCSS(target.Placeholder),
				escapeCSS(target.Placeholder),
				escapeCSS(target.Placeholder))
			loc = b.page.Locator(selector)
			count, countErr = loc.Count()
			if countErr != nil || count == 0 {
				if target.Role == "textbox" {
					b.logger.Debug("Placeholder not found, trying to find textbox by role only",
						zap.String("placeholder", target.Placeholder),
					)
					loc = b.page.GetByRole(playwright.AriaRole("textbox"))
					count, _ = loc.Count()
					if count > 0 {
						loc = loc.First()
						b.logger.Debug("Found textbox by role, using first match", zap.Int("count", count))
					}
				}
			}

			count, countErr = loc.Count()
			if countErr != nil || count == 0 {
				b.logger.Debug("CSS selector with partial match found nothing, trying role-based search",
					zap.String("placeholder", target.Placeholder),
				)
				if target.Role == "textbox" || target.Role == "" {
					loc = b.page.GetByRole(playwright.AriaRole("textbox"))
					count, _ = loc.Count()
					if count > 0 {
						loc = loc.First()
						b.logger.Debug("Found textbox by role, using first match", zap.Int("count", count))
					}
				}
			}
		}
	} else if target.Label != "" {
		loc = b.page.GetByLabel(target.Label)
		b.logger.Debug("Using getByLabel", zap.String("label", target.Label))
	} else if target.Role != "" && target.Role != "generic" && target.TextContains != "" {
		textLoc := b.page.GetByText(target.TextContains)
		count, _ := textLoc.Count()
		if count > 0 {
			loc = textLoc.First()
			b.logger.Debug("Using getByText with first match",
				zap.String("role", target.Role),
				zap.String("text", target.TextContains),
				zap.Int("count", count),
			)
		} else {
			if target.Role == "link" {
				loc = b.page.GetByRole(playwright.AriaRole("link"))
				loc = loc.First()
				b.logger.Debug("Using getByRole(link).First() as fallback", zap.String("text", target.TextContains))
			} else {
				selector := b.buildSelector(target)
				if selector != "" {
					loc = b.page.Locator(selector).First()
				} else {
					loc = b.page.GetByRole(playwright.AriaRole(target.Role)).First()
				}
				b.logger.Debug("Using role-based selector as fallback", zap.String("role", target.Role), zap.String("text", target.TextContains))
			}
		}
	} else {
		selector := b.buildSelector(target)
		if selector == "" {
			b.logger.Warn("Failed to build selector, returning empty locator")
			var empty playwright.Locator
			return empty
		}
		loc = b.page.Locator(selector)
		b.logger.Debug("Using CSS selector fallback", zap.String("selector", selector))
	}

	if target.TextContains != "" && (target.Name == "" || target.Role == "" || target.Role == "generic") {
		b.logger.Debug("TextContains specified but already using locator", zap.String("text", target.TextContains))
	}

	if target.Nth > 0 {
		loc = loc.Nth(target.Nth)
	} else {
		if target.Role == "link" || target.Role == "" {
			count, _ := loc.Count()
			if count > 1 {
				loc = loc.First()
				b.logger.Debug("Multiple elements found, using First()",
					zap.String("role", target.Role),
					zap.Int("count", count),
				)
			}
		}
	}

	return loc
}

func escapeCSS(s string) string {
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

func (b *PlaywrightBrowser) buildSelector(target *memory.Target) string {
	var parts []string

	if target.Role != "" && target.Role != "generic" {
		if target.Role == "button" {
			parts = append(parts, "button, [role='button']")
		} else if target.Role == "link" {
			parts = append(parts, "a, [role='link']")
		} else if target.Role == "textbox" {
			parts = append(parts, "input[type='text'], input[type='search'], textarea, [role='textbox']")
		} else if target.Role == "combobox" {
			parts = append(parts, "select, [role='combobox']")
		} else {
			parts = append(parts, fmt.Sprintf("[role='%s']", target.Role))
		}
	} else {
		parts = append(parts, "*")
	}

	selector := parts[0]

	if target.Name != "" {
		selector += fmt.Sprintf("[name='%s']", target.Name)
	}
	if target.Placeholder != "" {
		selector += fmt.Sprintf("[placeholder*='%s']", target.Placeholder)
	}

	return selector
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func splitText(text string, maxLines, maxLength int) []string {
	lines := make([]string, 0)
	current := ""

	for _, char := range text {
		if char == '\n' || char == '\r' {
			if current != "" {
				if len(current) > maxLength {
					current = current[:maxLength]
				}
				lines = append(lines, current)
				current = ""
				if len(lines) >= maxLines {
					break
				}
			}
		} else {
			current += string(char)
		}
	}

	if current != "" && len(lines) < maxLines {
		if len(current) > maxLength {
			current = current[:maxLength]
		}
		lines = append(lines, current)
	}

	return lines
}
