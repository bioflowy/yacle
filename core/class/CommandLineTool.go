package class

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	cwl "github.com/otiai10/cwl.go"
)

// CommandLineTool represents class described as "CommandLineTool".
type CommandLineTool struct {
	Outdir     string // Given by context
	Root       *cwl.Root
	Parameters cwl.Parameters
	Command    *exec.Cmd
}

// Run ...
func (tool *CommandLineTool) Run() error {

	// FIXME: this procedure ONLY adjusts to "baseCommand" job
	arguments := tool.ensureArguments()

	priors, inputs, err := tool.ensureInputs()
	if err != nil {
		return fmt.Errorf("failed to ensure required inputs: %v", err)
	}

	cmd, err := tool.generateBasicCommand(priors, arguments, inputs)
	if err != nil {
		return fmt.Errorf("failed to generate command struct: %v", err)
	}
	tool.Command = cmd

	if err := tool.defineCommandExecDirectory(); err != nil {
		return fmt.Errorf("failed to define command execution directory: %v", err)
	}

	if err := tool.placeInputFilesToCommandExecDir(); err != nil {
		return fmt.Errorf("failed to place input files: %v", err)
	}
	if err := tool.defineStdinSource(); err != nil {
		return fmt.Errorf("failed to define stdin source: %v", err)
	}

	if err := tool.defineStdoutDestination(); err != nil {
		return fmt.Errorf("failed to define stdout destination: %v", err)
	}

	if err := tool.defineStderrDestination(); err != nil {
		return fmt.Errorf("failed to define stderr destination: %v", err)
	}

	if err := tool.setupEnvironmentVariable(); err != nil {
		return fmt.Errorf("failed to setup environment variable: %v", err)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("exec failed: %v", err)
	}

	if err := tool.defineOutputDir(); err != nil {
		return fmt.Errorf("failed to define output dir: %v", err)
	}

	if err := tool.arrangeOutputDirContents(); err != nil {
		return fmt.Errorf("failed to arrange output contents: %v", err)
	}

	return nil
}

// ensureArguments ...
func (tool *CommandLineTool) ensureArguments() []string {
	result := []string{}
	sort.Sort(tool.Root.Arguments)
	for i, arg := range tool.Root.Arguments {
		if arg.Binding != nil && arg.Binding.ValueFrom != nil {
			tool.Root.Arguments[i].Value = tool.AliasFor(arg.Binding.ValueFrom.Key())
		}
		result = append(result, tool.Root.Arguments[i].Flatten()...)
	}
	return result
}

// ensureInputs ...
func (tool *CommandLineTool) ensureInputs() (priors []string, result []string, err error) {
	sort.Sort(tool.Root.Inputs)
	for _, in := range tool.Root.Inputs {
		in, err = tool.ensureInput(in)
		if err != nil {
			return priors, result, err
		}
		if in.Binding == nil {
			continue
		}
		if in.Binding.Position < 0 {
			priors = append(priors, in.Flatten()...)
		} else {
			result = append(result, in.Flatten()...)
		}
	}
	return priors, result, nil
}

// ensureInput ...
func (tool *CommandLineTool) ensureInput(input *cwl.Input) (*cwl.Input, error) {
	if provided, ok := tool.Parameters[input.ID]; ok {
		input.Provided = cwl.Provided{}.New(input.ID, provided)
	}
	if input.Default == nil && input.Binding == nil && input.Provided == nil {
		return input, fmt.Errorf("input `%s` doesn't have default field but not provided", input.ID)
	}
	if key, needed := input.Types[0].NeedRequirement(); needed {
		for _, req := range tool.Root.Requirements {
			for _, requiredtype := range req.Types {
				if requiredtype.Name == key {
					input.RequiredType = &requiredtype
					input.Requirements = tool.Root.Requirements
				}
			}
		}
	}
	return input, nil
}

// AliasFor ...
func (tool *CommandLineTool) AliasFor(key string) string {
	switch key {
	case "runtime.cores":
		return "2"
	}
	return ""
}

// generateBasicCommand ...
func (tool *CommandLineTool) generateBasicCommand(priors, arguments, inputs []string) (*exec.Cmd, error) {

	if len(tool.Root.BaseCommands) == 0 {
		return exec.Command("bash", "-c", tool.Root.Arguments[0].Binding.ValueFrom.Key()), nil
	}

	// Join all slices
	oneline := []string{}
	oneline = append(oneline, tool.Root.BaseCommands...)
	oneline = append(oneline, priors...)
	oneline = append(oneline, arguments...)
	oneline = append(oneline, inputs...)

	return exec.Command(oneline[0], oneline[1:]...), nil
}

// defineCommandExecDirectory
func (tool *CommandLineTool) defineCommandExecDirectory() error {

	// Prefer specified "--outdir" for working directory
	if tool.Outdir != "" {
		tool.Command.Dir = tool.Outdir
		return nil
	}

	// Anyway, use temp directory for Command Exec Directory
	tmpdir, err := ioutil.TempDir("/tmp", "yacle-")
	if err != nil {
		return err
	}
	tool.Command.Dir = tmpdir

	return nil
}

// placeInputFilesToCommandExecDir ...
func (tool *CommandLineTool) placeInputFilesToCommandExecDir() error {

	rootdir := filepath.Dir(tool.Root.Path)
	cmddir := tool.Command.Dir

	for _, input := range tool.Root.Inputs {
		if provided := input.Provided; provided != nil && provided.Entry != nil {
			return provided.Entry.LinkTo(cmddir, rootdir)
		}
		if defaultinput := input.Default; defaultinput != nil && defaultinput.Entry != nil {
			return defaultinput.Entry.LinkTo(cmddir, rootdir)
		}
	}

	return nil
}

// defineStdoutDestination
func (tool *CommandLineTool) defineStdinSource() error {
	if tool.Root.Stdin == "" {
		return nil
	}
	stdin, err := tool.Command.StdinPipe()
	if err != nil {
		return err
	}
	defer stdin.Close()
	stdinfilename := tool.Root.Stdin
	if tool.needVariableEvaluation(stdinfilename) {
		// Create JavaScript runtime
		vm, err := tool.Root.Inputs.ToJavaScriptVM()
		if err != nil {
			return err
		}
		if vm == nil {
			return nil
		}
		variable, err := tool.extractVariableExpression(stdinfilename)
		if err != nil {
			return err
		}
		retval, err := vm.Run(variable)
		if err != nil {
			return err
		}
		if !retval.IsString() {
			return nil
		}
		stdinfilename = retval.String()
		tool.Root.InputsVM = vm
	}
	stdinfilepath := filepath.Join(tool.Command.Dir, stdinfilename)
	if filepath.IsAbs(stdinfilename) {
		stdinfilepath = stdinfilename
	}
	stdinfile, err := os.Open(stdinfilepath)
	if err != nil {
		return err
	}
	_, err = io.Copy(stdin, stdinfile)
	if err != nil {
		return err
	}

	return nil
}

// check if we need variable evaluation
func (tool *CommandLineTool) needVariableEvaluation(s string) bool {
	return strings.HasPrefix(s, "$(") && strings.HasSuffix(s, ")")
}

// extract variable expression
func (tool *CommandLineTool) extractVariableExpression(s string) (string, error) {
	s = strings.TrimLeft(s, "$(")
	s = regexp.MustCompile("\\)$").ReplaceAllString(s, "")
	return s, nil
}

// defineStdoutDestination
func (tool *CommandLineTool) defineStdoutDestination() error {

	// Prefer "stdout" specified on ROOT
	if tool.Root.Stdout != "" {
		stdoutfilepath := filepath.Join(tool.Command.Dir, tool.Root.Stdout)
		stdoutfile, err := os.Create(stdoutfilepath)
		if err != nil {
			return err
		}
		tool.Command.Stdout = stdoutfile
		return nil
	}

	// Respect "stdout" specified in each "output"
	for _, o := range tool.Root.Outputs {
		if o.Types[0].Type == "stdout" {
			stdoutfilepath := filepath.Join(tool.Command.Dir, o.ID)
			stdoutfile, err := os.Create(stdoutfilepath)
			if err != nil {
				return err
			}
			tool.Command.Stdout = stdoutfile
		}
	}

	// nothing specified
	return nil
}

// defineStderrDestination
func (tool *CommandLineTool) defineStderrDestination() error {

	// Prefer "stderr" specified on ROOT
	if tool.Root.Stderr != "" {
		stderrfilepath := filepath.Join(tool.Command.Dir, tool.Root.Stderr)
		stderrfile, err := os.Create(stderrfilepath)
		if err != nil {
			return err
		}
		tool.Command.Stderr = stderrfile
		return nil
	}

	// Respect "stderr" specified in each "output"
	for _, o := range tool.Root.Outputs {
		if o.Types[0].Type == "stderr" {
			stderrfilepath := filepath.Join(tool.Command.Dir, o.ID)
			stderrfile, err := os.Create(stderrfilepath)
			if err != nil {
				return err
			}
			tool.Command.Stderr = stderrfile
		}
	}

	return nil
}

// defineOutputDir
func (tool *CommandLineTool) defineOutputDir() error {
	if tool.Outdir != "" {
		return nil
	}
	rootdir := filepath.Dir(tool.Root.Path)
	tool.Outdir = rootdir
	return nil
}

// setupEnvironmentVariable
func (tool *CommandLineTool) setupEnvironmentVariable() error {

	// Setup Environment Variable to command
	if tool.Root.Requirements == nil {
		return nil
	}
	for _, requirement := range tool.Root.Requirements {
		if requirement.Class != "EnvVarRequirement" {
			continue
		}
		for _, envdef := range requirement.EnvDef {
			if !tool.needVariableEvaluation(envdef.Value) {
				tool.Command.Env = append(tool.Command.Env, envdef.Name+"="+envdef.Value)
				continue
			}
			// Create JavaScript runtime
			if tool.Root.InputsVM == nil {
				vm, err := tool.Root.Inputs.ToJavaScriptVM()
				if err != nil {
					return err
				}
				if vm == nil {
					return nil
				}
				tool.Root.InputsVM = vm
			}
			variable, err := tool.extractVariableExpression(envdef.Value)
			if err != nil {
				return err
			}
			retval, err := tool.Root.InputsVM.Run(variable)
			if err != nil {
				return err
			}
			if !retval.IsString() {
				return nil
			}

			tool.Command.Env = append(tool.Command.Env, envdef.Name+"="+retval.String())
		}
	}
	return nil
}

// arrangeOutputDirContents
func (tool *CommandLineTool) arrangeOutputDirContents() error {

	// If "cwl.output.json" exists on executed command directory,
	// dump the file contents on stdout.
	// This is described on official document.
	// See also https://www.commonwl.org/v1.0/CommandLineTool.html#Output_binding
	whatthefuck := filepath.Join(tool.Command.Dir, "cwl.output.json")
	if defaultout, err := os.Open(whatthefuck); err == nil {
		defer defaultout.Close()
		if _, err := io.Copy(os.Stdout, defaultout); err != nil {
			return err
		}
		return nil
	}

	// Load Contents as JavaScript runtime if needed.
	vm, err := tool.Root.Outputs.LoadContents(tool.Command.Dir)
	if err != nil {
		return err
	}

	// CWL wants to dump metadata of outputs with type="File"
	// See also https://www.commonwl.org/v1.0/CommandLineTool.html#File
	if err := tool.Root.Outputs.Dump(vm, tool.Command.Dir, tool.Root.Stdout, tool.Root.Stderr, os.Stdout); err != nil {
		return err
	}

	return nil
}

// Finalize closes all file desccriptors if needed.
func (tool *CommandLineTool) Finalize() error {
	return nil
}
