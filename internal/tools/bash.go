package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type bashInput struct {
	Command string   `json:"command"`
	Timeout *float64 `json:"timeout"`
}

func init() {
	Register(Tool{
		Name:        "bash",
		Description: "Execute a bash command in the current working directory. Returns stdout and stderr. Output is truncated to last 2000 lines or 50KB (whichever is hit first). If truncated, full output is saved to a temp file. A timeout in seconds is required; the command is not run without it.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"command": {"type": "string", "description": "Shell command to execute"},
				"timeout": {"type": "number", "description": "Timeout in seconds (optional, no default timeout)"}
			},
			"required": ["command", "timeout"],
			"additionalProperties": false
		}`),
		Run: runBash,
	})
}

func runBash(ctx context.Context, args json.RawMessage) (string, error) {
	var input bashInput
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("invalid bash arguments: %w", err)
	}
	if input.Command == "" {
		return "", fmt.Errorf("command is required")
	}
	if input.Timeout == nil {
		return "", fmt.Errorf("timeout is required: specify a timeout in seconds")
	}

	cwd := WorkDir(ctx)
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		return "", fmt.Errorf("working directory does not exist: %s\ncannot execute bash commands", cwd)
	}

	shell, err := exec.LookPath("bash")
	if err != nil {
		return "", fmt.Errorf("bash is not available: %w", err)
	}

	timeoutSeconds := *input.Timeout
	if timeoutSeconds <= 0 {
		return "", fmt.Errorf("invalid timeout: must be a finite number of seconds")
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds*float64(time.Second)))
	defer cancel()

	outputFile, err := os.CreateTemp("", bashOutputPrefix+"*")
	if err != nil {
		return "", fmt.Errorf("create output file: %w", err)
	}
	outputPath := outputFile.Name()
	defer func() {
		if outputFile != nil {
			outputFile.Close()
		}
	}()

	counter := &outputCounter{file: outputFile}
	command := exec.Command(shell, "-c", input.Command)
	command.Dir = cwd
	command.Env = os.Environ()
	command.Stdout = counter
	command.Stderr = counter
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Do not wait forever for output pipes held open by detached children.
	command.WaitDelay = 2 * time.Second

	if err := command.Start(); err != nil {
		return "", fmt.Errorf("start command: %w", err)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- command.Wait() }()

	var waitErr error
	select {
	case waitErr = <-waitDone:
	case <-runCtx.Done():
		// Kill the whole process group so child processes die too.
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		waitErr = <-waitDone
	}
	timedOut := runCtx.Err() == context.DeadlineExceeded

	if err := outputFile.Close(); err != nil {
		return "", fmt.Errorf("close output file: %w", err)
	}
	outputFile = nil

	exitCode := 0
	if waitErr != nil {
		if exitError, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitCodeOf(exitError)
		} else {
			os.Remove(outputPath)
			return "", waitErr
		}
	}

	totalBytes := counter.total()
	totalLines := counter.lines()
	output, truncated, partial, truncatedBy := readTail(outputPath, totalBytes, totalLines)

	var text string
	if truncated {
		fullOutput := "Full output: " + outputPath
		startLine := totalLines - countTailLines(output) + 1
		endLine := totalLines
		switch {
		case partial:
			text = output + fmt.Sprintf("\n\n[Showing last %s of line %d (line is %s). %s]",
				formatSize(len(output)), startLine, formatSize(totalBytes), fullOutput)
		case truncatedBy == "lines":
			text = output + fmt.Sprintf("\n\n[Showing lines %d-%d of %d. %s]", startLine, endLine, totalLines, fullOutput)
		default:
			text = output + fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). %s]", startLine, endLine, totalLines, formatSize(maxBytes), fullOutput)
		}
	} else {
		os.Remove(outputPath)
		text = output
		if text == "" {
			text = "(no output)"
		}
	}

	if ctx.Err() != nil && !timedOut {
		return text, fmt.Errorf("Command aborted")
	}
	if timedOut {
		return text, fmt.Errorf("Command timed out after %s seconds", formatSeconds(timeoutSeconds))
	}
	if exitCode != 0 {
		return text, fmt.Errorf("Command exited with code %d", exitCode)
	}
	return text, nil
}

type outputCounter struct {
	file      *os.File
	mu        sync.Mutex
	totalSize int
	newlines  int
	last      byte
}

func (c *outputCounter) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, err := c.file.Write(p)
	c.totalSize += n
	c.newlines += bytes.Count(p[:n], []byte{'\n'})
	if n > 0 {
		c.last = p[n-1]
	}
	return n, err
}

func (c *outputCounter) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalSize
}

func (c *outputCounter) lines() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.totalSize == 0 {
		return 0
	}
	if c.last == '\n' {
		return c.newlines
	}
	return c.newlines + 1
}

func exitCodeOf(err *exec.ExitError) int {
	if status, ok := err.Sys().(syscall.WaitStatus); ok {
		if status.Signaled() {
			return 128 + int(status.Signal())
		}
		return status.ExitStatus()
	}
	return err.ExitCode()
}

// readTail reads the last maxBytes of output and keeps at most maxLines lines.
func readTail(path string, totalBytes, totalLines int) (output string, truncated, partial bool, truncatedBy string) {
	if totalBytes <= maxBytes && totalLines <= maxLines {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", false, false, ""
		}
		return string(data), false, false, ""
	}

	file, err := os.Open(path)
	if err != nil {
		return "", true, false, "lines"
	}
	defer file.Close()

	offset := int64(0)
	if totalBytes > maxBytes {
		offset = int64(totalBytes - maxBytes)
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return "", true, false, "lines"
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return "", true, false, "lines"
	}
	window := string(data)

	truncatedBy = "lines"
	if offset > 0 {
		truncatedBy = "bytes"
		// Drop the partial first line unless the window is a single huge line.
		if index := strings.IndexByte(window, '\n'); index >= 0 {
			window = window[index+1:]
		} else {
			partial = true
		}
	}

	lines := splitLinesForCounting(window)
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
		truncatedBy = "lines"
	}
	return strings.Join(lines, "\n"), true, partial, truncatedBy
}

func countTailLines(content string) int {
	if content == "" {
		return 0
	}
	return len(strings.Split(content, "\n"))
}

// bashOutputPrefix names the temporary files holding full bash output.
const bashOutputPrefix = "foliarz-bash-"

// CleanOutputFiles removes bash full-output files older than maxAge. Truncated
// tool output keeps its full copy in a temp file, so old files are pruned to
// stop unbounded disk growth.
func CleanOutputFiles(maxAge time.Duration) int {
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), bashOutputPrefix+"*"))
	if err != nil {
		return 0
	}
	removed := 0
	cutoff := time.Now().Add(-maxAge)
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(path); err == nil {
			removed++
		}
	}
	return removed
}

func formatSeconds(seconds float64) string {
	if seconds == float64(int64(seconds)) {
		return fmt.Sprintf("%d", int64(seconds))
	}
	return fmt.Sprintf("%g", seconds)
}
