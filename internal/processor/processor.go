package processor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Operation defines the operations the processor can handle.
type Operation int

const (
	// StartContentOperation tells the processor to begin streaming content.
	StartContentOperation = iota
	// StartGroupsOperation tells the processor to begin streaming groups.
	StartGroupsOperation
	// StopOperation tells the processor to shut down all spawned children,
	// contexts, and pipes.
	StopOperation
)

// Command contains the description of a command the processor will execute.
type Command struct {
	Operation  Operation
	Selector   string
	Format     string
	Group      string
	Path       string
	Width      int
	Wrap       bool
	LineNumber bool
}

// CommandChannel is a tea.Msg that conveys the channel the processor will be
// listening on for commands.
type CommandChannel struct {
	CmdChan chan<- Command
}

// ContentError is a tea.Msg that conveys an error that occurred when looking
// for content.
type ContentError struct {
	Message string
	Err     error
	Jq      string
}

// ContentLine is a tea.Msg that conveys a line of content read by the
// processor.
type ContentLine struct {
	Line string
}

// GroupLine is a tea.Msg that conveys a group read by the processor.
type GroupLine struct {
	Line string
}

// GroupError is a tea.Msg that conveys an error that occurred when looking
// for groups.
type GroupError struct {
	Message string
	Err     error
	Jq      string
}

// JQCommand is a tea.Msg that conveys the equivalent jq command that would
// produce the content reported by the processor.
type JQCommand struct {
	Jq string
}

// ContentStart is a tea.Msg that indicates the processor is (re)starting a read
// for content.
type ContentStart struct {
}

// GroupsStart is a tea.Msg that indicates the processor is (re)starting a read
// for groups.
type GroupsStart struct {
}

// Stopped is a tea.Msg that indicates the processor has stopped. All child
// processes are killed, contexts are cancled, and pipes are closed.
type Stopped struct {
}

// Run runs the processor for the given tea.Program. It first creates a command
// channel and then sends that channel to the program via a CommandChannel
// message. It then listens on that channel for commands.
func Run(program *tea.Program) {
	cmdChan := make(chan Command)
	program.Send(CommandChannel{CmdChan: cmdChan})
	var contentHandler *streamHandler
	var groupsHandler *streamHandler
	for {
		cmd := <-cmdChan
		switch cmd.Operation {
		case StartContentOperation:
			if contentHandler != nil {
				contentHandler.cancel()
			}
			contentHandler = &streamHandler{}
			contentHandler.ctx, contentHandler.cancel = context.WithCancel(context.Background())
			program.Send(ContentStart{})
			go contentHandler.streamContent(program, cmd)
		case StartGroupsOperation:
			if groupsHandler != nil {
				groupsHandler.cancel()
			}
			groupsHandler = &streamHandler{}
			groupsHandler.ctx, groupsHandler.cancel = context.WithCancel(context.Background())
			program.Send(GroupsStart{})
			go groupsHandler.streamGroups(program, cmd)
		case StopOperation:
			if contentHandler != nil {
				contentHandler.cancel()
				kill(contentHandler.cmds...)
				program.Send(Stopped{})
			}
			return
		}
	}
}

// streamHandler holds the tracking data for a processor process. This includes
// the context that will stop any child processes, the cancel function for that
// context, and a slice of the child process commands.
type streamHandler struct {
	ctx    context.Context
	cancel func()
	cmds   []*exec.Cmd
}

// streamContent creates a command pipeline that is basically
//
// tail -f -n +1 $filename | jq -r --unbuffered $format
//
// with a sed at the end in place of either cut or fold depending on whether or
// not wrapping is requested.  Cut and fold would be better here but the
// buffering those programs do message up the eventing. Sed brings its own
// problems as it only supports 255 repitions. If the width is greater than 255,
// wrapping is capped at 255. If not wrapping then characters 253,254, and 255
// are replaced with '.'. The rest of the line is cut. A ContentLine message is
// sent to the program for each line that makes it through the pipline.
func (h *streamHandler) streamContent(program *tea.Program, cmd Command) {
	arg := createContentArg(cmd.Selector, cmd.Group, cmd.Format)
	jqCmdString := "jq -r '" + arg + "' '" + cmd.Path + "'"
	program.Send(JQCommand{
		Jq: jqCmdString,
	})
	tailCmd := exec.CommandContext(h.ctx, "tail", "-f", "-n", "+1", cmd.Path)
	jqCmd := exec.CommandContext(h.ctx, "jq", "-r", "--unbuffered", arg)
	var filterCmd *exec.Cmd
	width := calculateWidth(cmd)
	ellipsis := ""
	if width > 255 {
		width = 252
		ellipsis = "..."
	}
	if cmd.Wrap {
		filterCmd = exec.CommandContext(h.ctx, "sed", "-u", fmt.Sprintf("s/.\\{%d\\}/&\\n/g", width))
	} else {
		filterCmd = exec.CommandContext(h.ctx, "sed", "-u", fmt.Sprintf("s/^\\(.\\{%d\\}\\).*/\\1%s/", width, ellipsis))
	}
	stdoutPipe, err := join(tailCmd, jqCmd, filterCmd)
	if err != nil {
		program.Send(ContentError{Message: "join", Err: err, Jq: jqCmdString})
	}
	err = start(tailCmd, jqCmd, filterCmd)
	if err != nil {
		program.Send(ContentError{Message: "start", Err: err, Jq: jqCmdString})
	}
	h.cmds = []*exec.Cmd{tailCmd, jqCmd, filterCmd}
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Split(bufio.ScanLines)
	lineNumber := 1
	for scanner.Scan() {
		select {
		case <-h.ctx.Done():
			err = kill(h.cmds...)
			if err != nil {
				program.Send(ContentError{Message: "kill", Err: err, Jq: jqCmdString})
			}
			return
		default:
			line := scanner.Text()
			if cmd.LineNumber {
				line = fmt.Sprintf("%5d: %s", lineNumber, line)
			}
			program.Send(ContentLine{
				Line: line,
			})
			lineNumber++
		}
	}
}

// streamGroups creates a command pipeline that is basically
//
// tail -f -n +1 $filename | jq -r --unbuffered $selector
//
// A GroupLine message is sent to the program for each line that makes it
// through the pipline.
func (h *streamHandler) streamGroups(program *tea.Program, cmd Command) {
	arg := createGroupsSelectorArg(cmd.Selector)
	jqCmdString := "jq -r '" + arg + "' '" + cmd.Path + "'"
	tailCmd := exec.CommandContext(h.ctx, "tail", "-f", "-n", "+1", cmd.Path)
	jqCmd := exec.CommandContext(h.ctx, "jq", "-r", "--unbuffered", arg)
	stdoutPipe, err := join(tailCmd, jqCmd)
	if err != nil {
		program.Send(GroupError{Message: "join", Err: err, Jq: jqCmdString})
	}
	err = start(tailCmd, jqCmd)
	if err != nil {
		program.Send(GroupError{Message: "start", Err: err, Jq: jqCmdString})
	}
	h.cmds = []*exec.Cmd{tailCmd, jqCmd}
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		select {
		case <-h.ctx.Done():
			err = kill(h.cmds...)
			if err != nil {
				program.Send(GroupError{Message: "kill", Err: err, Jq: jqCmdString})
			}
			return
		default:
			line := scanner.Text()
			if line == "" || line[0] == '{' || line[0] == '[' {
				h.cancel()
				err = kill(h.cmds...)
				if err != nil {
					program.Send(GroupError{Message: "kill", Err: err, Jq: jqCmdString})
				}
				return
			}
			program.Send(GroupLine{
				Line: line,
			})
		}
	}
}

// calculateWidth returns the appropriate width for wrapping or cutting based on
// the given command.
func calculateWidth(cmd Command) int {
	if cmd.LineNumber {
		return cmd.Width - 7
	}
	return cmd.Width
}

// kill kills all the given exec.Cmds.
func kill(cmds ...*exec.Cmd) error {
	for _, cmd := range cmds {
		err := cmd.Process.Kill()
		if err != nil {
			return err
		}
	}
	return nil
}

// start starts all the given exec.Cmds.
func start(cmds ...*exec.Cmd) error {
	for _, cmd := range cmds {
		cmd.WaitDelay = 1 * time.Nanosecond
		err := cmd.Start()
		if err != nil {
			return err
		}
	}
	return nil
}

// join connects the stdout of each exec.Cmd in the given slice to the next
// exec.Cmd in the slice. An io.MultiReader connected to the stdout and stderr
// of the last exec.Cmd in the list is returned.
func join(cmds ...*exec.Cmd) (io.Reader, error) {
	for i := 0; i < len(cmds)-1; i++ {
		stdout, err := cmds[i].StdoutPipe()
		if err != nil {
			return nil, err
		}
		cmds[i+1].Stdin = stdout
	}
	stdout, err := cmds[len(cmds)-1].StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmds[len(cmds)-1].StderrPipe()
	if err != nil {
		return nil, err
	}
	return io.MultiReader(stdout, stderr), nil
}

// createContentArg returns a jq query string for the given selector, group, and
// format. The selector identifies the field that must exist in the JSON
// objects, the group represents the value that the field must have, and the
// format represents the format of the object to return. For example,
// seletor:= ".level"
// group:="error"
// format:=".timeStamp + \":\" + .message"
func createContentArg(selector, group, format string) string {
	if selector == "" {
		selector = "."
	}
	if format == "" {
		format = "."
	}
	if group == "*" {
		return fmt.Sprintf(".|select(%s)|%s", selector, format)
	}
	return fmt.Sprintf(".|select(%s==\"%s\")|%s", selector, group, format)
}

// createGroupsSelectorArg returns a jq query string for the given selector. It
// is expected that this selector identifies a field in a JSON object. Like
// ".level" or ".object.field". The returned string, when passed to jq, will
// produce a newline delimited list of strings that can be used to select
// objects where the selector matches the value.
func createGroupsSelectorArg(selector string) string {
	if selector == "" {
		return "."
	}
	return fmt.Sprintf(".|select(%s)|%s", selector, selector)
}
