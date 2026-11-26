package plugins

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

const (
	workerCompile      byte = 'C'
	workerExecute      byte = 'E'
	workerHeaderSize        = 9
	workerTimeout           = 5 * time.Second
	workerOutputLimit       = 2*maxGuestOutput + 1024
	workerMemoryTarget      = 96 << 20
)

type workerReply struct {
	Error  string        `json:"error,omitempty"`
	Result *ActionResult `json:"result,omitempty"`
}

// RunPackageWorker is the private worker entry point. It installs resource
// limits before accepting untrusted bytes. It receives no app configuration.
func RunPackageWorker(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	runtime.GOMAXPROCS(1)
	debug.SetMemoryLimit(workerMemoryTarget)
	if err := restrictPackageWorker(); err != nil {
		return err
	}
	return runPackageWorker(ctx, stdin, stdout)
}

func runPackageWorker(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	var header [workerHeaderSize]byte
	if _, err := io.ReadFull(stdin, header[:]); err != nil {
		return err
	}
	op := header[0]
	wasmSize := binary.BigEndian.Uint32(header[1:5])
	inputSize := binary.BigEndian.Uint32(header[5:9])
	if !validWorkerRequest(op, int(wasmSize), int(inputSize)) {
		return errors.New("插件工作进程请求无效")
	}
	wasm := make([]byte, wasmSize)
	input := make([]byte, inputSize)
	if _, err := io.ReadFull(stdin, wasm); err != nil {
		return err
	}
	if _, err := io.ReadFull(stdin, input); err != nil {
		return err
	}

	runner, err := newInProcessRuntime(ctx)
	if err != nil {
		return err
	}
	defer runner.close(context.Background())
	reply := workerReply{}
	module, err := runner.compile(ctx, wasm)
	if err != nil {
		reply.Error = err.Error()
	} else if op == workerExecute {
		res, err := runner.execute(ctx, module, nil, input)
		if err != nil {
			reply.Error = strings.TrimPrefix(err.Error(), ErrPackageExecution.Error()+"：")
		} else {
			reply.Result = &res
		}
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(reply)
}

func callPackageWorker(ctx context.Context, executable string, op byte, wasm, input []byte) (ActionResult, error) {
	if !validWorkerRequest(op, len(wasm), len(input)) {
		return ActionResult{}, errors.New("插件工作进程请求无效")
	}
	ctx, cancel := context.WithTimeout(ctx, workerTimeout)
	defer cancel()
	var header [workerHeaderSize]byte
	header[0] = op
	binary.BigEndian.PutUint32(header[1:5], uint32(len(wasm)))
	binary.BigEndian.PutUint32(header[5:9], uint32(len(input)))
	output := boundedOutput{limit: workerOutputLimit}
	cmd := exec.CommandContext(ctx, executable, "--plugin-worker")
	cmd.Env = []string{"GOMAXPROCS=1", "GOMEMLIMIT=96MiB"}
	cmd.Stdin = io.MultiReader(bytes.NewReader(header[:]), bytes.NewReader(wasm), bytes.NewReader(input))
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return ActionResult{}, errors.New("插件工作进程已取消或超时")
	}
	if output.overflow {
		return ActionResult{}, errors.New("插件工作进程输出超限")
	}
	if err != nil {
		return ActionResult{}, errors.New("插件工作进程中止或超限")
	}
	var reply workerReply
	if decodePackageJSON(output.buffer.Bytes(), &reply) != nil {
		return ActionResult{}, errors.New("插件工作进程的响应无效")
	}
	if reply.Error != "" && validText(reply.Error, 400, false) {
		return ActionResult{}, errors.New(reply.Error)
	}
	if op == workerExecute && reply.Result != nil && validateResult(reply.Result) == nil {
		return *reply.Result, nil
	}
	if op == workerCompile && reply.Result == nil && reply.Error == "" {
		return ActionResult{}, nil
	}
	return ActionResult{}, errors.New("插件工作进程的响应无效")
}

func validWorkerRequest(op byte, wasmSize, inputSize int) bool {
	if wasmSize < 1 || wasmSize > maxModuleBytes || inputSize < 0 || inputSize > maxGuestInput {
		return false
	}
	return (op == workerCompile && inputSize == 0) || (op == workerExecute && inputSize > 0)
}
