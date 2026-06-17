package script

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dop251/goja"
	"k8s.io/klog/v2"
)

// JSRule executes a user-defined JavaScript function to process sensor messages.
//
// The script must define a global function:
//
//	function process(messages) {
//	    // messages: array of sensor data objects
//	    // return:   transformed array, or null / empty array to drop
//	    return messages.filter(function(m) { return m.value > 100; });
//	}
//
// Built-in helpers available inside the script:
//
//	log(msg)  – write a klog Info line from within the script
type JSRule struct {
	name        string
	sourceTopic string
	program     *goja.Program // pre-compiled; reused per invocation
}

// scriptTimeout is the maximum wall-clock time allowed per script execution.
const scriptTimeout = 5 * time.Second

// NewJSRule compiles the provided JavaScript source and returns a JSRule.
// An error is returned if the script has a syntax error.
func NewJSRule(name, sourceTopic, jsCode string) (*JSRule, error) {
	prog, err := goja.Compile(name+".js", jsCode, false)
	if err != nil {
		return nil, fmt.Errorf("compile JS script: %w", err)
	}
	// Validate that script defines a process() function by doing a dry run.
	vm := goja.New()
	if _, err := vm.RunProgram(prog); err != nil {
		return nil, fmt.Errorf("run JS script: %w", err)
	}
	if _, ok := goja.AssertFunction(vm.Get("process")); !ok {
		return nil, fmt.Errorf("JS script must define a process(messages) function")
	}
	klog.Infof("JS rule %q compiled successfully", name)
	return &JSRule{name: name, sourceTopic: sourceTopic, program: prog}, nil
}

func (r *JSRule) Name() string        { return r.name }
func (r *JSRule) SourceTopic() string { return r.sourceTopic }

// Process unmarshals payload, runs the JS process() function, and marshals the result.
func (r *JSRule) Process(payload []byte) ([]byte, error) {
	// Unmarshal input JSON into a generic Go value so goja can consume it.
	var messages interface{}
	if err := json.Unmarshal(payload, &messages); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	vm := goja.New()

	// Register built-in helpers.
	_ = vm.Set("log", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			klog.Infof("[JS rule %s] %v", r.name, call.Argument(0).Export())
		}
		return goja.Undefined()
	})

	// Run the script to define the process() function.
	if _, err := vm.RunProgram(r.program); err != nil {
		return nil, fmt.Errorf("run script: %w", err)
	}

	processFn, ok := goja.AssertFunction(vm.Get("process"))
	if !ok {
		return nil, fmt.Errorf("process() function not found after script run")
	}

	// Enforce execution timeout via goroutine interrupt.
	timer := time.AfterFunc(scriptTimeout, func() {
		vm.Interrupt(fmt.Errorf("script execution timeout (%s)", scriptTimeout))
	})
	defer timer.Stop()

	result, err := processFn(goja.Undefined(), vm.ToValue(messages))
	if err != nil {
		return nil, fmt.Errorf("execute process(): %w", err)
	}

	exported := result.Export()
	if exported == nil {
		return nil, nil // script returned null/undefined → drop
	}

	out, err := json.Marshal(exported)
	if err != nil {
		return nil, fmt.Errorf("marshal script result: %w", err)
	}

	// Treat empty array or JSON null as a drop signal.
	if string(out) == "null" || string(out) == "[]" {
		return nil, nil
	}

	return out, nil
}
