package navigation

import (
	"regexp"
	"strings"
)

func ExtractURLFromDescription(description string) string {
	urlPattern := regexp.MustCompile(`https?://[^\s]+`)
	if m := urlPattern.FindString(description); m != "" {
		return m
	}
	return ""
}

func ResolveURLFromTask(task string) string {
	if u := extractExplicitURL(task); u != "" {
		return u
	}
	return DetermineURLFromTaskContext(task)
}

func extractExplicitURL(s string) string {
	urlPattern := regexp.MustCompile(`https?://[^\s]+`)
	if m := urlPattern.FindString(s); m != "" {
		return m
	}
	return ""
}

func DetermineURLFromTaskContext(task string) string {
	taskLower := strings.ToLower(task)
	if strings.Contains(taskLower, "yandex market") || strings.Contains(taskLower, "яндекс маркет") ||
		(strings.Contains(taskLower, "yandex") && strings.Contains(taskLower, "market")) {
		return "https://market.yandex.ru"
	}
	if strings.Contains(taskLower, "ozon") || strings.Contains(taskLower, "озон") {
		return "https://www.ozon.ru"
	}
	if strings.Contains(taskLower, "wildberries") || strings.Contains(taskLower, "вильдберриз") || strings.Contains(taskLower, "вб") {
		return "https://www.wildberries.ru"
	}
	return ""
}
