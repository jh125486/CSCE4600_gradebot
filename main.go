package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/alecthomas/kong"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// embedded testdata.
var (
	//go:embed testdata/fcfs.csv
	fcfsIn []byte
	//go:embed testdata/fcfs.out
	fcfsOut []byte

	//go:embed testdata/sjf.csv
	sjfIn []byte
	//go:embed testdata/sjf.out
	sjfOut []byte

	//go:embed testdata/sjfp.csv
	sjfpIn []byte
	//go:embed testdata/sjfp.out
	sjfpOut []byte

	//go:embed testdata/rr.csv
	rrIn []byte
	//go:embed testdata/rr.out
	rrOut []byte
)

type (
	grammar struct {
		options
		PathToDir string `name:"dir" default:"." help:"Path to scheduler directory" type:"path" required:"true"`
	}
	options struct {
		Debug bool `help:"Debug output."`
		Total bool `help:"Print total only"`
	}
)

func main() {
	if err := kong.Parse(&grammar{},
		kong.Name("gradebot"),
		kong.Description("Gradebot 9000 is a tool to grade your 4600 project 1."),
		kong.UsageOnError(),
	).Run(); err != nil {
		slog.Error("error running gradebot", slog.String("err", err.Error()))
	}
	pauseForInput(os.Stdout, os.Stdin)
}

func pauseForInput(w io.Writer, r io.Reader) {
	_, _ = fmt.Fprintf(w, "press any key to continue...")
	input := bufio.NewScanner(r)
	input.Scan()
}

type (
	Context struct {
		srcDir string
		binary string
	}
	Check  func(*Context) (Result, error)
	Result struct {
		label    string
		awarded  int
		possible int
		message  string
	}
)

func (o *options) setup() {
	// Set up logging.
	lvl := new(slog.LevelVar)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: lvl,
	}))
	if o.Debug {
		lvl.Set(slog.LevelDebug)
	}
	if o.Total {
		lvl.Set(10)
	}
	slog.SetDefault(logger)
}

func (cmd grammar) Run() error {
	cmd.options.setup()

	var (
		rubric  Context
		results = make([]Result, 0)
	)
	rubric.srcDir = cmd.PathToDir
	for _, check := range []Check{
		CheckCompilable,
		CheckScreenshotExists,
		CheckREADMEExists,
		CheckScheduler(Result{
			label:    "First-come, first-serve scheduling",
			possible: 20,
		}, "-fcfs", fcfsIn, fcfsOut),
		CheckScheduler(Result{
			label:    "Shortest-job-first scheduling",
			possible: 20,
		}, "-sjf", sjfIn, sjfOut),
		CheckScheduler(Result{
			label:    "Shortest-job-first with priority scheduling",
			possible: 20,
		}, "-sjfp", sjfpIn, sjfpOut),
		CheckScheduler(Result{
			label:    "Round-robin scheduling",
			possible: 10,
		}, "-rr", rrIn, rrOut),
	} {
		result, err := check(&rubric)
		if err != nil {
			slog.Error(result.label, slog.String("err", err.Error()))
		}
		results = append(results, result)
	}

	printRubricResults(cmd.Total, results...)

	// cleanup
	_ = os.RemoveAll(rubric.binary)

	return nil
}

func printRubricResults(onlyTotal bool, results ...Result) {
	if onlyTotal {
		totalPoints := 0
		for i := range results {
			totalPoints += results[i].awarded
		}
		fmt.Println(totalPoints)
		return
	}

	t := table.NewWriter()
	t.AppendHeader(table.Row{"Rubric Item", "Error?", "Possible", "Awarded"})
	t.SetStyle(table.StyleRounded)
	t.SetColumnConfigs([]table.ColumnConfig{
		{Number: 2, AlignFooter: text.AlignRight},
	})

	var (
		possiblePoints int
		totalPoints    int
	)
	for i := range results {
		t.AppendRow([]any{results[i].label, results[i].message, results[i].possible, results[i].awarded})
		possiblePoints += results[i].possible
		totalPoints += results[i].awarded
	}
	t.AppendFooter(table.Row{"", "Total", possiblePoints, totalPoints})
	fmt.Println(t.Render())
}

//region Checkers

func CheckCompilable(c *Context) (Result, error) {
	result := Result{
		label:    "Compilable",
		awarded:  0,
		possible: 10,
	}
	if err := os.Chdir(c.srcDir); err != nil {
		return result, err
	}
	// check for Go in path.
	if _, err := exec.LookPath("go"); err != nil {
		result.message = "Go executable not found in path"
		return result, err
	}
	// compile the scheduler.
	if err := exec.Command("go", "build", "-o", "scheduler.bin").Run(); err != nil {
		result.message = "scheduler is not compileable"
		return result, err
	}
	c.binary = filepath.Join(c.srcDir, "scheduler.bin")

	result.awarded += 10
	slog.Debug("scheduler is compileable", slog.Int("pts", 10))

	return result, nil

}

func CheckScreenshotExists(c *Context) (Result, error) {
	result := Result{
		label:    "Screenshot exists",
		awarded:  0,
		possible: 10,
	}
	if c.binary == "" {
		result.message = "scheduler was not compileable"
		return result, errors.New("binary not found")
	}
	if _, err := os.Stat(filepath.Join(c.srcDir, "screenshot.png")); err != nil {
		result.message = "screenshot.png not found"
		return result, err
	}
	result.awarded += 10
	slog.Debug("screenshot.png exists", slog.Int("pts", 10))

	return result, nil
}

func CheckREADMEExists(c *Context) (Result, error) {
	result := Result{
		label:    "README.md exists",
		awarded:  0,
		possible: 10,
	}
	if c.binary == "" {
		result.message = "scheduler was not compileable"
		return result, errors.New("binary not found")
	}
	if _, err := os.Stat(filepath.Join(c.srcDir, "README.md")); err != nil {
		result.message = "README.md not found"
		return result, err
	}
	result.awarded += 10
	slog.Debug("README.md exists", slog.Int("pts", 10))

	return result, nil
}

func CheckScheduler(result Result, flag string, in, out []byte) func(c *Context) (Result, error) {
	return func(c *Context) (Result, error) {
		if c.binary == "" {
			result.message = "scheduler was not compileable"
			return result, errors.New("binary not found")
		}

		// run the scheduler
		cmd := exec.Command(c.binary, flag)

		// send embedded csv to stdin.
		cmd.Stdin = bytes.NewReader(in)

		var bb bytes.Buffer
		cmd.Stdout = &bb
		if err := cmd.Run(); err != nil {
			result.message = "scheduler exited with error"
			return result, err
		}
		if bb.String() == "" {
			result.message = "scheduler ran with no output"
			return result, nil
		}

		// compare output to expected output
		if bytes.Compare(bb.Bytes(), out) != 0 {
			result.message = "output does not match expected"
			fmt.Print(flag, " expected:\n", string(out))
			fmt.Print(flag, " actual:\n", bb.String())
			return result, errors.New("output does not match expected")
		}

		result.awarded = result.possible
		slog.Debug(fmt.Sprintf("%v Scheduler output matches expected", flag), slog.Int("pts", 30))

		return result, nil
	}
}

//endregion
