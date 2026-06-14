package ops

import (
	"bytes"
	"embed"
	"strings"
	"text/template"
)

//go:embed templates/*
var templateFS embed.FS

func renderTemplate(name string, data any) ([]byte, error) {
	tpl, err := template.New(name).Funcs(template.FuncMap{
		"indent": indent,
	}).ParseFS(templateFS, "templates/"+name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func indent(spaces int, value string) string {
	prefix := strings.Repeat(" ", spaces)
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n") + "\n"
}
