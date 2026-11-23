package automation

import "context"

// ScanProgress 描述列表读取与实际详情检查的最新进度，列表返回前总量可能未知。
type ScanProgress struct {
	Stage                    string
	Page, Current, Completed int
}

type scanProgressKey struct{}

// WithScanProgress 为本次扫描绑定可选观察者，不改变查询和筛选规则。
func WithScanProgress(ctx context.Context, report func(ScanProgress)) context.Context {
	return context.WithValue(ctx, scanProgressKey{}, report)
}

func reportScanProgress(ctx context.Context, progress ScanProgress) {
	if report, ok := ctx.Value(scanProgressKey{}).(func(ScanProgress)); ok && report != nil {
		report(progress)
	}
}
