package main

import (
	"fmt"
	"io"
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
	<path>                               The path of the JSON file to watch.
	                                     "-" for stdin.
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

// streamStdinToTmpFile creates a temp file and copies stdin to that file.  It
// returns the path to the created temp file, a cleanup function, and a channel
// that will be written to when all data has been read from stdin.  If streaming
// from a process that does not stop, like `tail -f`, the channel will never be
// written to and never closed.
func streamStdinToTmpFile() (string, func(), <-chan struct{}) {
	tmpFile, err := os.CreateTemp("", "jlv")
	if err != nil {
		panic(err)
	}
	path := tmpFile.Name()
	cleanup := func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}
	// Spawn a go routine to continually copy data from stdin to the tmp file.
	// Signal done if/when the read is complete.
	done := make(chan struct{})
	go func() {
		io.Copy(tmpFile, os.Stdin)
		done <- struct{}{}
		close(done)
	}()
	return path, cleanup, done
}

func main() {
	opts, err := parseArgs(jsonlogUsage)
	if err != nil {
		panic(err)
	}
	// If reading from stdin, cache data in a temp file so that changing
	// selector and output format can be applied to content displayed in the
	// output window and not just content that arrives on stdin after the change
	// has been made.
	var stdInDone <-chan struct{}
	if opts.Path == "-" {
		var cleanup func()
		opts.Path, cleanup, stdInDone = streamStdinToTmpFile()
		defer cleanup()
	}
	p := tea.NewProgram(model.NewModel(opts), tea.WithAltScreen(), tea.WithInputTTY())
	go processor.Run(p)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	if stdInDone != nil {
		select {
		case <-stdInDone:
		default:
			fmt.Println("Stdin may not be closed. Ctrl-C to exit.")
		}
	}
}
