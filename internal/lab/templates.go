package lab

import (
	"bytes"
	"embed"
	"text/template"
)

//go:embed templates/*
var templateFS embed.FS

func renderTemplate(name string, data any) ([]byte, error) {
	tpl, err := template.ParseFS(templateFS, "templates/"+name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
