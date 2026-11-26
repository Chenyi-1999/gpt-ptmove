package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

const (
	maxGuestInput  = 128 << 10
	maxGuestOutput = 64 << 10
	guestTimeout   = 3 * time.Second
)

type packageRuntime struct {
	engine     wazero.Runtime
	wasi       map[string]api.FunctionDefinition
	slots      chan struct{}
	executable string
}

type packageModule struct {
	local wazero.CompiledModule
}

var isolatedExecutable atomic.Pointer[string]

// EnableProcessIsolation selects the production runtime before creating the
// app. An invalid executable fails initialization instead of falling back.
func EnableProcessIsolation(executable string) {
	isolatedExecutable.Store(&executable)
}

func newPackageRuntime(ctx context.Context) (*packageRuntime, error) {
	if executable := isolatedExecutable.Load(); executable != nil {
		if !filepath.IsAbs(*executable) {
			return nil, errors.New("插件工作进程需要绝对路径")
		}
		return &packageRuntime{executable: *executable, slots: make(chan struct{}, 2)}, nil
	}
	return newInProcessRuntime(ctx)
}

func newInProcessRuntime(ctx context.Context) (*packageRuntime, error) {
	config := wazero.NewRuntimeConfigInterpreter().
		WithMemoryLimitPages(512).
		WithMemoryCapacityFromMax(true).
		WithCloseOnContextDone(true).
		WithDebugInfoEnabled(false)
	engine := wazero.NewRuntimeWithConfig(ctx, config)
	_, err := wasi_snapshot_preview1.Instantiate(ctx, engine)
	if err != nil {
		_ = engine.Close(ctx)
		return nil, err
	}
	return &packageRuntime{
		engine: engine,
		wasi:   engine.Module(wasi_snapshot_preview1.ModuleName).ExportedFunctionDefinitions(),
		slots:  make(chan struct{}, 4),
	}, nil
}

// compile checks the WASI command contract without instantiating guest code.
func (r *packageRuntime) compile(ctx context.Context, binary []byte) (*packageModule, error) {
	if r.executable != "" {
		if _, err := callPackageWorker(ctx, r.executable, workerCompile, binary, nil); err != nil {
			return nil, err
		}
		return &packageModule{}, nil
	}
	compiled, err := r.engine.CompileModule(ctx, binary)
	if err != nil {
		return nil, errors.New("无法编译 WASM 模块")
	}
	if err := r.validateImports(compiled); err != nil {
		_ = compiled.Close(ctx)
		return nil, err
	}
	return &packageModule{local: compiled}, nil
}

func (r *packageRuntime) close(ctx context.Context) error {
	if r.engine == nil {
		return nil
	}
	return r.engine.Close(ctx)
}

func (m *packageModule) close() {
	if m.local != nil {
		_ = m.local.Close(context.Background())
	}
}

func (r *packageRuntime) validateImports(compiled wazero.CompiledModule) error {
	start, ok := compiled.ExportedFunctions()["_start"]
	if !ok || len(start.ParamTypes()) != 0 || len(start.ResultTypes()) != 0 {
		return errors.New("WASM 模块须提供 WASI p1 _start 入口")
	}
	if len(compiled.ImportedMemories()) != 0 {
		return errors.New("WASM 模块不能导入内存")
	}
	for _, f := range compiled.ImportedFunctions() {
		module, name, _ := f.Import()
		host, exists := r.wasi[name]
		if module != wasi_snapshot_preview1.ModuleName || !exists || strings.HasPrefix(name, "sock_") ||
			!slices.Equal(f.ParamTypes(), host.ParamTypes()) || !slices.Equal(f.ResultTypes(), host.ResultTypes()) {
			return errors.New("WASM 仅支持兼容的 WASI p1 接口")
		}
	}
	return nil
}

func (r *packageRuntime) execute(ctx context.Context, compiled *packageModule, code, input []byte) (ActionResult, error) {
	if len(input) > maxGuestInput {
		return ActionResult{}, fmt.Errorf("%w：输入数据过大", ErrPackageExecution)
	}
	if r.executable != "" {
		result, err := callPackageWorker(ctx, r.executable, workerExecute, code, input)
		if err != nil {
			return ActionResult{}, fmt.Errorf("%w：%s", ErrPackageExecution, err)
		}
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, guestTimeout)
	defer cancel()
	output := boundedOutput{limit: maxGuestOutput}
	config := wazero.NewModuleConfig().
		WithName("").
		WithStartFunctions("_start").
		WithStdin(bytes.NewReader(input)).
		WithStdout(&output).
		WithStderr(io.Discard)
	// No arguments, environment, filesystem mounts, or socket listeners are exposed.
	mod, err := r.engine.InstantiateModule(ctx, compiled.local, config)
	if mod != nil {
		defer mod.Close(context.Background())
	}
	if ctx.Err() != nil {
		return ActionResult{}, fmt.Errorf("%w：运行超时或已取消", ErrPackageExecution)
	}
	if output.overflow {
		return ActionResult{}, fmt.Errorf("%w：输出超过 64 KiB", ErrPackageExecution)
	}
	if err != nil {
		return ActionResult{}, fmt.Errorf("%w：WASM 运行异常", ErrPackageExecution)
	}
	var result ActionResult
	if decodePackageJSON(output.buffer.Bytes(), &result) != nil || validateResult(&result) != nil {
		return ActionResult{}, fmt.Errorf("%w：返回内容不符合协议", ErrPackageExecution)
	}
	return result, nil
}

func guestInput(info PackageInfo, action string, resources map[string]any) ([]byte, error) {
	data := make(map[string]any)
	for _, capability := range info.Capabilities {
		if value, ok := resources[capability]; ok {
			data[capability] = value
		}
	}
	return json.Marshal(struct {
		Action    string         `json:"action"`
		Settings  map[string]any `json:"settings"`
		Resources map[string]any `json:"resources"`
	}{action, info.Settings, data})
}

type boundedOutput struct {
	buffer   bytes.Buffer
	overflow bool
	limit    int
}

func (w *boundedOutput) Write(data []byte) (int, error) {
	if len(data) > w.limit-w.buffer.Len() {
		w.overflow = true
		return 0, errors.New("WASM 输出超限")
	}
	return w.buffer.Write(data)
}
