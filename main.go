package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/docopt/docopt-go"
	"github.com/mrxk/jlv/internal/model"
)

func parseArgs(usage string) (model.ModelOpts, error) {
	opts := model.ModelOpts{}
	docOpts, err := docopt.ParseDoc(usage)
	if err != nil {
		return opts, err
	}
	opts.Format, _ = docOpts.String("--format")
	opts.Selector, _ = docOpts.String("--selector")
	opts.Path, _ = docOpts.String("<path>")
	return opts, nil
}

func main() {
	opts, err := parseArgs(jsonlogUsage)
	if err != nil {
		panic(err)
	}
	p := tea.NewProgram(model.NewModel(opts), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

const (
	jsonlogUsage = `jlv

Usage:
	jlv [options] <path>

Options:
	-s <selector>, --selector=<selector> JSON path to grouping field.
	-f <format>, --format=<format>       Format of output.
	`
)
