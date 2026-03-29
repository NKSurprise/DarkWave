package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type DarkWaveTheme struct {
	accent color.Color
}

var (
	AccentRed    = color.NRGBA{R: 255, G: 59, B: 59, A: 255}
	AccentBlue   = color.NRGBA{R: 88, G: 101, B: 242, A: 255}
	AccentGreen  = color.NRGBA{R: 0, G: 200, B: 120, A: 255}
	AccentPurple = color.NRGBA{R: 149, G: 70, B: 255, A: 255}
)

func NewDarkWaveTheme(accent color.Color) *DarkWaveTheme {
	return &DarkWaveTheme{accent: accent}
}

func (t *DarkWaveTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary:
		return t.accent
	case theme.ColorNameBackground:
		return color.NRGBA{R: 30, G: 31, B: 34, A: 255}
	case theme.ColorNameButton:
		return color.NRGBA{R: 43, G: 45, B: 49, A: 255}
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 43, G: 45, B: 49, A: 255}
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 43, G: 45, B: 49, A: 255}
	}
	return theme.DarkTheme().Color(name, variant)
}

func (t *DarkWaveTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DarkTheme().Font(style)
}

func (t *DarkWaveTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DarkTheme().Icon(name)
}

func (t *DarkWaveTheme) Size(name fyne.ThemeSizeName) float32 {
	return theme.DarkTheme().Size(name)
}
