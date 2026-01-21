// Package runner implements HTML report generation.
package runner

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"html/template"
	"os"

	"github.com/brianxiadong/llm-benchmark-kit/pkg/result"
)

//go:embed templates/report.html
var reportTemplate string

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

	data := map[string]interface{}{
		"Report":     report,
		"ReportJSON": template.JS(reportJSON),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	return os.WriteFile(path, buf.Bytes(), 0644)
}
