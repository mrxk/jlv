package processor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
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
	Operation Operation
	Selector  string
	Format    string
	Group     string
	Path      string
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

// GroupsLine is a tea.Msg that conveys a group read by the processor.
type GroupsLine struct {
	Line string
}

// GroupsError is a tea.Msg that conveys an error that occurred when looking
// for groups.
type GroupsError struct {
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
	InitialContent []string
}

// GroupsStart is a tea.Msg that indicates the processor is (re)starting a read
// for groups.
type GroupsStart struct {
	InitialGroups []string
}

// ContentStopped is a tea.Msg that indicates the processor has stopped. All child
// processes are killed, contexts are cancled, and pipes are closed.
type ContentStopped struct {
}

// GroupsStopped is a tea.Msg that indicates the processor has stopped. All child
// processes are killed, contexts are cancled, and pipes are closed.
type GroupsStopped struct {
}

// Run runs the processor for the given tea.Program. It first creates a command
// channel and then sends that channel to the program via a CommandChannel
// message. It then listens on that channel for commands.
func Run(program *tea.Program) {
	cmdChan := make(chan Command)
	program.Send(CommandChannel{CmdChan: cmdChan})
	contentChan := make(chan streamArgs)
	groupsChan := make(chan streamArgs)
	var contentCancel func() = nil
	var groupsCancel func() = nil
	go func() {
		for {
			streamArgs, ok := <-contentChan
			if !ok {
				program.Send(ContentStopped{})
				return
			}
			streamContent(streamArgs)
		}
	}()
	go func() {
		for {
			streamArgs, ok := <-groupsChan
			if !ok {
				program.Send(GroupsStopped{})
				return
			}
			streamGroups(streamArgs)
		}
	}()
	for {
		cmd := <-cmdChan
		switch cmd.Operation {
		case StartContentOperation:
			if contentCancel != nil {
				contentCancel()
			}
			var ctx context.Context
			ctx, contentCancel = context.WithCancel(context.Background())
			contentChan <- streamArgs{
				ctx:     ctx,
				cancel:  contentCancel,
				program: program,
				cmd:     cmd,
			}
		case StartGroupsOperation:
			if groupsCancel != nil {
				groupsCancel()
			}
			var ctx context.Context
			ctx, groupsCancel = context.WithCancel(context.Background())
			groupsChan <- streamArgs{
				ctx:     ctx,
				cancel:  groupsCancel,
				program: program,
				cmd:     cmd,
			}
		case StopOperation:
			if contentCancel != nil {
				contentCancel()
			}
			if groupsCancel != nil {
				groupsCancel()
			}
			close(contentChan)
			close(groupsChan)
			return
		}
	}
}

// streamArgs holds the tracking data for a processor process. This includes
// the context that will stop any child processes, the cancel function for that
// context, and a slice of the child process commands.
type streamArgs struct {
	ctx     context.Context
	cancel  func()
	program *tea.Program
	cmd     Command
}

// streamContent parses the file and sends the parsed content to the program.
func streamContent(args streamArgs) {
	jqQuery := createJQContentQuery(args.cmd.Selector, args.cmd.Group, args.cmd.Format)
	consumedLineCount, err := sendInitialContent(args, jqQuery)
	if err != nil {
		return
	}
	streamNewContent(args, jqQuery, consumedLineCount)
}

// sendInitialContent parses the current contents of the file and sends them as
// a ContentStart message to the program. The number of lines read from the file
// is returned.
func sendInitialContent(args streamArgs, jqQuery string) (int, error) {
	jqCmdString := "jq -r '" + jqQuery + "' '" + args.cmd.Path + "'"
	args.program.Send(JQCommand{
		Jq: jqCmdString,
	})
	lineCount, err := countLines(args.cmd.Path)
	if err != nil {
		args.program.Send(ContentError{Message: "sendInitialContent count", Err: err, Jq: jqCmdString})
		return 0, err
	}
	headCmd := exec.CommandContext(args.ctx, "head", fmt.Sprintf("-%d", lineCount), args.cmd.Path)
	jqCmd := exec.CommandContext(args.ctx, "jq", "-r", jqQuery, args.cmd.Path)
	pipe, err := join(headCmd, jqCmd)
	if err != nil {
		args.program.Send(ContentError{Message: "sendInitialContent join", Err: err, Jq: jqCmdString})
		return 0, err
	}
	err = start(headCmd, jqCmd)
	if err != nil {
		if err != context.Canceled {
			args.program.Send(ContentError{Message: "sendInitialContent start", Err: err, Jq: jqCmdString})
		}
		return 0, err
	}
	initialContentBytes, err := io.ReadAll(pipe)
	if err != nil {
		args.program.Send(ContentError{Message: "sendInitialContent io.ReadAll", Err: err, Jq: jqCmdString})
		return 0, err
	}
	err = kill(headCmd, jqCmd)
	if err != nil {
		args.program.Send(ContentError{Message: "sendInitialContent kill", Err: err, Jq: jqCmdString})
		return 0, err
	}
	// If we were cancled then don't send the content we gathered
	select {
	case <-args.ctx.Done():
		return 0, nil
	default:
	}
	initialContentBytes = bytes.TrimRight(initialContentBytes, "\n")
	initialContent := strings.Split(string(initialContentBytes), "\n")
	args.program.Send(ContentStart{
		InitialContent: initialContent,
	})
	return lineCount, nil
}

// streamNewContent creates a command pipeline that connects tail -f and jq with
// a query string assembled from the Selector, Format, and Group fields of the
// given Command. The tail command starts at the given startLineNumber. Each
// line emitted from jq is sent as a ContentLine message to the attached
// tea.Program.
func streamNewContent(args streamArgs, jqQuery string, startLineNumber int) {
	jqCmdString := "jq -r '" + jqQuery + "' '" + args.cmd.Path + "'"
	tailCmd := exec.CommandContext(args.ctx, "tail", "-f", "-n", fmt.Sprintf("+%d", startLineNumber+1), args.cmd.Path)
	jqCmd := exec.CommandContext(args.ctx, "jq", "-r", "--unbuffered", jqQuery)
	stdoutPipe, err := join(tailCmd, jqCmd)
	if err != nil {
		args.program.Send(ContentError{Message: "streamNewContent join", Err: err, Jq: jqCmdString})
		return
	}
	err = start(tailCmd, jqCmd)
	if err != nil {
		if err != context.Canceled {
			args.program.Send(ContentError{Message: "streamNewContent start", Err: err, Jq: jqCmdString})
		}
		return
	}
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		select {
		case <-args.ctx.Done():
			err = kill(tailCmd, jqCmd)
			if err != nil {
				args.program.Send(ContentError{Message: "streamNewContent kill", Err: err, Jq: jqCmdString})
			}
			return
		default:
			line := scanner.Text()
			args.program.Send(ContentLine{
				Line: line,
			})
		}
	}
}

// streamGroups parses the file and sends the parsed content to the program.
func streamGroups(args streamArgs) {
	jqQuery := createGroupsSelectorArg(args.cmd.Selector)
	consumedLineCount, err := sendInitialGroups(args, jqQuery)
	if err != nil {
		return
	}
	streamNewGroups(args, jqQuery, consumedLineCount)
}

// sendInitialGroups parses the current contents of the file and sends them as
// a GroupsStart message to the program. The number of lines read from the file
// is returned.
func sendInitialGroups(args streamArgs, jqQuery string) (int, error) {
	jqCmdString := "jq -r '" + jqQuery + "' '" + args.cmd.Path + "'"
	lines, err := countLines(args.cmd.Path)
	if err != nil {
		args.program.Send(GroupsError{Message: "sendInitialGroups count", Err: err, Jq: jqCmdString})
		return 0, err
	}
	headCmd := exec.CommandContext(args.ctx, "head", fmt.Sprintf("-%d", lines), args.cmd.Path)
	jqCmd := exec.CommandContext(args.ctx, "jq", "-r", jqQuery, args.cmd.Path)
	pipe, err := join(headCmd, jqCmd)
	if err != nil {
		args.program.Send(GroupsError{Message: "sendInitialGroups join", Err: err, Jq: jqCmdString})
		return 0, err
	}
	err = start(headCmd, jqCmd)
	if err != nil {
		if err != context.Canceled {
			args.program.Send(GroupsError{Message: "sendInitialGroups start", Err: err, Jq: jqCmdString})
		}
		return 0, err
	}
	initialContentBytes, err := io.ReadAll(pipe)
	if err != nil {
		args.program.Send(GroupsError{Message: "sendInitialGroups io.ReadAll", Err: err, Jq: jqCmdString})
		return 0, err
	}
	err = kill(headCmd, jqCmd)
	if err != nil {
		args.program.Send(GroupsError{Message: "sendInitialContent kill", Err: err, Jq: jqCmdString})
		return 0, err
	}
	// If we were cancled then don't send the content we gathered
	select {
	case <-args.ctx.Done():
		return 0, nil
	default:
	}
	var initialContent []string
	if len(initialContentBytes) != 0 && initialContentBytes[0] != '{' && initialContentBytes[0] != '[' {
		initialContentBytes = bytes.TrimRight(initialContentBytes, "\n")
		initialContent = strings.Split(string(initialContentBytes), "\n")
	}
	args.program.Send(GroupsStart{
		InitialGroups: initialContent,
	})
	return lines, nil
}

// streamNewGroups creates a command pipeline that connects tail -f and jq with a
// query string assembled from the Selector field of the given Command. Each
// line emitted from jq is sent as a GroupsLine message to the attached
// tea.Program.
func streamNewGroups(args streamArgs, jqQuery string, startLineNumber int) {
	jqCmdString := "jq -r '" + jqQuery + "' '" + args.cmd.Path + "'"
	tailCmd := exec.CommandContext(args.ctx, "tail", "-f", "-n", fmt.Sprintf("+%d", startLineNumber+1), args.cmd.Path)
	jqCmd := exec.CommandContext(args.ctx, "jq", "-r", "--unbuffered", jqQuery)
	stdoutPipe, err := join(tailCmd, jqCmd)
	if err != nil {
		args.program.Send(GroupsError{Message: "streamNewGroups join", Err: err, Jq: jqCmdString})
		return
	}
	err = start(tailCmd, jqCmd)
	if err != nil {
		if err != context.Canceled {
			args.program.Send(GroupsError{Message: "streamNewGroups start", Err: err, Jq: jqCmdString})
		}
		return
	}
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		select {
		case <-args.ctx.Done():
			err = kill(tailCmd, jqCmd)
			if err != nil {
				args.program.Send(GroupsError{Message: "streamNewGroups kill", Err: err, Jq: jqCmdString})
			}
			return
		default:
			line := scanner.Text()
			if line == "" || line[0] == '{' || line[0] == '[' {
				args.cancel()
				err = kill(tailCmd, jqCmd)
				if err != nil {
					args.program.Send(GroupsError{Message: "streamNewGroups kill", Err: err, Jq: jqCmdString})
				}
				return
			}
			args.program.Send(GroupsLine{
				Line: line,
			})
		}
	}
}

// countLines returns the number of newline delimited lines in the given file.
func countLines(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	buf := make([]byte, bufio.MaxScanTokenSize)
	count := 0
	for {
		n, err := file.Read(buf)
		count += bytes.Count(buf[:n], []byte{'\n'})
		if err != nil {
			if err == io.EOF {
				return count, nil
			}
			return count, err
		}
	}
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

// createJQContentQuery returns a jq query string for the given selector, group, and
// format. The selector identifies the field that must exist in the JSON
// objects, the group represents the value that the field must have, and the
// format represents the format of the object to return. For example,
// seletor:= ".level"
// group:="error"
// format:=".timeStamp + \":\" + .message"
func createJQContentQuery(selector, group, format string) string {
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
