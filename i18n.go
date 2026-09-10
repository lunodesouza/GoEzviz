package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"strings"
	"sync"
	"text/template"

	"fyne.io/fyne/v2/lang"
)

//go:embed translation/*.json
var translationFS embed.FS

const (
	langEnglish    = "en"
	langPortuguese = "pt"
)

var (
	i18nMu       sync.RWMutex
	i18nLang     = langEnglish
	i18nCatalogs = map[string]map[string]string{}
)

func init() {
	loadTranslationCatalog(langEnglish)
	loadTranslationCatalog(langPortuguese)
	SetLanguage(langEnglish)
}

func loadTranslationCatalog(code string) {
	data, err := translationFS.ReadFile("translation/" + code + ".json")
	if err != nil {
		return
	}
	catalog := map[string]string{}
	if err := json.Unmarshal(data, &catalog); err != nil {
		return
	}
	i18nMu.Lock()
	i18nCatalogs[code] = catalog
	i18nMu.Unlock()
}

func SetLanguage(code string) {
	code = normalizeLanguage(code)
	i18nMu.Lock()
	i18nLang = code
	i18nMu.Unlock()
}

func CurrentLanguage() string {
	i18nMu.RLock()
	defer i18nMu.RUnlock()
	return i18nLang
}

func normalizeLanguage(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if strings.HasPrefix(code, "pt") {
		return langPortuguese
	}
	if code == langEnglish || strings.HasPrefix(code, "en") {
		return langEnglish
	}
	return langEnglish
}

func suggestedLanguage() string {
	locale := strings.ToLower(lang.SystemLocale().LanguageString())
	if strings.HasPrefix(locale, "pt") {
		return langPortuguese
	}
	return langEnglish
}

// T returns the translation for key in the current language.
// Optional data is applied as a text/template (map or struct with exported fields).
func T(key string, data ...any) string {
	i18nMu.RLock()
	current := i18nLang
	catalog := i18nCatalogs[current]
	fallback := i18nCatalogs[langEnglish]
	i18nMu.RUnlock()

	text, ok := catalog[key]
	if !ok || text == "" {
		text, ok = fallback[key]
	}
	if !ok || text == "" {
		text = key
	}
	if len(data) == 0 {
		return text
	}
	return renderTranslation(key, text, data[0])
}

func renderTranslation(key, text string, data any) string {
	tpl, err := template.New(key).Parse(text)
	if err != nil {
		return text
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return text
	}
	return out.String()
}

func languageDisplayName(code string) string {
	switch normalizeLanguage(code) {
	case langPortuguese:
		return T("Portuguese")
	default:
		return T("English")
	}
}

func languageFromDisplay(name string) string {
	switch name {
	case T("Portuguese"), "Português", "Portuguese":
		return langPortuguese
	default:
		return langEnglish
	}
}

func resolutionModeLabel(mode string) string {
	switch normalizeResolutionMode(mode) {
	case "original":
		return T("Original")
	default:
		return T("Optimized")
	}
}

func resolutionModeFromLabel(label string) string {
	if label == T("Original") || label == "Original" {
		return "original"
	}
	return "optimized"
}

func normalizeResolutionMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "original", "Original":
		return "original"
	case "optimized", "Otimizada", "Optimized":
		return "optimized"
	default:
		return "optimized"
	}
}

func normalizeQuality(quality string) string {
	switch strings.TrimSpace(quality) {
	case "hd", "HD":
		return "hd"
	case "fluid", "Fluido", "Fluid", "Otimizada":
		return "fluid"
	default:
		return "fluid"
	}
}

func qualityLabel(quality string) string {
	if normalizeQuality(quality) == "hd" {
		return T("HD")
	}
	return T("Fluid")
}
