package main

import "testing"

func TestTranslationPortugueseAndEnglish(t *testing.T) {
	SetLanguage(langEnglish)
	if got := T("Connect"); got != "Connect" {
		t.Fatalf("english Connect = %q", got)
	}
	if got := T("Talk"); got != "Talk" {
		t.Fatalf("english Talk = %q", got)
	}
	SetLanguage(langPortuguese)
	if got := T("Connect"); got != "Conectar" {
		t.Fatalf("portuguese Connect = %q", got)
	}
	if got := T("Talk"); got != "Falar" {
		t.Fatalf("portuguese Talk = %q", got)
	}
	if got := T("CamerasShowing", map[string]any{"Count": 3}); got != "3 cameras/perfis em exibicao" {
		t.Fatalf("portuguese template = %q", got)
	}
	SetLanguage(langEnglish)
}

func TestTranslationFallbackToKey(t *testing.T) {
	SetLanguage(langEnglish)
	if got := T("MissingKeyThatDoesNotExist"); got != "MissingKeyThatDoesNotExist" {
		t.Fatalf("missing key fallback = %q", got)
	}
}

func TestNormalizeResolutionAndQuality(t *testing.T) {
	if got := normalizeResolutionMode("Otimizada"); got != "optimized" {
		t.Fatalf("Otimizada -> %q", got)
	}
	if got := normalizeResolutionMode("Original"); got != "original" {
		t.Fatalf("Original -> %q", got)
	}
	if got := normalizeQuality("Fluido"); got != "fluid" {
		t.Fatalf("Fluido -> %q", got)
	}
	if got := normalizeQuality("HD"); got != "hd" {
		t.Fatalf("HD -> %q", got)
	}
}

func TestLanguageHelpers(t *testing.T) {
	SetLanguage(langPortuguese)
	if languageFromDisplay(T("Portuguese")) != langPortuguese {
		t.Fatal("portuguese display was not mapped back")
	}
	SetLanguage(langEnglish)
	if languageFromDisplay(T("English")) != langEnglish {
		t.Fatal("english display was not mapped back")
	}
	if resolutionModeFromLabel(T("Original")) != "original" {
		t.Fatal("original label was not mapped back")
	}
}
