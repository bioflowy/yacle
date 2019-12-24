package core

import (
	"os"
	"fmt"
	"path/filepath"
	"github.com/otiai10/yacle/core/class"
	cwl "github.com/otiai10/cwl.go"
)

func (h *Handler) getSteps(steps cwl.Steps) ([]class.WorkflowStep, error) {
	tools := make([]class.WorkflowStep,0)
	for _,step := range(steps){
		root := cwl.NewCWL()
		reader,err := os.Open(step.Run.Value)
		if err != nil {
			return nil,fmt.Errorf("Cannot open file: %v", err)
		}
		if err := root.Decode(reader); err != nil {
			return nil,fmt.Errorf("failed to decode CWL file: %v", err)
		}
		if root.Path, err = filepath.Abs(step.Run.Value); err != nil {
			return nil, err
		}
		handler, err := NewHandler(root)
		if err != nil {
			return nil,fmt.Errorf("failed to instantiate yacle.Handler: %v", err)
		}
		handler.Outdir = h.Outdir
		tool,err := handler.ClassTool()
		if err != nil {
			return nil,fmt.Errorf("failed to decode CWL file: %v", err)
		}
		tools = append(tools,class.WorkflowStep{
			Tool: &tool,
			Root: step,
		})
	}
	return tools,nil;
}
	// ClassTool constructs and initializes ClassTool, e.g. CommandLineTool.
func (h *Handler) ClassTool() (class.Tool, error) {
	switch h.Workflow.Class {
	case "CommandLineTool":
		return &class.CommandLineTool{
			Outdir:     h.Outdir,
			Root:       h.Workflow,
			Parameters: h.Parameters,
		}, nil
	case "Workflow":
		steps,err := h.getSteps(h.Workflow.Steps)
		if err != nil {
			return nil,err
		}
		return &class.Workflow{
			Outdir:     h.Outdir,
			Root:       h.Workflow,
			Steps:		steps,
			Parameters: h.Parameters,
		}, nil
	default:
		return &class.CommandLineTool{
			Outdir:     h.Outdir,
			Root:       h.Workflow,
			Parameters: h.Parameters,
		}, nil
	}
}
