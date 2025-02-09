package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/docopt/docopt-go"
	"github.com/mrxk/jlv/internal/model"
	"github.com/mrxk/jlv/internal/processor"
)

const (
	jsonlogUsage = `
JSON log viewer: jlv

Usage:
	jlv [options] <path>

Options:
	-s <selector>, --selector=<selector> JSON path to grouping field.
	-o <format>, --output=<format>       Format of output.
	-l, --linenumbers                    Show line numbers.
	-w, --wrap                           Wrap output.
	`
)

// parseArgs takes a usage sting and returns a populated model.ModelOpts from
// the current os.Args.
func parseArgs(usage string) (model.ModelOpts, error) {
	opts := model.ModelOpts{}
	docOpts, err := docopt.ParseDoc(usage)
	if err != nil {
		return opts, err
	}
	opts.Selector, _ = docOpts.String("--selector")
	opts.Output, _ = docOpts.String("--output")
	opts.Path, _ = docOpts.String("<path>")
	opts.LineNumbers, _ = docOpts.Bool("--linenumbers")
	opts.Wrap, _ = docOpts.Bool("--wrap")
	return opts, nil
}

func main() {
	opts, err := parseArgs(jsonlogUsage)
	if err != nil {
		panic(err)
	}
	p := tea.NewProgram(model.NewModel(opts), tea.WithAltScreen())
	go processor.Run(p)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
