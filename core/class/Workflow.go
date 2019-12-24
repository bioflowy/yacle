package class

import (
	cwl "github.com/otiai10/cwl.go"
)

// CommandLineTool represents class described as "CommandLineTool".
type Workflow struct {
	Outdir     string // Given by context
	Root       *cwl.Root
	Parameters cwl.Parameters
	Steps      []WorkflowStep
}
type WorkflowStep struct {
	Tool *Tool
	Root cwl.Step
}

// SetParameter ...
func (w *Workflow) SetParameter(params cwl.Parameters) {
	w.Parameters = params
}
func (w *Workflow) GetOutputMetadata() (map[string]interface{}, error) {
	// TODO impliment
	return nil, nil
}

func (w *Workflow) Run() error {
	for _, step := range w.Steps {
		params := cwl.Parameters{}
		for _, in := range step.Root.In {
			params[in.ID] = w.Parameters[in.Source[0]]
		}
		tool := *step.Tool
		tool.SetParameter(params)
		if err := tool.Run(); err != nil {
			return err
		}
		m, err := tool.GetOutputMetadata()
		if err != nil {
			return err
		}
		for _, output := range step.Root.Out {
			m2 := m[output.ID]
			w.Parameters[step.Root.ID+"/"+output.ID] = m2
		}

	}
	return nil
}
func (w *Workflow) Finalize() error {
	return nil
}
