// Package runner implements HTML report generation.
package runner

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"os"

	"github.com/brianxiadong/llm-benchmark-kit/pkg/result"
)

//go:embed templates/report.html
var reportTemplate string

//go:embed templates/assets/js/echarts.min.js
var echartsJS []byte

//go:embed templates/assets/fonts/JetBrainsMono-Regular.woff2
var jetBrainsMonoFont []byte

//go:embed templates/assets/fonts/PlusJakartaSans-Variable.woff2
var plusJakartaSansFont []byte

func (r *Runner) writeHTMLReport(report *result.BenchmarkReport, path string) error {
	tmpl, err := template.New("report").Parse(reportTemplate)
	if err != nil {
		return err
	}

	// Convert report to JSON for embedding
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return err
	}

	// Encode fonts to base64 for embedding
	jetBrainsMonoBase64 := base64.StdEncoding.EncodeToString(jetBrainsMonoFont)
	plusJakartaSansBase64 := base64.StdEncoding.EncodeToString(plusJakartaSansFont)

	data := map[string]interface{}{
		"Report":                report,
		"ReportJSON":            template.JS(reportJSON),
		"EChartsJS":             template.JS(echartsJS),
		"JetBrainsMonoBase64":   jetBrainsMonoBase64,
		"PlusJakartaSansBase64": plusJakartaSansBase64,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	return os.WriteFile(path, buf.Bytes(), 0644)
}
