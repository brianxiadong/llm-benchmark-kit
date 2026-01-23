// Package assets provides shared static assets for HTML reports.
// This includes fonts, CSS themes, and JavaScript libraries.
package assets

import (
	_ "embed"
	"encoding/base64"
)

// Fonts (embedded as base64-encoded woff2)
//
//go:embed fonts/JetBrainsMono-Regular.woff2
var JetBrainsMonoWoff2 []byte

//go:embed fonts/PlusJakartaSans-Regular.woff2
var PlusJakartaSansWoff2 []byte

// JavaScript
//
//go:embed js/echarts.min.js
var EChartsJS []byte

// CSS
//
//go:embed css/theme.css
var ThemeCSS []byte

// GetEChartsJS returns the ECharts library as a string
func GetEChartsJS() string {
	return string(EChartsJS)
}

// GetThemeCSS returns the theme CSS as a string
func GetThemeCSS() string {
	return string(ThemeCSS)
}

// GetFontFaceCSS returns the @font-face CSS declarations with embedded fonts
func GetFontFaceCSS() string {
	jetBrainsB64 := base64.StdEncoding.EncodeToString(JetBrainsMonoWoff2)
	jakartaB64 := base64.StdEncoding.EncodeToString(PlusJakartaSansWoff2)

	return `
        @font-face {
            font-family: 'JetBrains Mono';
            src: url('data:font/woff2;base64,` + jetBrainsB64 + `') format('woff2');
            font-weight: 400 600;
            font-style: normal;
            font-display: swap;
        }
        @font-face {
            font-family: 'Plus Jakarta Sans';
            src: url('data:font/woff2;base64,` + jakartaB64 + `') format('woff2');
            font-weight: 400 600;
            font-style: normal;
            font-display: swap;
        }
    `
}

// GetFullCSS returns the complete CSS including fonts and theme
func GetFullCSS() string {
	return GetFontFaceCSS() + "\n" + GetThemeCSS()
}

// LogoMarkSVG returns the SVG logo mark
const LogoMarkSVG = `<svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
    <path d="M13 3L4 14h7l-2 7 9-11h-7l2-7z" fill="currentColor"/>
</svg>`
